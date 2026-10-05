package sql

import (
	"context"
	"fmt"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/index"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/index/sql/ent/commitrow"
	"github.com/twcclan/goback/index/sql/ent/pin"
	"github.com/twcclan/goback/index/sql/ent/set"
	"github.com/twcclan/goback/storage/pack"
)

// findSet returns the id of the named set, or backup.ErrNotFound.
func findSet(ctx context.Context, c *ent.Client, name string) (int64, error) {
	id, err := c.Set.Query().Where(set.Name(name)).OnlyID(ctx)
	if ent.IsNotFound(err) {
		return 0, fmt.Errorf("%w: set %q", backup.ErrNotFound, name)
	}

	return id, err
}

// ensureSet returns the id of the named set, creating it if needed. A
// non-zero wantID finds or recreates the set under the id its commits
// carry, which a rebuild after index loss needs, so same-named sets stay
// apart; a recreated set comes up with retention paused until an operator
// sets its policy. The name decides only when that id cannot be used.
func ensureSet(ctx context.Context, c *ent.Client, name string, wantID int64) (int64, error) {
	if wantID != 0 {
		s, err := c.Set.Get(ctx, wantID)
		if ent.IsNotFound(err) {
			s, err = c.Set.Create().SetID(wantID).SetName(name).SetRetentionPaused(true).Save(ctx)
		}

		if err == nil {
			return s.ID, nil
		}

		if !ent.IsConstraintError(err) {
			return 0, err
		}
	}

	id, err := c.Set.Query().Where(set.Name(name)).OnlyID(ctx)
	if !ent.IsNotFound(err) {
		return id, err
	}

	created, err := c.Set.Create().SetName(name).Save(ctx)
	if err == nil {
		return created.ID, nil
	}

	// another commit created the set first
	if !ent.IsConstraintError(err) {
		return 0, err
	}

	return c.Set.Query().Where(set.Name(name)).OnlyID(ctx)
}

// lockSet takes the set's row lock for the transaction.
func (x *Index) lockSet(ctx context.Context, tx *ent.Tx, setID int64) (*ent.Set, error) {
	s, err := forUpdate(x, tx.Set.Query().Where(set.ID(setID))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, fmt.Errorf("%w: set %d", backup.ErrNotFound, setID)
	}

	return s, err
}

// BeginCommit implements backup.CommitGate: the set is created if needed,
// it must be active, and the grant carries the store policy.
func (x *Index) BeginCommit(ctx context.Context, name string) (*backup.CommitGrant, error) {
	if _, ok := backup.SetID(name); ok {
		return nil, fmt.Errorf("%w: %q", backup.ErrSetName, name)
	}

	setID, err := ensureSet(ctx, x.client, name, 0)
	if err != nil {
		return nil, err
	}

	s, err := x.client.Set.Get(ctx, setID)
	if err != nil {
		return nil, err
	}

	if s.State != set.StateActive {
		return nil, fmt.Errorf("%w: set %q", backup.ErrSetClosed, name)
	}

	grant := &backup.CommitGrant{}

	grant.Policy, err = x.StorePolicy(ctx)
	if err != nil {
		return nil, err
	}

	grant.Rescan, grant.Damaged, err = x.damage(ctx, s)
	if err != nil {
		return nil, err
	}

	return grant, nil
}

// ListSets returns every set by name, each with the size of its newest
// live commit and the sizes of all of them added up.
func (x *Index) ListSets(ctx context.Context) ([]index.SetInfo, error) {
	rows, err := x.client.Set.Query().Order(ent.Asc(set.FieldName)).All(ctx)
	if err != nil {
		return nil, err
	}

	out := mapAll(rows, m.Set)
	for i := range out {
		out[i].LogicalSize, err = x.latestSize(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}

		out[i].KeptLogicalSize, err = x.keptSize(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}

		out[i].PhysicalSize = deref(rows[i].PhysicalSize)
		out[i].DeduplicatedSize = deref(rows[i].DeduplicatedSize)
		out[i].AloneSize = deref(rows[i].AloneSize)
		out[i].ExclusiveSize = deref(rows[i].ExclusiveSize)
	}

	return out, nil
}

// RootOwner returns the lookup a garbage collection attributes with: it
// answers a root, a commit or a pin holding one, with the set it belongs
// to and the group that set is collected in, and anything else with
// nothing.
func (x *Index) RootOwner(ctx context.Context) (func(root []byte) pack.Attribution, error) {
	var groups map[int64]int64
	if x.Grouping != nil {
		var err error
		if groups, err = x.Grouping(ctx); err != nil {
			return nil, err
		}
	}

	commits, err := x.client.CommitRow.Query().Select(commitrow.FieldRef, commitrow.FieldSetID).All(ctx)
	if err != nil {
		return nil, err
	}

	owners := make(map[string]int64, len(commits))
	for _, row := range commits {
		owners[string(row.Ref)] = row.SetID
	}

	pins, err := x.client.Pin.Query().Select(pin.FieldRef, pin.FieldTarget).All(ctx)
	if err != nil {
		return nil, err
	}

	for _, row := range pins {
		if owner, ok := owners[string(row.Target)]; ok {
			owners[string(row.Ref)] = owner
		}
	}

	return func(root []byte) pack.Attribution {
		set, ok := owners[string(root)]
		if !ok {
			return pack.Attribution{}
		}

		return pack.Attribution{Group: groups[set], Set: set}
	}, nil
}

// RecordSetSizes records what a garbage collection attributed to each set:
// the physical, deduplicated, alone and exclusive sizes; a set it did not
// name holds nothing of its own.
func (x *Index) RecordSetSizes(ctx context.Context, report *pack.CollectReport) error {
	return x.tx(ctx, func(tx *ent.Tx) error {
		err := tx.Set.Update().ClearPhysicalSize().ClearDeduplicatedSize().ClearAloneSize().ClearExclusiveSize().Exec(ctx)
		if err != nil {
			return err
		}

		for id, size := range report.SetBytes {
			err = tx.Set.UpdateOneID(id).SetPhysicalSize(int64(size)).SetDeduplicatedSize(int64(report.SetDeduplicated[id])).
				SetAloneSize(int64(report.SetAlone[id])).SetExclusiveSize(int64(report.SetExclusive[id])).Exec(ctx)
			if ent.IsNotFound(err) {
				continue
			}

			if err != nil {
				return err
			}
		}

		return nil
	})
}

func (x *Index) keptSize(ctx context.Context, setID int64) (int64, error) {
	var sums []struct {
		Sum *int64 `sql:"sum"`
	}

	err := x.client.CommitRow.Query().Where(commitrow.SetID(setID), liveCommit()).
		Aggregate(ent.Sum(commitrow.FieldLogicalSize)).Scan(ctx, &sums)
	if err != nil || len(sums) == 0 {
		return 0, err
	}

	return deref(sums[0].Sum), nil
}

func (x *Index) latestSize(ctx context.Context, setID int64) (int64, error) {
	newest, err := x.client.CommitRow.Query().Where(commitrow.SetID(setID), liveCommit()).Order(ent.Desc(commitrow.FieldReceivedAt)).First(ctx)
	if ent.IsNotFound(err) {
		return 0, nil
	}

	if err != nil {
		return 0, err
	}

	return deref(newest.LogicalSize), nil
}
