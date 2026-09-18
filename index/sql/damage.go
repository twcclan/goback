package sql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/index/sql/ent/commitrow"
	"github.com/twcclan/goback/index/sql/ent/damagedpath"
	"github.com/twcclan/goback/index/sql/ent/file"
	"github.com/twcclan/goback/index/sql/ent/predicate"
	"github.com/twcclan/goback/index/sql/ent/set"
	"github.com/twcclan/goback/proto"
)

// PathsOfFiles returns where the given file objects are stored, by set
// and path. A file object no live version references any more yields
// nothing.
func (x *Index) PathsOfFiles(ctx context.Context, refs []*proto.Ref) ([]backup.FilePath, error) {
	raw := make([][]byte, 0, len(refs))
	for _, ref := range refs {
		raw = append(raw, ref.GetHash())
	}

	rows, err := x.client.File.Query().Where(file.RefIn(raw...)).Order(ent.Asc(file.FieldPath)).All(ctx)
	if err != nil {
		return nil, err
	}

	names := make(map[int64]string)
	seen := make(map[string]bool)

	var out []backup.FilePath
	for _, row := range rows {
		name, ok := names[row.SetID]
		if !ok {
			s, err := x.client.Set.Get(ctx, row.SetID)
			if err != nil {
				return nil, err
			}

			name, names[row.SetID] = s.Name, s.Name
		}

		key := name + "\x00" + row.Path + "\x00" + string(row.Ref)
		if seen[key] {
			continue
		}

		seen[key] = true
		out = append(out, backup.FilePath{
			Ref: &proto.Ref{Hash: row.Ref}, Set: name, Path: row.Path,
			Open: row.ValidUntil == nil,
		})
	}

	return out, nil
}

// MarkDamaged records that the paths of a set hold content the store lost,
// so the next backup reads them again. An unknown set is ignored, since a
// set that is gone has nothing to repair.
func (x *Index) MarkDamaged(ctx context.Context, name string, paths []string) error {
	s, err := x.client.Set.Query().Where(set.Name(name)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	now := x.now()
	for _, path := range paths {
		err := x.client.DamagedPath.Create().SetSetID(s.ID).SetPath(path).SetFoundAt(now).
			OnConflictColumns(damagedpath.FieldSetID, damagedpath.FieldPath).
			UpdateFoundAt().Exec(ctx)
		if err != nil {
			return fmt.Errorf("marking %q of set %q damaged: %w", path, name, err)
		}
	}

	return nil
}

// MarkRescan records that a whole set must be read again, for damage no
// path could be resolved for.
func (x *Index) MarkRescan(ctx context.Context, name string) error {
	n, err := x.client.Set.Update().Where(set.Name(name)).SetRescan(true).Save(ctx)
	if err != nil {
		return err
	}

	if n == 0 {
		return fmt.Errorf("%w: set %q", backup.ErrNotFound, name)
	}

	return nil
}

// damage returns what a run on the set must read again regardless of what
// its change detection says.
func (x *Index) damage(ctx context.Context, s *ent.Set) (rescan bool, paths []string, err error) {
	if s.Rescan {
		return true, nil, nil
	}

	rows, err := x.client.DamagedPath.Query().Where(damagedpath.SetID(s.ID)).
		Order(ent.Asc(damagedpath.FieldPath)).All(ctx)
	if err != nil {
		return false, nil, err
	}

	for _, row := range rows {
		paths = append(paths, row.Path)
	}

	return false, paths, nil
}

// clearDamage forgets what a completed run has read again. A checkpoint
// leaves it, because it did not cover the whole set.
func (x *Index) clearDamage(ctx context.Context, tx *ent.Tx, setID int64, partial bool, scanStart int64) error {
	if partial {
		return nil
	}

	s, err := tx.Set.Get(ctx, setID)
	if err != nil {
		return err
	}

	if s.Rescan {
		err = tx.Set.UpdateOneID(setID).SetRescan(false).Exec(ctx)
		if err != nil {
			return err
		}
	}

	found := x.now()
	if scanStart > 0 {
		found = time.Unix(0, scanStart).UTC()
	}

	// damage found after this run started is not covered by it
	_, err = tx.DamagedPath.Delete().
		Where(damagedpath.SetID(setID), damagedpath.FoundAtLTE(found)).Exec(ctx)
	if err != nil {
		return err
	}

	// the run stored the open versions again, so what was lost of them is
	// back; a closed version keeps its mark, since nothing can produce it
	return tx.File.Update().
		Where(file.SetID(setID), file.Lost(true), file.ValidUntilIsNil()).
		SetLost(false).Exec(ctx)
}

// MarkLost records that stored versions cannot be read any more. Every
// commit whose range covers one holds a file it cannot restore.
func (x *Index) MarkLost(ctx context.Context, versions []backup.FilePath) error {
	ids := make(map[string]int64)

	for _, v := range versions {
		id, ok := ids[v.Set]
		if !ok {
			s, err := x.client.Set.Query().Where(set.Name(v.Set)).Only(ctx)
			if ent.IsNotFound(err) {
				continue
			}

			if err != nil {
				return err
			}

			id, ids[v.Set] = s.ID, s.ID
		}

		err := x.client.File.Update().
			Where(file.SetID(id), file.Path(v.Path), file.RefEQ(v.Ref.GetHash())).
			SetLost(true).Exec(ctx)
		if err != nil {
			return fmt.Errorf("marking %q of set %q lost: %w", v.Path, v.Set, err)
		}
	}

	return nil
}

// LostVersion is a stored version the store cannot read, with the live
// commits that hold it.
type LostVersion struct {
	Path    string
	Ref     *proto.Ref
	Commits []*proto.Ref
}

// LostVersions returns the versions of a set the store cannot read and the
// commits each one damages. A restore of those commits cannot produce that
// path.
func (x *Index) LostVersions(ctx context.Context, name string) ([]LostVersion, error) {
	setID, err := findSet(ctx, x.client, name)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	rows, err := x.client.File.Query().
		Where(file.SetID(setID), file.Lost(true)).
		Order(ent.Asc(file.FieldPath), ent.Asc(file.FieldValidFrom)).All(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]LostVersion, 0, len(rows))
	for _, row := range rows {
		held := []predicate.CommitRow{
			commitrow.SetID(setID),
			commitrow.ReceivedAtGTE(row.ValidFrom),
			liveCommit(),
		}

		if row.ValidUntil != nil {
			held = append(held, commitrow.ReceivedAtLT(*row.ValidUntil))
		}

		commits, err := x.client.CommitRow.Query().Where(held...).
			Order(ent.Desc(commitrow.FieldReceivedAt)).All(ctx)
		if err != nil {
			return nil, err
		}

		lost := LostVersion{Path: row.Path, Ref: &proto.Ref{Hash: row.Ref}}
		for _, c := range commits {
			lost.Commits = append(lost.Commits, &proto.Ref{Hash: c.Ref})
		}

		out = append(out, lost)
	}

	return out, nil
}
