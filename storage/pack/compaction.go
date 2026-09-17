package pack

import (
	"context"
	"log"
	"sort"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/bits-and-blooms/bitset"
	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
)

type compactionGroup struct {
	placement  Placement
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

// Compact rewrites the small committed archives of every placement group
// into full-sized archives of that group and returns when it is done.
func (ps *PackStorage) Compact() error {
	return ps.doCompaction()
}

func (ps *PackStorage) doCompaction() error {
	ps.compactorMtx.Lock()
	defer ps.compactorMtx.Unlock()

	groups := make(map[string]*compactionGroup)

	ps.mtx.RLock()
	for _, candidate := range ps.archives {
		candidate.mtx.RLock()
		eligible := candidate.readOnly && candidate.state == ArchiveCommitted && candidate.size < ps.maxSize
		candidate.mtx.RUnlock()

		if !eligible {
			continue
		}

		placement := ParsePlacement(candidate.name).Group()
		group, ok := groups[placement.Dir()]
		if !ok {
			group = &compactionGroup{placement: placement}
			groups[placement.Dir()] = group
		}

		group.candidates = append(group.candidates, candidate)
		group.total += candidate.size
	}
	ps.mtx.RUnlock()

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		group := groups[key]

		if len(group.candidates) > ps.compaction.MinimumCandidates || group.total >= ps.maxSize {
			log.Printf("Compacting %d archives with %s total size under %q", len(group.candidates), humanize.Bytes(group.total), key)

			err := ps.compactGroup(context.Background(), group)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// compactGroup rewrites the group's candidates. An object found committed
// elsewhere is dropped; the rest is copied into the group with its
// timestamp kept.
func (ps *PackStorage) compactGroup(ctx context.Context, group *compactionGroup) error {
	written := make(map[string]bool)
	open := make(map[string]*archive)

	var outputs []*archive

	closeArchive := func(a *archive, placement Placement) error {
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

	getArchive := func(placement Placement) (*archive, error) {
		dir := placement.Dir()

		if a := open[dir]; a != nil && a.size >= ps.maxSize {
			err := closeArchive(a, placement)
			if err != nil {
				return nil, err
			}

			delete(open, dir)
		}

		if open[dir] == nil {
			a, err := newArchive(ps.storage, dir, ps.atRest)
			if err != nil {
				return nil, err
			}

			open[dir] = a
		}

		return open[dir], nil
	}

	abort := func() {
		for _, a := range open {
			_ = a.Close()
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
			err = verifyStored(hdr, bytes)
			if err != nil {
				return errors.Wrapf(err, "object %x in archive %s", hdr.Ref.Hash, candidate.name)
			}

			ar, err := getArchive(group.placement)
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

	log.Printf("Dropped %d objects during compaction, saved %s", group.droppedObjects, humanize.Bytes(group.droppedBytes))

	for dir, a := range open {
		err := closeArchive(a, ParsePlacement(a.name))
		if err != nil {
			return errors.Wrapf(err, "closing compaction output under %q", dir)
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
			log.Printf("Failed getting archive index for deletion: %s", err)
			continue
		}

		ps.retireArchive(archive)

		if e := archive.Close(); e != nil {
			log.Printf("Failed closing obsolete archive after compaction: %v", e)
		}

		err = ps.index.DeleteArchive(archive.name, idx)
		if err != nil {
			log.Printf("Failed removing local index %s after compaction: %s", archive.name, err)
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
