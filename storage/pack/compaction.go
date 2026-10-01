package pack

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/bits-and-blooms/bitset"
	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
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
	rw := &rewrite{ps: ps, group: group, inGroup: make(map[string]bool, len(group.candidates)), written: make(map[string]bool)}
	for _, candidate := range group.candidates {
		rw.inGroup[candidate.name] = true
	}

	workers := ps.compaction.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}

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
		return err
	}

	ps.logger.Info("compaction dropped objects", "objects", group.droppedObjects, "saved", humanize.Bytes(group.droppedBytes))

	if err := ps.carryErasureClock(rw.obsolete, rw.outputs); err != nil {
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

	mtx      sync.Mutex
	written  map[string]bool
	outputs  []*archive
	obsolete []*archive
}

// claim reports whether the caller is the first to write the object.
func (rw *rewrite) claim(hash []byte) bool {
	rw.mtx.Lock()
	defer rw.mtx.Unlock()

	if rw.written[string(hash)] {
		return false
	}

	rw.written[string(hash)] = true

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

		if !rw.claim(hdr.Ref.Hash) {
			return nil
		}

		ar, err := out.archive()
		if err != nil {
			return err
		}

		return ar.putRaw(ctx, hdr, bytes)
	})
	if err != nil {
		return err
	}

	rw.mtx.Lock()
	rw.obsolete = append(rw.obsolete, candidate)
	rw.mtx.Unlock()

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

// carryErasureClock seeds the outputs of a rewrite with the earliest
// DeadSince of its inputs, so the erasure bound keeps counting from when
// the objects first went unmarked rather than from the rewrite.
func (ps *PackStorage) carryErasureClock(inputs, outputs []*archive) error {
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

	if since.IsZero() {
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
			DeadSince:  since,
		}

		if err := writeGCFile(ps.storage, a.name, seed); err != nil {
			return errors.Wrapf(err, "writing gc result of %s", a.name)
		}

		a.setGCResult(seed)
	}

	return nil
}
