package pack

import (
	"context"
	"errors"
	"fmt"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

// resurrect protects what a committing session relied on without storing
// it. It takes back the tombstones of every ref the session's objects
// reference outside its own archives, and when a collection sealed
// tombstones since the session began, it copies whatever those condemn
// under the skipped refs into the session.
func (ps *PackStorage) resurrect(ctx context.Context, ws *writeSession, commit *proto.Ref) error {
	skipped, err := ps.skippedRefs(ctx, ws, commit)
	if err != nil || len(skipped) == 0 {
		return err
	}

	// the un-tombstones are committed before the seals are read, so a
	// collection either reads them or sealed before
	untombs := newWriteSession(nil)
	for _, ref := range skipped {
		err := ps.withWritableArchive(ctx, untombs, func(a *archive) error {
			return a.putTombstone(ctx, proto.TombstoneRef(ref), false)
		})
		if err != nil {
			return fmt.Errorf("taking back the tombstones of what session %s relied on: %w", ws.id, err)
		}
	}

	if err := ps.flushSession(untombs); err != nil {
		return err
	}

	marker, err := ps.readBeginMarker(ws.id)
	if err != nil {
		return err
	}

	sealed, err := ps.sealedSince(marker.Sealed)
	if err != nil || len(sealed) == 0 {
		return err
	}

	copied, err := ps.copyCondemned(ctx, skipped, sealed)
	if err != nil {
		return err
	}

	if copied == 0 {
		return nil
	}

	ps.logger.Info("session copied what a collection condemned under it", "session", ws.id, "objects", copied)

	return ps.flushSession(ws)
}

// skippedRefs walks down from the commit while the objects are the
// session's own and returns the refs it reaches outside them: what the
// session deduplicated.
func (ps *PackStorage) skippedRefs(ctx context.Context, ws *writeSession, commit *proto.Ref) ([]*proto.Ref, error) {
	pending, err := ps.index.PendingArchives(ws.id)
	if err != nil {
		return nil, err
	}

	own := make(map[string]bool, len(pending))
	for _, name := range pending {
		own[name] = true
	}

	scope := ScopeOf(ctx)
	seen := map[string]bool{string(commit.Hash): true}
	frontier := []*proto.Ref{commit}

	var skipped []*proto.Ref

	for len(frontier) > 0 {
		found, err := ps.index.LocateCopies(frontier, scope)
		if err != nil {
			return nil, err
		}

		var next []*proto.Ref

		for _, ref := range frontier {
			loc, ours := ownCopy(found[string(ref.Hash)], own)
			if !ours {
				skipped = append(skipped, ref)
				continue
			}

			if proto.ObjectType(loc.Record.Type) == proto.ObjectType_BLOB {
				continue
			}

			obj, err := ps.Get(ctx, ref)
			if err != nil {
				return nil, fmt.Errorf("reading %x of session %s: %w", ref.Hash, ws.id, err)
			}

			for _, child := range backup.References(obj) {
				if !seen[string(child.Hash)] {
					seen[string(child.Hash)] = true
					next = append(next, child)
				}
			}
		}

		frontier = next
	}

	return skipped, nil
}

func ownCopy(locs []IndexLocation, own map[string]bool) (IndexLocation, bool) {
	for _, loc := range locs {
		if own[loc.Archive] {
			return loc, true
		}
	}

	return IndexLocation{}, false
}

// copyCondemned walks everything under the refs and copies into the
// session each object a sealed tombstone condemns. An object whose old
// copy is gone loses the session.
func (ps *PackStorage) copyCondemned(ctx context.Context, refs []*proto.Ref, sealed map[int64]bool) (int, error) {
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		seen[string(ref.Hash)] = true
	}

	scope := ScopeOf(ctx)
	copied := 0

	for frontier := refs; len(frontier) > 0; {
		condemned, err := ps.sealedTombstones(frontier, sealed)
		if err != nil {
			return 0, err
		}

		found, err := ps.index.LocateCopies(frontier, scope)
		if err != nil {
			return 0, err
		}

		var next []*proto.Ref

		for _, ref := range frontier {
			locs := found[string(ref.Hash)]
			if len(locs) == 0 {
				return 0, fmt.Errorf("%w: a collection took %x", backup.ErrSessionLost, ref.Hash)
			}

			blob := proto.ObjectType(locs[0].Record.Type) == proto.ObjectType_BLOB
			if blob && !condemned[string(ref.Hash)] {
				continue
			}

			obj, err := ps.Get(ctx, ref)
			if errors.Is(err, backup.ErrNotFound) {
				return 0, fmt.Errorf("%w: a collection took %x", backup.ErrSessionLost, ref.Hash)
			}

			if err != nil {
				return 0, err
			}

			if condemned[string(ref.Hash)] && obj.Type() != proto.ObjectType_COMMIT {
				if err := ps.put(ctx, obj); err != nil {
					return 0, err
				}

				copied++
			}

			for _, child := range backup.References(obj) {
				if !seen[string(child.Hash)] {
					seen[string(child.Hash)] = true
					next = append(next, child)
				}
			}
		}

		frontier = next
	}

	return copied, nil
}

// sealedTombstones reports which of refs a tombstone of a sealed version
// condemns.
func (ps *PackStorage) sealedTombstones(refs []*proto.Ref, sealed map[int64]bool) (map[string]bool, error) {
	tombs := make([]*proto.Ref, len(refs))
	for i, ref := range refs {
		tombs[i] = proto.TombstoneRef(ref)
	}

	found, err := ps.index.LocateCopies(tombs, Scope{})
	if err != nil {
		return nil, err
	}

	condemned := make(map[string]bool)

	for i, ref := range refs {
		for _, loc := range found[string(tombs[i].Hash)] {
			a, err := ps.archiveByName(loc.Archive)
			if err != nil && !errors.Is(err, errArchiveRetired) {
				return nil, err
			}

			if a != nil && sealed[a.version(loc.Record).Time.UnixNano()] {
				condemned[string(ref.Hash)] = true
			}
		}
	}

	return condemned, nil
}
