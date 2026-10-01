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
	"go.opentelemetry.io/otel/attribute"

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

	ctx, span := tracer.Start(ctx, "Index.Retire")
	defer span.End()

	phases := newPhases()
	tombstoned := 0

	defer func() {
		if tombstoned > 0 {
			phases.log(x.logger(), "retired commits", "commits", tombstoned)
		}
	}()

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

	phases.done("due")

	pinned, err := x.storedPins(ctx, due)
	if err != nil {
		return 0, err
	}

	phases.done("pins")

	var eligible []*ent.CommitRow

	for _, c := range due {
		if !held[string(c.Ref)] && !pinned[string(c.Ref)] {
			eligible = append(eligible, c)
		}
	}

	written, err := x.writeTombstones(ctx, eligible, now)
	if err != nil {
		return 0, err
	}

	tombstoned = len(written)
	phases.done("tombstones")
	span.SetAttributes(attribute.Int("due", len(due)), attribute.Int("tombstoned", len(written)))

	// rows say tombstoned only once the tombstones are durable, so a crash
	// before this point leaves commits that the next run tombstones again
	if len(written) > 0 {
		if err := x.flush(); err != nil {
			return 0, err
		}
	}

	phases.done("flush")

	count := 0
	sets := make(map[int64][][]byte)

	for _, c := range written {
		sets[c.SetID] = append(sets[c.SetID], c.Ref)
	}

	for setID, refs := range sets {
		for start := 0; start < len(refs); start += retireBatch {
			chunk := refs[start:min(start+retireBatch, len(refs))]

			if err := x.markTombstoned(ctx, setID, chunk, now); err != nil {
				return count, err
			}

			count += len(chunk)
		}
	}

	phases.done("mark")

	for setID, refs := range sets {
		if err := x.pruneRefs(ctx, setID, refs); err != nil {
			return count, err
		}

		if err := x.dropDeadRows(ctx, setID); err != nil {
			return count, err
		}
	}

	phases.done("prune")

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

// retireBatch is how many commits one retiring transaction takes.
const retireBatch = 500

// writeTombstones writes the tombstones of retired commits under their row
// locks, passing over those a pin, an undelete or another run got to
// first, and returns the ones it wrote.
func (x *Index) writeTombstones(ctx context.Context, due []*ent.CommitRow, now time.Time) ([]*ent.CommitRow, error) {
	var written []*ent.CommitRow

	for start := 0; start < len(due); start += retireBatch {
		chunk := due[start:min(start+retireBatch, len(due))]

		refs := make([][]byte, len(chunk))
		for i, c := range chunk {
			refs[i] = c.Ref
		}

		var done []*ent.CommitRow

		err := x.tx(ctx, func(tx *ent.Tx) error {
			done = nil

			// row order, so that two runs lock in the same order
			rows, err := forUpdate(x, tx.CommitRow.Query().Where(commitrow.RefIn(refs...)).Order(ent.Asc(commitrow.FieldID))).All(ctx)
			if err != nil {
				return err
			}

			current := make(map[string]*ent.CommitRow, len(rows))
			for _, row := range rows {
				current[string(row.Ref)] = row
			}

			pins, err := tx.Pin.Query().Where(pin.TargetIn(refs...), pin.DeletedAtIsNil()).Select(pin.FieldTarget).All(ctx)
			if err != nil {
				return err
			}

			pinned := make(map[string]bool, len(pins))
			for _, p := range pins {
				pinned[string(p.Target)] = true
			}

			var deleted [][]byte

			for _, c := range chunk {
				row := current[string(c.Ref)]
				if row == nil || row.TombstonedAt != nil || pinned[string(c.Ref)] || row.ExpiresAt == nil || row.ExpiresAt.After(now) {
					continue
				}

				if err := x.tombstone(ctx, c.Ref, c.Edges.Set.Erase); err != nil {
					return err
				}

				deleted = append(deleted, c.Ref)
				done = append(done, c)
			}

			// deleted_refs names the commits from here on, so no undelete or
			// pin slips in while the tombstones are on their way to the
			// archives
			return recordDeleted(ctx, tx.Client(), deleted, now)
		})
		if err != nil {
			return nil, err
		}

		written = append(written, done...)
	}

	return written, nil
}

// tombstone writes the commit's tombstone, erasing it when its set says
// so.
func (x *Index) tombstone(ctx context.Context, ref []byte, erase bool) error {
	if !erase {
		return x.ObjectStore.Delete(ctx, &proto.Ref{Hash: ref})
	}

	eraser, ok := storeAs[backup.Eraser](x.ObjectStore)
	if !ok {
		return fmt.Errorf("store %T cannot erase commit %x", x.ObjectStore, ref)
	}

	return eraser.Erase(ctx, &proto.Ref{Hash: ref})
}

// markTombstoned records durable tombstones on the rows of the set's
// commits and drops the commit refs from what the store's sets reach.
func (x *Index) markTombstoned(ctx context.Context, setID int64, refs [][]byte, now time.Time) error {
	return x.tx(ctx, func(tx *ent.Tx) error {
		if _, err := x.lockSet(ctx, tx, setID); err != nil {
			return err
		}

		rows, err := forUpdate(x, tx.CommitRow.Query().
			Where(commitrow.RefIn(refs...), commitrow.SetID(setID), commitrow.TombstonedAtIsNil()).
			Order(ent.Asc(commitrow.FieldID))).All(ctx)
		if err != nil {
			return err
		}

		if len(rows) == 0 {
			return nil
		}

		marked := make([][]byte, len(rows))
		for i, row := range rows {
			marked[i] = row.Ref
		}

		err = tx.CommitRow.Update().Where(commitrow.RefIn(marked...)).SetTombstonedAt(now).ClearPresence().Exec(ctx)
		if err != nil {
			return err
		}

		if err := recordDeleted(ctx, tx.Client(), marked, now); err != nil {
			return err
		}

		_, err = tx.SetRef.Delete().Where(setref.RefIn(marked...)).Exec(ctx)

		return err
	})
}

// recordDeleted adds refs to deleted_refs, the durable set of refs a
// tombstone names.
func recordDeleted(ctx context.Context, c *ent.Client, refs [][]byte, at time.Time) error {
	if len(refs) == 0 {
		return nil
	}

	rows := make([]*ent.DeletedRefCreate, len(refs))
	for i, ref := range refs {
		rows[i] = c.DeletedRef.Create().SetRef(ref).SetTombstonedAt(at)
	}

	return ignoreNoRows(c.DeletedRef.CreateBulk(rows...).OnConflict().DoNothing().Exec(ctx))
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
