package pack

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/bits-and-blooms/bitset"
	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
)

type compactionGroup struct {
	candidates []*archive
	total      uint64

	// keep, when set, decides whether a candidate's object survives the
	// rewrite; marked, when set, says whether a copy found elsewhere was
	// reachable and may replace the candidate's.
	keep   func(candidate *archive, hdr *proto.ObjectHeader) bool
	marked func(loc *IndexLocation) bool

	droppedObjects uint64
	droppedBytes   uint64
}

// Compact rewrites the small committed archives into full-sized archives at
// the root and returns when it is done.
func (ps *PackStorage) Compact() error {
	return ps.doCompaction()
}

func (ps *PackStorage) doCompaction() error {
	ps.compactorMtx.Lock()
	defer ps.compactorMtx.Unlock()

	group := &compactionGroup{}

	ps.mtx.RLock()
	for _, candidate := range ps.archives {
		candidate.mtx.RLock()
		eligible := candidate.readOnly && candidate.state == ArchiveCommitted && candidate.size < ps.maxSize
		candidate.mtx.RUnlock()

		if !eligible {
			continue
		}

		group.candidates = append(group.candidates, candidate)
		group.total += candidate.size
	}
	ps.mtx.RUnlock()

	if len(group.candidates) > ps.compaction.MinimumCandidates || group.total >= ps.maxSize {
		ps.logger.Info("compacting archives", "count", len(group.candidates), "size", humanize.Bytes(group.total))

		return ps.compactGroup(context.Background(), group)
	}

	return nil
}

// compactGroup rewrites the group's candidates, several at once. An object
// with a usable copy committed outside the group is dropped; the rest is
// copied into root archives with its timestamp kept.
func (ps *PackStorage) compactGroup(ctx context.Context, group *compactionGroup) error {
	ctx, span := tracer.Start(ctx, "PackStorage.rewrite")
	defer span.End()

	rw := &rewrite{started: time.Now(), ps: ps, group: group, inGroup: make(map[string]bool, len(group.candidates)),
		written: make(map[string]bool), unmarked: make(map[string]uint64)}
	for _, candidate := range group.candidates {
		rw.inGroup[candidate.name] = true
	}

	workers := ps.compaction.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}

	span.SetAttributes(attribute.Int("candidates", len(group.candidates)), attribute.Int("workers", workers))

	queue := make(chan *archive)
	grp, gctx := errgroup.WithContext(ctx)

	grp.Go(func() error {
		defer close(queue)

		for _, candidate := range group.candidates {
			select {
			case queue <- candidate:
			case <-gctx.Done():
				return gctx.Err()
			}
		}

		return nil
	})

	for range min(workers, len(group.candidates)) {
		grp.Go(func() error {
			out := &rewriteOutput{rw: rw}

			for candidate := range queue {
				if err := rw.candidate(gctx, out, candidate); err != nil {
					out.abort()
					return err
				}
			}

			if err := gctx.Err(); err != nil {
				out.abort()
				return err
			}

			return out.finish()
		})
	}

	if err := grp.Wait(); err != nil {
		span.RecordError(err)
		return err
	}

	span.SetAttributes(attribute.Int64("dropped_objects", int64(group.droppedObjects)), attribute.Int64("copied_bytes", int64(rw.copied)))

	ps.logger.Info("rewrote archives", "archives", len(group.candidates), "dropped", group.droppedObjects, "saved", humanize.Bytes(group.droppedBytes),
		"copied", humanize.Bytes(rw.copied), "took", time.Since(rw.started).Round(time.Second))

	if err := ps.carryErasureClock(rw.obsolete, rw.outputs, rw.unmarked); err != nil {
		return err
	}

	// the outputs are indexed, so readers that still land on an obsolete
	// archive find their copy elsewhere once it is retired
	for _, archive := range rw.obsolete {
		idx, err := archive.getIndex()
		if err != nil {
			ps.logger.Warn("reading the index of an obsolete archive failed", "archive", archive.name, "err", err)
			continue
		}

		ps.retireArchive(archive)

		if e := archive.Close(); e != nil {
			ps.logger.Warn("closing obsolete archive failed", "archive", archive.name, "err", e)
		}

		err = ps.index.DeleteArchive(archive.name, idx)
		if err != nil {
			ps.logger.Warn("removing local index failed", "archive", archive.name, "err", err)
		}

		ps.deleteArchiveFiles(archive.name)
	}

	return nil
}

// rewrite is the state the workers of one compactGroup share.
type rewrite struct {
	ps      *PackStorage
	group   *compactionGroup
	inGroup map[string]bool

	started time.Time

	mtx      sync.Mutex
	written  map[string]bool
	outputs  []*archive
	obsolete []*archive
	copied   uint64
	// unmarked names the copied objects their input's last mark result
	// left unreachable, and that result's generation
	unmarked map[string]uint64
}

// progressEvery is how many rewritten archives pass between progress logs.
const progressEvery = 500

// claim reports whether the caller is the first to write the object, and
// records the mark the input's last generation gave it.
func (rw *rewrite) claim(hash []byte, mark *gcFile, pos int) bool {
	rw.mtx.Lock()
	defer rw.mtx.Unlock()

	if rw.written[string(hash)] {
		return false
	}

	rw.written[string(hash)] = true

	if mark != nil && pos >= 0 && !mark.Current.Test(uint(pos)) {
		rw.unmarked[string(hash)] = mark.Generation
	}

	return true
}

// elsewhere reports whether one of the copies outside the group may stand
// in for the candidate's.
func (rw *rewrite) elsewhere(copies []IndexLocation) bool {
	for i := range copies {
		if rw.inGroup[copies[i].Archive] {
			continue
		}

		if rw.group.marked == nil || rw.group.marked(&copies[i]) {
			return true
		}
	}

	return false
}

// candidate copies what survives of one candidate into out.
func (rw *rewrite) candidate(ctx context.Context, out *rewriteOutput, candidate *archive) error {
	started := time.Now()
	var copied uint64

	idx, err := candidate.getIndex()
	if err != nil {
		return errors.Wrapf(err, "reading the index of %s", candidate.name)
	}

	refs := make([]*proto.Ref, len(idx))
	for i := range idx {
		refs[i] = &proto.Ref{Hash: idx[i].Sum[:]}
	}

	copies, err := rw.ps.index.LocateCopies(refs)
	if err != nil {
		return errors.Wrapf(err, "locating the objects of %s", candidate.name)
	}

	mark := candidate.gcResult()

	err = candidate.foreach(loadAll, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		if rw.group.keep != nil && !rw.group.keep(candidate, hdr) {
			atomic.AddUint64(&rw.group.droppedObjects, 1)
			atomic.AddUint64(&rw.group.droppedBytes, uint64(length))

			return nil
		}

		if rw.elsewhere(copies[string(hdr.Ref.Hash)]) {
			return nil
		}

		// a copy between archives is a trust boundary: never carry a
		// corrupted payload forward under a valid ref
		if err := proto.VerifyStored(hdr, bytes); err != nil {
			return errors.Wrapf(err, "object %x in archive %s", hdr.Ref.Hash, candidate.name)
		}

		if !rw.claim(hdr.Ref.Hash, mark, idx.position(hdr.Ref.Hash)) {
			return nil
		}

		ar, err := out.archive()
		if err != nil {
			return err
		}

		copied += uint64(length)

		return ar.putRaw(ctx, hdr, bytes)
	})
	if err != nil {
		return err
	}

	rewriteArchives.Add(ctx, 1)
	rewriteCopied.Add(ctx, int64(copied))
	rewriteDuration.Record(ctx, time.Since(started).Seconds())

	rw.mtx.Lock()
	rw.obsolete = append(rw.obsolete, candidate)
	rw.copied += copied
	done, total := len(rw.obsolete), len(rw.group.candidates)
	rw.mtx.Unlock()

	if done%progressEvery == 0 {
		elapsed := time.Since(rw.started)
		rw.ps.logger.Info("rewriting archives", "done", done, "of", total, "elapsed", elapsed.Round(time.Second),
			"left", (elapsed / time.Duration(done) * time.Duration(total-done)).Round(time.Second))
	}

	return nil
}

// rewriteOutput is the root archive one worker writes into.
type rewriteOutput struct {
	rw   *rewrite
	open *archive
}

func (o *rewriteOutput) archive() (*archive, error) {
	ps := o.rw.ps

	if o.open != nil && o.open.size >= ps.maxSize {
		if err := o.finish(); err != nil {
			return nil, err
		}
	}

	if o.open == nil {
		a, err := newArchive(ps.storage, "", ps.atRest, ps.logger)
		if err != nil {
			return nil, err
		}

		a.stored = ps.archiveStored
		o.open = a
	}

	return o.open, nil
}

// finish closes and indexes the open archive, if any.
func (o *rewriteOutput) finish() error {
	if o.open == nil {
		return nil
	}

	a, ps := o.open, o.rw.ps
	o.open = nil

	index, err := a.CloseWriter()
	if err != nil {
		return errors.Wrap(err, "closing compaction output")
	}

	if err := ps.index.IndexArchive(ArchiveInfo{Name: a.name}, index); err != nil {
		return err
	}

	a.releaseWriteIndex()

	ps.mtx.Lock()
	ps.archives = append(ps.archives, a)
	ps.mtx.Unlock()

	o.rw.mtx.Lock()
	o.rw.outputs = append(o.rw.outputs, a)
	o.rw.mtx.Unlock()

	return nil
}

func (o *rewriteOutput) abort() {
	if o.open != nil {
		_ = o.open.Close()
		o.open = nil
	}
}

// carryErasureClock seeds the outputs of a rewrite with the mark of the
// newest generation among its inputs, so that an unreachable object keeps
// counting towards its second unmarked generation, and with the earliest
// DeadSince of the inputs, so the erasure bound keeps counting from when
// the objects first went unmarked. An object whose input was last marked
// in an older generation counts as reachable.
func (ps *PackStorage) carryErasureClock(inputs, outputs []*archive, unmarked map[string]uint64) error {
	var since time.Time
	var generation uint64
	var snapshot time.Time

	for _, a := range inputs {
		g := a.gcResult()
		if g == nil {
			continue
		}

		if g.Generation > generation {
			generation, snapshot = g.Generation, g.Snapshot
		}

		if !g.DeadSince.IsZero() && (since.IsZero() || g.DeadSince.Before(since)) {
			since = g.DeadSince
		}
	}

	if generation == 0 {
		return nil
	}

	for _, a := range outputs {
		idx, err := a.getIndex()
		if err != nil {
			return errors.Wrapf(err, "reading index of %s", a.name)
		}

		seed := &gcFile{
			Generation: generation,
			Snapshot:   snapshot,
			Current:    bitset.New(uint(len(idx))).SetAll(),
		}

		for pos, rec := range idx {
			if g, ok := unmarked[string(rec.Sum[:])]; !ok || g != generation {
				continue
			}

			seed.Current.Clear(uint(pos))
			seed.DeadObjects++
			seed.DeadBytes += uint64(rec.Length)
			seed.Dead = append(seed.Dead, prefixOf(rec.Sum[:]))
		}

		if seed.DeadObjects > 0 {
			sort.Slice(seed.Dead, func(i, j int) bool { return seed.Dead[i] < seed.Dead[j] })

			seed.DeadSince = since
			if seed.DeadSince.IsZero() {
				seed.DeadSince = snapshot
			}
		} else if !since.IsZero() {
			seed.DeadSince = since
		}

		if err := writeGCFile(ps.storage, a.name, seed); err != nil {
			return errors.Wrapf(err, "writing gc result of %s", a.name)
		}

		a.setGCResult(seed)
	}

	return nil
}
