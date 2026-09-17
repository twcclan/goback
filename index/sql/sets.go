package sql

import (
	"context"
	"fmt"

	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/index"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/index/sql/ent/commitrow"
	"github.com/twcclan/goback/index/sql/ent/set"
)

// findSet returns the id of the named set, or backup.ErrNotFound.
func findSet(ctx context.Context, c *ent.Client, name string) (int64, error) {
	id, err := c.Set.Query().Where(set.Name(name)).OnlyID(ctx)
	if ent.IsNotFound(err) {
		return 0, fmt.Errorf("%w: set %q", backup.ErrNotFound, name)
	}

	return id, err
}

// ensureSet returns the id of the named set for a commit by agentID,
// creating the set owned by that agent if needed. A set belongs to the
// agent of its first commit; in strict mode a commit by any other agent
// fails with backup.ErrSetOwned, otherwise it is indexed regardless. A
// non-zero wantID recreates a set under the id its commits carry, which a
// rebuild after index loss needs; such a set comes up with retention
// paused until an operator sets its policy.
func ensureSet(ctx context.Context, c *ent.Client, name, agentID string, wantID int64, strict bool) (int64, error) {
	s, err := c.Set.Query().Where(set.Name(name)).Only(ctx)
	if ent.IsNotFound(err) {
		create := c.Set.Create().SetName(name).SetNillableAgentID(nilIfZero(agentID))
		if wantID != 0 {
			create.SetID(wantID).SetRetentionPaused(true)
		}

		var created *ent.Set
		created, err = create.Save(ctx)
		if err == nil {
			return created.ID, nil
		}

		// another commit created the set first; judge it like any other
		if !ent.IsConstraintError(err) {
			return 0, err
		}

		s, err = c.Set.Query().Where(set.Name(name)).Only(ctx)
	}

	if err != nil {
		return 0, err
	}

	owner := deref(s.AgentID)
	switch {
	case owner == "" && agentID != "":
		_, err = c.Set.Update().Where(set.ID(s.ID), set.AgentIDIsNil()).SetAgentID(agentID).Save(ctx)
	case owner != "" && owner != agentID && strict:
		err = fmt.Errorf("%w: set %q belongs to agent %q, commit is by %q", backup.ErrSetOwned, name, owner, agentID)
	}

	return s.ID, err
}

// lockSet takes the set's row lock for the transaction.
func (x *Index) lockSet(ctx context.Context, tx *ent.Tx, setID int64) (*ent.Set, error) {
	return forUpdate(x, tx.Set.Query().Where(set.ID(setID))).Only(ctx)
}

// BeginCommit implements backup.CommitGate: the set is created or its
// ownership checked, it must be active, and the grant carries the store
// policy.
func (x *Index) BeginCommit(ctx context.Context, name string) (*backup.CommitGrant, error) {
	p, err := auth.Require(ctx)
	if err != nil {
		return nil, err
	}

	setID, err := ensureSet(ctx, x.client, name, p.AgentID, 0, true)
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

	grant := &backup.CommitGrant{SetID: uint64(setID)}

	grant.Policy, err = x.StorePolicy(ctx)
	if err != nil {
		return nil, err
	}

	return grant, nil
}

// ListSets returns every set by name, each with the size of its newest
// live commit.
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
	}

	return out, nil
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

// TransferSet makes agentID the owner of a set; empty releases it.
func (x *Index) TransferSet(ctx context.Context, name, agentID string) error {
	update := x.client.Set.Update().Where(set.Name(name))
	if agentID == "" {
		update.ClearAgentID()
	} else {
		update.SetAgentID(agentID)
	}

	n, err := update.Save(ctx)
	if err != nil {
		return err
	}

	if n == 0 {
		return fmt.Errorf("%w: set %q", backup.ErrNotFound, name)
	}

	return nil
}
