package sql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/index"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/index/sql/ent/commitrow"
	"github.com/twcclan/goback/index/sql/ent/deletedref"
	"github.com/twcclan/goback/index/sql/ent/file"
	"github.com/twcclan/goback/index/sql/ent/pin"
	"github.com/twcclan/goback/index/sql/ent/predicate"
	"github.com/twcclan/goback/index/sql/ent/set"
	"github.com/twcclan/goback/index/sql/ent/setref"
	"github.com/twcclan/goback/index/sql/ent/tree"
	"github.com/twcclan/goback/proto"

	entsql "entgo.io/ent/dialect/sql"
)

// setConfig is what retention needs to know about a set.
type setConfig struct {
	name   string
	state  set.State
	paused bool
	policy retention.Policy
	hold   time.Duration
	trash  time.Duration
}

func (x *Index) loadSetConfig(ctx context.Context, c *ent.Client, setID int64) (*setConfig, error) {
	s, err := c.Set.Query().Where(set.ID(setID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, fmt.Errorf("%w: set %d", backup.ErrNotFound, setID)
	}

	if err != nil {
		return nil, err
	}

	defaults, err := loadSettings(ctx, c)
	if err != nil {
		return nil, err
	}

	cfg := &setConfig{
		name:   s.Name,
		state:  s.State,
		paused: s.RetentionPaused,
		policy: retention.Default,
		hold:   time.Duration(defaults.HoldDays) * 24 * time.Hour,
		trash:  time.Duration(defaults.TrashDays) * 24 * time.Hour,
	}

	if x.DefaultPolicy != nil {
		cfg.policy = *x.DefaultPolicy
	}

	for _, raw := range []*string{defaults.RetentionPolicy, s.RetentionPolicy} {
		if raw == nil {
			continue
		}

		p, err := retention.Parse([]byte(*raw))
		if err != nil {
			return nil, fmt.Errorf("set %d: %w", setID, err)
		}

		cfg.policy = p
	}

	return cfg, nil
}

// evaluateSet applies the set's policy to its un-tombstoned, un-deleted
// commits: kept ones record their reasons, the rest are retired into the
// hold window (none for a superseded checkpoint). A set whose retention
// is paused keeps everything until a policy is set.
func (x *Index) evaluateSet(ctx context.Context, tx *ent.Tx, setID int64, now time.Time) error {
	c := tx.Client()

	cfg, err := x.loadSetConfig(ctx, c, setID)
	if err != nil {
		return err
	}

	if cfg.state != set.StateActive || cfg.paused {
		return nil
	}

	rows, err := c.CommitRow.Query().Where(commitrow.SetID(setID), commitrow.TombstonedAtIsNil(), commitrow.DeletedAtIsNil()).All(ctx)
	if err != nil {
		return err
	}

	if len(rows) == 0 {
		return nil
	}

	refs := make([][]byte, len(rows))
	for i, row := range rows {
		refs[i] = row.Ref
	}

	pinned, err := c.Pin.Query().Where(pin.TargetIn(refs...), pin.DeletedAtIsNil()).Select(pin.FieldTarget).Strings(ctx)
	if err != nil {
		return err
	}

	held := make(map[string]bool, len(pinned))
	for _, target := range pinned {
		held[target] = true
	}

	commits := make([]retention.Commit, len(rows))
	for i, row := range rows {
		commits[i] = retention.Commit{ReceivedAt: row.ReceivedAt, Partial: row.Partial, Pinned: held[string(row.Ref)]}
	}

	for i, d := range retention.Evaluate(commits, cfg.policy, now) {
		switch {
		case d.Keep:
			err = c.CommitRow.Update().Where(commitrow.Ref(refs[i])).SetRetainedBy(d.RetainedBy()).ClearRetireAt().ClearExpiresAt().Exec(ctx)
		case rows[i].RetireAt != nil:
			continue
		default:
			expires := now.Add(cfg.hold)
			if commits[i].Partial {
				expires = now
			}

			err = c.CommitRow.Update().Where(commitrow.Ref(refs[i])).SetRetainedBy("").SetRetireAt(now).SetExpiresAt(expires).Exec(ctx)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// reevaluateSet re-applies a set's policy, as after a policy change.
func (x *Index) reevaluateSet(ctx context.Context, setID int64) error {
	return x.tx(ctx, func(tx *ent.Tx) error {
		if _, err := x.lockSet(ctx, tx, setID); err != nil {
			return err
		}

		return x.evaluateSet(ctx, tx, setID, x.now())
	})
}

// GetPolicy reports a set's retention.
func (x *Index) GetPolicy(ctx context.Context, name string) (index.SetRetention, error) {
	setID, err := findSet(ctx, x.client, name)
	if err != nil {
		return index.SetRetention{}, err
	}

	cfg, err := x.loadSetConfig(ctx, x.client, setID)
	if err != nil {
		return index.SetRetention{}, err
	}

	s, err := x.client.Set.Get(ctx, setID)
	if err != nil {
		return index.SetRetention{}, err
	}

	ret := index.SetRetention{Effective: cfg.policy, Paused: cfg.paused}
	if s.RetentionPolicy != nil {
		p, err := retention.Parse([]byte(*s.RetentionPolicy))
		if err != nil {
			return index.SetRetention{}, err
		}

		ret.Policy = &p
	}

	return ret, nil
}

// SetPolicy stores a set's policy, nil to inherit the store's default,
// clamped by the limits, and re-evaluates the set. Setting a policy also
// resumes retention paused by a rebuild.
func (x *Index) SetPolicy(ctx context.Context, name string, p *retention.Policy) error {
	setID, err := findSet(ctx, x.client, name)
	if err != nil {
		return err
	}

	raw, err := x.encodePolicy(p)
	if err != nil {
		return err
	}

	update := x.client.Set.UpdateOneID(setID).SetRetentionPaused(false)
	if raw == nil {
		update.ClearRetentionPolicy()
	} else {
		update.SetRetentionPolicy(*raw)
	}

	if err := update.Exec(ctx); err != nil {
		return err
	}

	return x.reevaluateSet(ctx, setID)
}

func (x *Index) encodePolicy(p *retention.Policy) (*string, error) {
	if p == nil {
		return nil, nil
	}

	policy := *p
	if policy.KeepLast < 1 {
		policy.KeepLast = 1
	}

	if err := policy.Validate(); err != nil {
		return nil, err
	}

	raw, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}

	return ptr(string(raw)), nil
}

// lockedCommit is a commit row under its lock, with the set it belongs to.
type lockedCommit struct {
	row        *ent.CommitRow
	tombstoned bool
	cfg        *setConfig
}

func (x *Index) lockOwnCommit(ctx context.Context, tx *ent.Tx, ref *proto.Ref) (*lockedCommit, error) {
	row, err := x.lockCommitRow(ctx, tx, ref.GetHash())
	if err != nil {
		return nil, err
	}

	cfg, err := x.loadSetConfig(ctx, tx.Client(), row.SetID)
	if err != nil {
		return nil, err
	}

	tombstoned := row.TombstonedAt != nil
	if !tombstoned {
		// a tombstone written but not yet durable already names the commit
		tombstoned, err = isDeleted(ctx, tx.Client(), ref.GetHash())
		if err != nil {
			return nil, err
		}
	}

	return &lockedCommit{row: row, tombstoned: tombstoned, cfg: cfg}, nil
}

// DeleteCommit implements backup.Retention.
func (x *Index) DeleteCommit(ctx context.Context, ref *proto.Ref) error {
	return x.tx(ctx, func(tx *ent.Tx) error {
		c, err := x.lockOwnCommit(ctx, tx, ref)
		if err != nil {
			return err
		}

		if c.tombstoned {
			return fmt.Errorf("%w: commit %x", backup.ErrTombstoned, ref.GetHash())
		}

		if c.cfg.state == set.StateActive {
			newest, err := tx.CommitRow.Query().Where(commitrow.SetID(c.row.SetID), liveCommit()).Order(ent.Desc(commitrow.FieldReceivedAt)).Select(commitrow.FieldRef).First(ctx)
			if err != nil && !ent.IsNotFound(err) {
				return err
			}

			if newest != nil && ref.Equal(&proto.Ref{Hash: newest.Ref}) {
				return fmt.Errorf("%w: %x", backup.ErrNewestCommit, ref.GetHash())
			}
		}

		pinned, err := tx.Pin.Query().Where(pin.Target(ref.GetHash()), pin.DeletedAtIsNil()).Exist(ctx)
		if err != nil {
			return err
		}

		if pinned {
			return fmt.Errorf("%w: %x", backup.ErrPinned, ref.GetHash())
		}

		now := x.now()
		expires := now.Add(c.cfg.trash)
		if c.row.ExpiresAt != nil && c.row.ExpiresAt.Before(expires) {
			expires = *c.row.ExpiresAt
		}

		return tx.CommitRow.Update().Where(commitrow.Ref(ref.GetHash())).SetDeletedAt(now).SetRetainedBy("").SetExpiresAt(expires).Exec(ctx)
	})
}

// UndeleteCommit implements backup.Retention.
func (x *Index) UndeleteCommit(ctx context.Context, ref *proto.Ref) error {
	return x.tx(ctx, func(tx *ent.Tx) error {
		c, err := x.lockOwnCommit(ctx, tx, ref)
		if err != nil {
			return err
		}

		if c.tombstoned {
			return fmt.Errorf("%w: commit %x", backup.ErrTombstoned, ref.GetHash())
		}

		if c.cfg.state != set.StateActive {
			return fmt.Errorf("%w: set %q", backup.ErrSetClosed, c.cfg.name)
		}

		err = tx.CommitRow.Update().Where(commitrow.Ref(ref.GetHash())).ClearDeletedAt().ClearRetireAt().ClearExpiresAt().Exec(ctx)
		if err != nil {
			return err
		}

		return x.evaluateSet(ctx, tx, c.row.SetID, x.now())
	})
}

func (x *Index) lockOwnSet(ctx context.Context, tx *ent.Tx, name string) (int64, *setConfig, error) {
	setID, err := findSet(ctx, tx.Client(), name)
	if err != nil {
		return 0, nil, err
	}

	if _, err := x.lockSet(ctx, tx, setID); err != nil {
		return 0, nil, err
	}

	cfg, err := x.loadSetConfig(ctx, tx.Client(), setID)

	return setID, cfg, err
}

// DeleteSet implements backup.Retention.
func (x *Index) DeleteSet(ctx context.Context, name string, erase bool) error {
	return x.tx(ctx, func(tx *ent.Tx) error {
		setID, cfg, err := x.lockOwnSet(ctx, tx, name)
		if err != nil {
			return err
		}

		if cfg.state == set.StateDeleted {
			return fmt.Errorf("%w: set %q", backup.ErrTombstoned, name)
		}

		now := x.now()
		expires := now.Add(cfg.trash)
		if erase {
			expires = now
		}

		update := tx.Set.UpdateOneID(setID).SetState(set.StateClosing)
		if erase {
			update.SetErase(true)
		}

		if err := update.Exec(ctx); err != nil {
			return err
		}

		live := []predicate.CommitRow{commitrow.SetID(setID), commitrow.TombstonedAtIsNil()}

		err = tx.CommitRow.Update().Where(append(live, commitrow.DeletedAtIsNil())...).SetDeletedAt(now).Exec(ctx)
		if err != nil {
			return err
		}

		err = tx.CommitRow.Update().Where(append(live, commitrow.Or(commitrow.ExpiresAtIsNil(), commitrow.ExpiresAtGT(expires)))...).SetExpiresAt(expires).Exec(ctx)
		if err != nil {
			return err
		}

		return tx.CommitRow.Update().Where(live...).SetRetainedBy("").Exec(ctx)
	})
}

// UndeleteSet implements backup.Retention.
func (x *Index) UndeleteSet(ctx context.Context, name string) error {
	return x.tx(ctx, func(tx *ent.Tx) error {
		setID, cfg, err := x.lockOwnSet(ctx, tx, name)
		if err != nil {
			return err
		}

		if cfg.state == set.StateDeleted {
			return fmt.Errorf("%w: set %q", backup.ErrTombstoned, name)
		}

		err = tx.Set.UpdateOneID(setID).SetState(set.StateActive).SetErase(false).Exec(ctx)
		if err != nil {
			return err
		}

		err = tx.CommitRow.Update().Where(commitrow.SetID(setID), commitrow.TombstonedAtIsNil()).ClearDeletedAt().ClearRetireAt().ClearExpiresAt().Exec(ctx)
		if err != nil {
			return err
		}

		return x.evaluateSet(ctx, tx, setID, x.now())
	})
}

// inVisibleSets keeps the pins whose commit belongs to a set the query
// can see.
func inVisibleSets() predicate.Pin {
	return func(s *entsql.Selector) {
		commits := entsql.Select(commitrow.FieldRef).From(entsql.Table(commitrow.Table))
		sets := entsql.Select(set.FieldID).From(entsql.Table(set.Table))
		commits.Where(entsql.In(commits.C(commitrow.FieldSetID), sets))

		s.Where(entsql.In(s.C(pin.FieldTarget), commits))
	}
}

// Unpin implements backup.Retention.
func (x *Index) Unpin(ctx context.Context, ref *proto.Ref) error {
	row, err := x.client.Pin.Query().Where(pin.Ref(ref.GetHash()), inVisibleSets()).Only(ctx)
	if ent.IsNotFound(err) {
		return fmt.Errorf("%w: pin %x", backup.ErrNotFound, ref.GetHash())
	}

	if err != nil {
		return err
	}

	if row.DeletedAt != nil {
		return fmt.Errorf("%w: pin %x", backup.ErrTombstoned, ref.GetHash())
	}

	// the tombstone is durable before the row says so; a second unpin in
	// the meantime writes a duplicate tombstone, which archives tolerate
	err = x.ObjectStore.Delete(ctx, ref)
	if err != nil {
		return err
	}

	if err := x.flush(); err != nil {
		return err
	}

	return x.tx(ctx, func(tx *ent.Tx) error {
		err := tx.Pin.Update().Where(pin.Ref(ref.GetHash()), pin.DeletedAtIsNil()).SetDeletedAt(x.now()).Exec(ctx)
		if err != nil {
			return err
		}

		setID, tombstoned, err := x.lockCommit(ctx, tx, row.Target)
		if err == nil && !tombstoned {
			err = x.evaluateSet(ctx, tx, setID, x.now())
		}

		if err != nil && !errors.Is(err, backup.ErrNotFound) {
			return err
		}

		return nil
	})
}

// Pins implements backup.Retention.
func (x *Index) Pins(ctx context.Context) ([]*proto.PinInfo, error) {
	rows, err := x.client.Pin.Query().Where(pin.DeletedAtIsNil(), inVisibleSets()).Order(ent.Asc(pin.FieldReceivedAt)).All(ctx)
	if err != nil {
		return nil, err
	}

	return mapAll(rows, m.Pin), nil
}

// Retire implements backup.Retirer: every retired commit past its window
// gets a tombstone, its set loses the rows no other commit holds, and a
// closing set with nothing left becomes deleted. An active set whose
// retention is paused is skipped; a deleted set proceeds regardless.
func (x *Index) Retire(ctx context.Context, now time.Time) (int, error) {
	now = now.UTC()

	held, err := x.restoreLeases(ctx)
	if err != nil {
		return 0, err
	}

	due, err := x.client.CommitRow.Query().
		Where(commitrow.TombstonedAtIsNil(), commitrow.ExpiresAtNotNil(), commitrow.ExpiresAtLTE(now),
			commitrow.HasSetWith(set.Or(set.RetentionPaused(false), set.StateNEQ(set.StateActive)))).
		WithSet().All(ctx)
	if err != nil {
		return 0, err
	}

	pinned, err := x.storedPins(ctx, due)
	if err != nil {
		return 0, err
	}

	var written []*ent.CommitRow

	for _, c := range due {
		if held[string(c.Ref)] || pinned[string(c.Ref)] {
			continue
		}

		done, err := x.writeTombstone(ctx, c.Ref, now, c.Edges.Set.Erase)
		if err != nil {
			return 0, err
		}

		if done {
			written = append(written, c)
		}
	}

	// rows say tombstoned only once the tombstones are durable, so a crash
	// before this point leaves commits that the next run tombstones again
	if len(written) > 0 {
		if err := x.flush(); err != nil {
			return 0, err
		}
	}

	count := 0
	sets := make(map[int64][][]byte)

	for _, c := range written {
		if err := x.markTombstoned(ctx, c.Ref, now); err != nil {
			return count, err
		}

		count++
		sets[c.SetID] = append(sets[c.SetID], c.Ref)
	}

	for setID, refs := range sets {
		if err := x.pruneRefs(ctx, setID, refs); err != nil {
			return count, err
		}

		if err := x.dropDeadRows(ctx, setID); err != nil {
			return count, err
		}
	}

	return count, x.closeEmptySets(ctx)
}

// storedPins indexes every pin in the store that holds a commit of an
// active set due for its tombstone, and returns those commits. The index
// may have missed a pin, restored from a copy older than it, and what the
// store holds is what counts, unpins included.
func (x *Index) storedPins(ctx context.Context, due []*ent.CommitRow) (map[string]bool, error) {
	targets := make(map[string]bool, len(due))
	for _, c := range due {
		if c.Edges.Set.State == set.StateActive {
			targets[string(c.Ref)] = true
		}
	}

	if len(targets) == 0 {
		return nil, nil
	}

	unpinned := map[string]bool{}

	if hw, ok := storeAs[backup.HeaderWalker](x.ObjectStore); ok {
		err := hw.WalkHeaders(ctx, proto.ObjectType_TOMBSTONE, func(hdr *proto.ObjectHeader) error {
			unpinned[string(hdr.GetTombstoneFor().GetHash())] = true
			return nil
		})
		if err != nil && !errors.Is(err, backup.ErrNotImplemented) {
			return nil, err
		}
	}

	pinned := map[string]bool{}

	err := x.ObjectStore.Walk(ctx, true, proto.ObjectType_PIN, func(obj *proto.Object) error {
		target := obj.GetPin().GetTarget().GetHash()
		ref := obj.Ref()

		if !targets[string(target)] || unpinned[string(ref.Hash)] {
			return nil
		}

		gone, err := isDeleted(ctx, x.client, ref.Hash)
		if err != nil || gone {
			return err
		}

		gone, err = x.client.Pin.Query().Where(pin.Ref(ref.Hash), pin.DeletedAtNotNil()).Exist(ctx)
		if err != nil || gone {
			return err
		}

		pinned[string(target)] = true

		return x.indexPin(ctx, obj.GetPin(), ref, false)
	})
	if errors.Is(err, backup.ErrNotImplemented) {
		return pinned, nil
	}

	return pinned, err
}

// restoreLeases returns the refs live restore sessions hold.
func (x *Index) restoreLeases(ctx context.Context) (map[string]bool, error) {
	leaser, ok := storeAs[backup.RestoreLeaser](x.ObjectStore)
	if !ok {
		return nil, nil
	}

	refs, err := leaser.RestoreLeases(ctx)
	if err != nil {
		return nil, err
	}

	held := make(map[string]bool, len(refs))
	for _, ref := range refs {
		held[string(ref.GetHash())] = true
	}

	return held, nil
}

// writeTombstone writes the tombstone of one retired commit under its row
// lock, unless a pin, an undelete or another run got there first.
func (x *Index) writeTombstone(ctx context.Context, ref []byte, now time.Time, erase bool) (bool, error) {
	var written bool

	err := x.tx(ctx, func(tx *ent.Tx) error {
		row, err := forUpdate(x, tx.CommitRow.Query().Where(commitrow.Ref(ref))).Only(ctx)
		if ent.IsNotFound(err) || (err == nil && row.TombstonedAt != nil) {
			return nil
		}

		if err != nil {
			return err
		}

		pinned, err := tx.Pin.Query().Where(pin.Target(ref), pin.DeletedAtIsNil()).Exist(ctx)
		if err != nil {
			return err
		}

		if pinned || row.ExpiresAt == nil || row.ExpiresAt.After(now) {
			return nil
		}

		if erase {
			eraser, ok := storeAs[backup.Eraser](x.ObjectStore)
			if !ok {
				return fmt.Errorf("store %T cannot erase commit %x", x.ObjectStore, ref)
			}

			err = eraser.Erase(ctx, &proto.Ref{Hash: ref})
		} else {
			err = x.ObjectStore.Delete(ctx, &proto.Ref{Hash: ref})
		}

		if err != nil {
			return err
		}

		// deleted_refs names the commit from here on, so no undelete or pin
		// slips in while the tombstone is on its way to the archives
		err = recordDeleted(ctx, tx.Client(), ref, now)
		if err != nil {
			return err
		}

		written = true

		return nil
	})

	return written, err
}

// markTombstoned records a durable tombstone on the commit's row and drops
// the commit ref from what the store's sets reach.
func (x *Index) markTombstoned(ctx context.Context, ref []byte, now time.Time) error {
	return x.tx(ctx, func(tx *ent.Tx) error {
		_, tombstoned, err := x.lockCommit(ctx, tx, ref)
		if err != nil || tombstoned {
			return err
		}

		err = tx.CommitRow.Update().Where(commitrow.Ref(ref)).SetTombstonedAt(now).ClearPresence().Exec(ctx)
		if err != nil {
			return err
		}

		err = recordDeleted(ctx, tx.Client(), ref, now)
		if err != nil {
			return err
		}

		_, err = tx.SetRef.Delete().Where(setref.Ref(ref)).Exec(ctx)

		return err
	})
}

// recordDeleted adds a ref to deleted_refs, the durable set of refs a
// tombstone names.
func recordDeleted(ctx context.Context, c *ent.Client, ref []byte, at time.Time) error {
	return ignoreNoRows(c.DeletedRef.Create().SetRef(ref).SetTombstonedAt(at).OnConflict().DoNothing().Exec(ctx))
}

// isDeleted reports whether a tombstone names the ref.
func isDeleted(ctx context.Context, c *ent.Client, ref []byte) (bool, error) {
	return c.DeletedRef.Query().Where(deletedref.Ref(ref)).Exist(ctx)
}

// dropDeadRows deletes the set's files and trees rows whose validity range
// holds no commit without a tombstone.
func (x *Index) dropDeadRows(ctx context.Context, setID int64) error {
	untombstoned := func(c *entsql.SelectTable) *entsql.Predicate {
		return entsql.IsNull(c.C(commitrow.FieldTombstonedAt))
	}

	_, err := x.client.File.Delete().Where(file.SetID(setID), predicate.File(entsql.NotPredicates(heldByCommit(untombstoned)))).Exec(ctx)
	if err != nil {
		return err
	}

	_, err = x.client.Tree.Delete().Where(tree.SetID(setID), predicate.Tree(entsql.NotPredicates(heldByCommit(untombstoned)))).Exec(ctx)

	return err
}

func (x *Index) closeEmptySets(ctx context.Context) error {
	noLiveCommit := func(s *entsql.Selector) {
		c := entsql.Table(commitrow.Table)
		s.Where(entsql.Not(entsql.Exists(entsql.Select().From(c).Where(entsql.And(
			entsql.ColumnsEQ(c.C(commitrow.FieldSetID), s.C(set.FieldID)),
			entsql.IsNull(c.C(commitrow.FieldTombstonedAt)),
		)))))
	}

	ids, err := x.client.Set.Query().Where(set.StateEQ(set.StateClosing), predicate.Set(noLiveCommit)).IDs(ctx)
	if err != nil {
		return err
	}

	for _, id := range ids {
		err := x.tx(ctx, func(tx *ent.Tx) error {
			s, err := x.lockSet(ctx, tx, id)
			if err != nil {
				return err
			}

			live, err := tx.CommitRow.Query().Where(commitrow.SetID(id), commitrow.TombstonedAtIsNil()).Exist(ctx)
			if err != nil || live || s.State != set.StateClosing {
				return err
			}

			if _, err := tx.File.Delete().Where(file.SetID(id)).Exec(ctx); err != nil {
				return err
			}

			if _, err := tx.Tree.Delete().Where(tree.SetID(id)).Exec(ctx); err != nil {
				return err
			}

			if _, err := tx.SetRef.Delete().Where(setref.SetID(id)).Exec(ctx); err != nil {
				return err
			}

			return tx.Set.UpdateOneID(id).SetState(set.StateDeleted).Exec(ctx)
		})
		if err != nil {
			return err
		}

		x.logger().Info("deleted empty set", "set", id)
	}

	return nil
}
