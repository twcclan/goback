package backup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/twcclan/goback/backup/blobcache"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
)

// OverwriteMode says how an existing destination file is treated.
type OverwriteMode int

const (
	// OverwriteAlways hashes every part of the existing file and rewrites
	// the ones that differ.
	OverwriteAlways OverwriteMode = iota
	// OverwriteIfChanged trusts a size and mtime match and leaves such a
	// file unread.
	OverwriteIfChanged
)

// Outcome is what RestoreFile did with one file.
type Outcome int

const (
	// OutcomeWritten means the file was written.
	OutcomeWritten Outcome = iota
	// OutcomeUnchanged means the destination already held every part and
	// only its stat was applied.
	OutcomeUnchanged
	// OutcomeSkipped means the destination matched by size and mtime under
	// OverwriteIfChanged and was left unread.
	OutcomeSkipped
	// OutcomeWouldWrite means a dry run would have written the file.
	OutcomeWouldWrite
)

// String implements fmt.Stringer.
func (o Outcome) String() string {
	switch o {
	case OutcomeWritten:
		return "written"
	case OutcomeUnchanged:
		return "unchanged"
	case OutcomeSkipped:
		return "skipped"
	case OutcomeWouldWrite:
		return "would write"
	}

	return "unknown"
}

// RestoreStats counts what a Restorer did and where the bytes came from.
type RestoreStats struct {
	Files     int64
	Written   int64
	Unchanged int64
	Skipped   int64

	BytesFromDestination int64
	BytesFromSeeds       int64
	BytesFromCache       int64
	BytesFromStore       int64
}

const defaultRestoreWorkers = 32

var (
	restoreMeter = otel.Meter("goback.io/backup")
	restoreBytes = mustInstrument(restoreMeter.Int64Counter("goback.backup.restore.bytes", metric.WithDescription("bytes of restored files by source"), metric.WithUnit("By")))
	restoreFiles = mustInstrument(restoreMeter.Int64Counter("goback.backup.restore.files", metric.WithDescription("restored files by outcome")))

	keySource  = attribute.Key("source")
	keyOutcome = attribute.Key("outcome")
)

func mustInstrument[T any](instrument T, err error) T {
	if err != nil {
		panic(err)
	}

	return instrument
}

// Restorer writes files from the store, taking every part it can from the
// destination file, the seeds and the blob cache before the network. Local
// bytes are used only after they hash to the recorded ref.
type Restorer struct {
	Store ObjectStore
	Key   *storekey.Key

	Seeds *SeedMap
	Cache *blobcache.Cache

	// Workers bounds the parts fetched at once; 0 means 32.
	Workers   int
	Overwrite OverwriteMode
	// Verify rehashes every written file before it is renamed into place.
	Verify bool
	// DryRun reports what would change and writes nothing.
	DryRun bool

	stats RestoreStats
}

// Stats returns what the restorer has done so far.
func (r *Restorer) Stats() RestoreStats {
	return RestoreStats{
		Files:                atomic.LoadInt64(&r.stats.Files),
		Written:              atomic.LoadInt64(&r.stats.Written),
		Unchanged:            atomic.LoadInt64(&r.stats.Unchanged),
		Skipped:              atomic.LoadInt64(&r.stats.Skipped),
		BytesFromDestination: atomic.LoadInt64(&r.stats.BytesFromDestination),
		BytesFromSeeds:       atomic.LoadInt64(&r.stats.BytesFromSeeds),
		BytesFromCache:       atomic.LoadInt64(&r.stats.BytesFromCache),
		BytesFromStore:       atomic.LoadInt64(&r.stats.BytesFromStore),
	}
}

func (r *Restorer) countBytes(ctx context.Context, counter *int64, source string, n int64) {
	atomic.AddInt64(counter, n)
	restoreBytes.Add(ctx, n, metric.WithAttributes(keySource.String(source)))
}

func (r *Restorer) countFile(ctx context.Context, outcome Outcome) {
	atomic.AddInt64(&r.stats.Files, 1)

	switch outcome {
	case OutcomeWritten:
		atomic.AddInt64(&r.stats.Written, 1)
	case OutcomeUnchanged:
		atomic.AddInt64(&r.stats.Unchanged, 1)
	case OutcomeSkipped:
		atomic.AddInt64(&r.stats.Skipped, 1)
	}

	restoreFiles.Add(ctx, 1, metric.WithAttributes(keyOutcome.String(outcome.String())))
}

func (r *Restorer) workers() int {
	if r.Workers > 0 {
		return r.Workers
	}

	return defaultRestoreWorkers
}

// RestoreFile puts the file ref describes at path with the recorded mode
// and mtime, writing next to the destination and renaming into place.
func (r *Restorer) RestoreFile(ctx context.Context, path string, stat *proto.FileInfo, ref *proto.Ref) (Outcome, error) {
	obj, err := r.Store.Get(ctx, ref)
	if err != nil {
		return 0, errors.Wrapf(err, "file %x", ref.Hash)
	}

	if obj.GetFile() == nil {
		return 0, errors.Errorf("object %x is not a file", ref.Hash)
	}

	reader := newFileReader(ctx, r.Store, obj.GetFile(), r.Key)
	if _, err := reader.getFileParts(ctx); err != nil {
		return 0, err
	}

	existing, err := os.Lstat(path)
	hasFile := err == nil && existing.Mode().IsRegular()

	if hasFile && r.Overwrite == OverwriteIfChanged && existing.Size() == stat.Size && existing.ModTime().UnixNano() == stat.MtimeNs {
		r.countFile(ctx, OutcomeSkipped)
		return OutcomeSkipped, nil
	}

	var source *os.File
	var matched []bool

	if hasFile {
		source, err = os.Open(path)
		if err != nil {
			return 0, err
		}
		defer source.Close()

		matched = r.verifyParts(source, reader)

		if existing.Size() == reader.size() && allTrue(matched) {
			if !r.DryRun {
				if err := applyStat(path, stat); err != nil {
					return 0, err
				}
			}

			r.countBytes(ctx, &r.stats.BytesFromDestination, "destination", reader.size())
			r.countFile(ctx, OutcomeUnchanged)

			return OutcomeUnchanged, nil
		}
	}

	if r.DryRun {
		r.countFile(ctx, OutcomeWouldWrite)
		return OutcomeWouldWrite, nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".goback-*")
	if err != nil {
		return 0, err
	}

	err = r.assemble(ctx, tmp, source, reader, matched, ref)
	if err == nil && r.Verify {
		err = r.verifyWritten(tmp, reader)
	}

	if err == nil {
		err = tmp.Sync()
	}

	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}

	if err == nil {
		err = applyStat(tmp.Name(), stat)
	}

	if source != nil {
		// the destination must be closed before it is replaced
		_ = source.Close()
	}

	if err == nil {
		err = replace(tmp.Name(), path)
	}

	if err != nil {
		_ = os.Remove(tmp.Name())
		return 0, errors.Wrapf(err, "restoring %s", path)
	}

	r.countFile(ctx, OutcomeWritten)

	return OutcomeWritten, nil
}

// replace renames tmp over path and makes the rename durable; a read-only
// destination, which Windows refuses to replace, is made writable first.
func replace(tmp, path string) error {
	err := os.Rename(tmp, path)
	if err != nil {
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode()&0o200 != 0 {
			return err
		}

		if chmodErr := os.Chmod(path, info.Mode().Perm()|0o200); chmodErr != nil {
			return err
		}

		if err = os.Rename(tmp, path); err != nil {
			return err
		}
	}

	return syncDir(filepath.Dir(path))
}

// syncDir flushes a directory's entries; Windows has no directory fsync
// and its rename is already durable once the volume flushes.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}

	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()

	return d.Sync()
}

func allTrue(flags []bool) bool {
	for _, f := range flags {
		if !f {
			return false
		}
	}

	return true
}

func applyStat(path string, stat *proto.FileInfo) error {
	err := os.Chtimes(path, time.Now(), time.Unix(0, stat.MtimeNs))
	if err != nil {
		return err
	}

	return os.Chmod(path, os.FileMode(stat.Mode))
}

// verifyParts reports for every part whether the file holds it at the
// recorded offset.
func (r *Restorer) verifyParts(file *os.File, reader *fileReader) []bool {
	matched := make([]bool, len(reader.parts))

	for i, part := range reader.parts {
		buf := make([]byte, part.Length)

		_, err := file.ReadAt(buf, int64(part.Offset))
		if err != nil {
			continue
		}

		matched[i] = r.matches(reader, i, buf)
	}

	return matched
}

// verifyWritten rehashes every part of the assembled file.
func (r *Restorer) verifyWritten(file *os.File, reader *fileReader) error {
	for i, part := range reader.parts {
		buf := make([]byte, part.Length)

		if _, err := file.ReadAt(buf, int64(part.Offset)); err != nil {
			return errors.Wrapf(err, "verifying part %d", i)
		}

		if !r.matches(reader, i, buf) {
			return errors.Errorf("part %d at offset %d does not hash to its ref after writing", i, part.Offset)
		}
	}

	return nil
}

// matches reports whether chunk is the content of part i: under the store
// key it must derive the part's blob key, in the clear it must hash to the
// part's ref.
func (r *Restorer) matches(reader *fileReader, i int, chunk []byte) bool {
	part := reader.parts[i]
	if uint64(len(chunk)) != part.Length {
		return false
	}

	if part.Ref == nil {
		return bytes.Equal(chunk, reader.inline)
	}

	if i < len(reader.partKeys) && len(reader.partKeys[i]) > 0 {
		if r.Key == nil {
			return false
		}

		want := reader.partKeys[i]
		first := r.Key.Choose(reader.size(), chunk)
		if first == proto.Encryption_PLAINTEXT {
			first = proto.Encryption_STORE_KEYED
		}

		for _, mode := range []proto.Encryption{first, otherMode(first)} {
			if hmac.Equal(r.Key.BlobKey(mode, chunk), want) {
				return true
			}
		}

		return false
	}

	return proto.NewObject(&proto.Blob{Data: chunk}).Ref().Equal(part.Ref)
}

func otherMode(mode proto.Encryption) proto.Encryption {
	if mode == proto.Encryption_STORE_KEYED {
		return proto.Encryption_CONVERGENT
	}

	return proto.Encryption_STORE_KEYED
}

// assemble writes every part at its offset: matched parts are copied from
// source, the rest come from the seeds, the cache or the store.
func (r *Restorer) assemble(ctx context.Context, dst, source *os.File, reader *fileReader, matched []bool, ref *proto.Ref) error {
	var fromStore []int

	for i, part := range reader.parts {
		offset := int64(part.Offset)

		if part.Ref == nil {
			if _, err := dst.WriteAt(reader.inline, 0); err != nil {
				return err
			}

			r.countBytes(ctx, &r.stats.BytesFromStore, "store", int64(len(reader.inline)))

			continue
		}

		if matched != nil && matched[i] {
			buf := make([]byte, part.Length)
			if _, err := source.ReadAt(buf, offset); err != nil {
				return errors.Wrapf(err, "rereading part %d of %s", i, source.Name())
			}

			if _, err := dst.WriteAt(buf, offset); err != nil {
				return err
			}

			r.countBytes(ctx, &r.stats.BytesFromDestination, "destination", int64(part.Length))

			continue
		}

		if r.Seeds != nil {
			if buf, ok := r.Seeds.read(part); ok && r.matches(reader, i, buf) {
				if _, err := dst.WriteAt(buf, offset); err != nil {
					return err
				}

				r.countBytes(ctx, &r.stats.BytesFromSeeds, "seed", int64(part.Length))

				continue
			}
		}

		if r.Cache != nil {
			if obj, ok := r.Cache.Get(part.Ref); ok {
				data, err := reader.openPart(i, part, obj)
				if err == nil {
					if _, err := dst.WriteAt(data, offset); err != nil {
						return err
					}

					r.countBytes(ctx, &r.stats.BytesFromCache, "cache", int64(len(data)))

					continue
				}

				r.Cache.Drop(part.Ref)
			}
		}

		fromStore = append(fromStore, i)
	}

	if len(fromStore) == 0 {
		return nil
	}

	if parts, ok := r.Store.(PartReader); ok {
		return r.stream(ctx, parts, dst, reader, fromStore, ref)
	}

	grp, gctx := errgroup.WithContext(ctx)
	grp.SetLimit(r.workers())

	for _, i := range fromStore {
		grp.Go(func() error {
			part := reader.parts[i]

			obj, err := r.Store.Get(gctx, part.Ref)
			if err != nil {
				return errors.Wrapf(err, "part %d (%x) of file %x", i, part.Ref.Hash, reader.fileRef())
			}

			return r.writePart(gctx, dst, reader, i, obj)
		})
	}

	return grp.Wait()
}

// stream fetches the wanted parts through one ReadParts call, skipping
// every other part of the file.
func (r *Restorer) stream(ctx context.Context, parts PartReader, dst *os.File, reader *fileReader, wanted []int, ref *proto.Ref) error {
	want := make(map[int]bool, len(wanted))
	for _, i := range wanted {
		want[i] = true
	}

	skip := make([]int, 0, len(reader.parts)-len(wanted))
	for i := range reader.parts {
		if !want[i] {
			skip = append(skip, i)
		}
	}

	seen := 0
	err := parts.ReadParts(ctx, ref, skip, func(i int, obj *proto.Object) error {
		if !want[i] {
			return errors.Errorf("file %x: unrequested part %d", ref.Hash, i)
		}

		seen++

		return r.writePart(ctx, dst, reader, i, obj)
	})
	if err != nil {
		return errors.Wrapf(err, "file %x", ref.Hash)
	}

	if seen != len(wanted) {
		return errors.Errorf("file %x: %d of %d parts streamed", ref.Hash, seen, len(wanted))
	}

	return nil
}

// writePart opens a part's stored object, writes it at its offset and
// caches it.
func (r *Restorer) writePart(ctx context.Context, dst *os.File, reader *fileReader, i int, obj *proto.Object) error {
	part := reader.parts[i]

	data, err := reader.openPart(i, part, obj)
	if err != nil {
		return err
	}

	if _, err := dst.WriteAt(data, int64(part.Offset)); err != nil {
		return err
	}

	if r.Cache != nil {
		_ = r.Cache.Put(part.Ref, obj)
	}

	r.countBytes(ctx, &r.stats.BytesFromStore, "store", int64(len(data)))

	return nil
}
