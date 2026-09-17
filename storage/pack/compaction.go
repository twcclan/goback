package pack

import (
	"context"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/bits-and-blooms/bitset"
	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
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

// compactGroup rewrites the group's candidates. An object found committed
// elsewhere is dropped; the rest is copied into root archives with its
// timestamp kept.
func (ps *PackStorage) compactGroup(ctx context.Context, group *compactionGroup) error {
	written := make(map[string]bool)

	var open *archive
	var outputs []*archive

	closeArchive := func(a *archive) error {
		index, err := a.CloseWriter()
		if err != nil {
			return err
		}

		err = ps.index.IndexArchive(ArchiveInfo{Name: a.name}, index)
		if err != nil {
			return err
		}

		a.releaseWriteIndex()

		ps.mtx.Lock()
		ps.archives = append(ps.archives, a)
		ps.mtx.Unlock()

		outputs = append(outputs, a)

		return nil
	}

	getArchive := func() (*archive, error) {
		if open != nil && open.size >= ps.maxSize {
			err := closeArchive(open)
			if err != nil {
				return nil, err
			}

			open = nil
		}

		if open == nil {
			a, err := newArchive(ps.storage, "", ps.atRest, ps.logger)
			if err != nil {
				return nil, err
			}

			open = a
		}

		return open, nil
	}

	abort := func() {
		if open != nil {
			_ = open.Close()
		}
	}

	var obsolete []*archive

	for _, candidate := range group.candidates {
		err := candidate.foreach(loadAll, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
			key := string(hdr.Ref.Hash)

			if group.keep != nil && !group.keep(candidate, hdr) {
				group.droppedObjects++
				group.droppedBytes += uint64(length)

				return nil
			}

			if written[key] {
				return nil
			}

			loc, err := ps.indexLocationExcept(hdr.Ref, group.candidates...)
			if err != nil {
				return err
			}

			if loc != nil && group.marked != nil && !group.marked(loc) {
				loc = nil
			}

			if loc != nil {
				return nil
			}

			// a copy between archives is a trust boundary: never carry
			// a corrupted payload forward under a valid ref
			err = proto.VerifyStored(hdr, bytes)
			if err != nil {
				return errors.Wrapf(err, "object %x in archive %s", hdr.Ref.Hash, candidate.name)
			}

			ar, err := getArchive()
			if err != nil {
				return err
			}

			written[key] = true

			return ar.putRaw(ctx, hdr, bytes)
		})

		if err != nil {
			abort()
			return err
		}

		obsolete = append(obsolete, candidate)
	}

	ps.logger.Info("compaction dropped objects", "objects", group.droppedObjects, "saved", humanize.Bytes(group.droppedBytes))

	if open != nil {
		err := closeArchive(open)
		if err != nil {
			return errors.Wrap(err, "closing compaction output")
		}
	}

	if err := ps.carryErasureClock(obsolete, outputs); err != nil {
		return err
	}

	// the outputs are indexed, so readers that still land on an obsolete
	// archive find their copy elsewhere once it is retired
	for _, archive := range obsolete {
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
