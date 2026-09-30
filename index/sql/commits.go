package sql

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/index/sql/ent/commitrow"
	"github.com/twcclan/goback/index/sql/ent/file"
	"github.com/twcclan/goback/index/sql/ent/pin"
	"github.com/twcclan/goback/index/sql/ent/predicate"
	"github.com/twcclan/goback/index/sql/ent/set"
	"github.com/twcclan/goback/index/sql/ent/setref"
	"github.com/twcclan/goback/index/sql/ent/tree"
	"github.com/twcclan/goback/index/sql/mapping"
	"github.com/twcclan/goback/index/sql/mapping/gen"
	"github.com/twcclan/goback/proto"

	entsql "entgo.io/ent/dialect/sql"
	"golang.org/x/sync/errgroup"
)

// m maps index rows to what the callers speak; one value for the package.
var m = gen.MapperImpl{}

// Stamp assigns the receipt time to a commit or pin and the set id to a
// commit, replacing whatever they carried.
func (x *Index) Stamp(ctx context.Context, object *proto.Object) error {
	commit := object.GetCommit()
	if commit == nil && object.GetPin() == nil {
		return nil
	}

	var setID int64
	if commit != nil {
		var err error
		setID, err = x.ensureSet(ctx, x.client, commit, nil, true)
		if err != nil {
			return err
		}
	}

	object.Stamp(uint64(setID), x.stamp())

	return nil
}

// seedStamp continues the receipt clock from the newest commit, so stamps
// stay strictly increasing across restarts and clock steps.
func (x *Index) seedStamp(ctx context.Context) error {
	newest, err := x.client.CommitRow.Query().Order(ent.Desc(commitrow.FieldReceivedAt)).First(ctx)
	if ent.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}

	x.stampMu.Lock()
	x.lastStamp = newest.ReceivedAt.UTC()
	x.stampMu.Unlock()

	return nil
}

// Put stores an object and indexes a commit or pin.
func (x *Index) Put(ctx context.Context, object *proto.Object) error {
	err := backup.CheckReferences(ctx, x.ObjectStore, object)
	if err != nil {
		return err
	}

	if p := object.GetPin(); p != nil {
		err = x.checkPinTarget(ctx, x.client, p.GetTarget())
		if err != nil {
			return err
		}
	}

	err = x.Stamp(ctx, object)
	if err != nil {
		return err
	}

	err = x.ObjectStore.Put(ctx, object)
	if err != nil {
		return err
	}

	switch object.Type() {
	case proto.ObjectType_COMMIT:
		return x.indexCommit(ctx, object.GetCommit(), object.Ref(), true, true)
	case proto.ObjectType_PIN:
		return x.indexPin(ctx, object.GetPin(), object.Ref(), true)
	}

	return nil
}

func (x *Index) checkPinTarget(ctx context.Context, c *ent.Client, target *proto.Ref) error {
	deleted, err := isDeleted(ctx, c, target.GetHash())
	if err != nil {
		return err
	}

	if !deleted {
		row, err := c.CommitRow.Query().Where(commitrow.Ref(target.GetHash())).Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}

		deleted = err == nil && row.TombstonedAt != nil
	}

	if deleted {
		return fmt.Errorf("%w: commit %x", backup.ErrTombstoned, target.GetHash())
	}

	return nil
}

// ensureSet resolves the set a commit belongs to from the name in its
// body. A commit without a set name is rejected in strict mode and
// skipped (returning set id 0) otherwise; strict mode also refuses a set
// that is not active.
func (x *Index) ensureSet(ctx context.Context, c *ent.Client, commit *proto.Commit, ref *proto.Ref, strict bool) (int64, error) {
	if commit.GetBackupSet() == "" {
		if strict {
			return 0, fmt.Errorf("%w: commit names no set", backup.ErrDanglingRef)
		}

		x.logger().Warn("ignoring commit that names no set", "ref", fmt.Sprintf("%x", ref.GetHash()))
		return 0, nil
	}

	var wantID int64
	if !strict {
		wantID = int64(commit.GetSetId())
	}

	setID, err := ensureSet(ctx, c, commit.GetBackupSet(), commit.GetAgentId(), wantID, strict)
	if err != nil || !strict {
		return setID, err
	}

	s, err := c.Set.Get(ctx, setID)
	if err != nil {
		return 0, err
	}

	if s.State != set.StateActive {
		return 0, fmt.Errorf("%w: set %q", backup.ErrSetClosed, commit.GetBackupSet())
	}

	return setID, nil
}

// indexCommit records a commit and the versions its tree introduces. In
// strict mode a commit whose tree cannot be traversed is rejected with
// backup.ErrDanglingRef. Commits of a set are indexed one at a time, in
// receipt order.
func (x *Index) indexCommit(ctx context.Context, commit *proto.Commit, ref *proto.Ref, strict, evaluate bool) error {
	treeObj, err := x.ObjectStore.Get(ctx, commit.Tree)
	if errors.Is(err, backup.ErrNotFound) {
		if strict {
			return fmt.Errorf("%w: root tree %x", backup.ErrDanglingRef, commit.Tree.GetHash())
		}

		x.logger().Warn("root tree could not be retrieved", "tree", fmt.Sprintf("%x", commit.Tree.GetHash()))
		return nil
	}

	if err != nil {
		return err
	}

	// the set is created outside the transaction: a unique violation
	// would abort a Postgres transaction, and a set without commits is
	// harmless
	setID, err := x.ensureSet(ctx, x.client, commit, ref, strict)
	if err != nil || setID == 0 {
		return err
	}

	var (
		start   = time.Now()
		indexed int64
	)

	err = x.tx(ctx, func(tx *ent.Tx) error {
		c := tx.Client()

		exists, err := c.CommitRow.Query().Where(commitrow.Ref(ref.Hash)).Exist(ctx)
		if err != nil || exists {
			return err
		}

		deleted, err := isDeleted(ctx, c, ref.Hash)
		if err != nil {
			return err
		}

		if deleted {
			if strict {
				return fmt.Errorf("%w: commit %x", backup.ErrTombstoned, ref.Hash)
			}

			return nil
		}

		s, err := x.lockSet(ctx, tx, setID)
		if err != nil {
			return err
		}

		if strict {
			if s.State != set.StateActive {
				return fmt.Errorf("%w: set %q", backup.ErrSetClosed, commit.GetBackupSet())
			}

			if owner := deref(s.AgentID); owner != "" && owner != commit.GetAgentId() {
				return fmt.Errorf("%w: set %q belongs to agent %q, commit is by %q", backup.ErrSetOwned, commit.GetBackupSet(), owner, commit.GetAgentId())
			}
		}

		at := time.Unix(0, commit.ReceivedAtNs).UTC()

		newest, err := c.CommitRow.Query().Where(commitrow.SetID(setID)).Order(ent.Desc(commitrow.FieldReceivedAt)).First(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}

		if err == nil && !at.After(newest.ReceivedAt) {
			x.logger().Warn("ignoring commit received before the set's newest", "ref", fmt.Sprintf("%x", ref.Hash), "received", at, "newest", newest.ReceivedAt)
			return nil
		}

		diff := &treeDiff{c: c, store: x.ObjectStore, setID: setID, at: at}
		root, err := diff.flatten(ctx, commit.Tree, treeObj.GetTree())
		if err == nil {
			err = diff.dir(ctx, "", root)
		}
		if err != nil {
			if strict {
				return fmt.Errorf("%w: %v", backup.ErrDanglingRef, err)
			}

			x.logger().Warn("ignoring commit, traversing its tree failed", "ref", fmt.Sprintf("%x", ref.Hash), "err", err)
			return nil
		}

		size, err := logicalSize(ctx, c, setID, at)
		if err != nil {
			return err
		}

		err = c.CommitRow.Create().SetRef(ref.Hash).SetTimestamp(time.Unix(commit.Timestamp, 0).UTC()).SetReceivedAt(at).
			SetTree(commit.Tree.Hash).SetParent(commit.GetParent().GetHash()).SetAgentID(commit.GetAgentId()).
			SetScanStartNs(commit.GetScanStartNs()).SetPolicyVersion(commit.GetPolicyVersion()).SetConsistent(commit.GetConsistent()).
			SetSetID(setID).SetPartial(commit.Partial).SetMetadata(commit.GetMetadata()).SetLogicalSize(size).Exec(ctx)
		if err != nil {
			return err
		}

		err = diff.ref(ctx, ref.Hash)
		if err != nil {
			return err
		}

		err = x.clearDamage(ctx, tx, setID, commit.Partial, commit.GetScanStartNs())
		if err != nil {
			return err
		}

		if evaluate {
			err = x.evaluateSet(ctx, tx, setID, x.now())
			if err != nil {
				return err
			}
		}

		indexed = setID

		return nil
	})
	if err != nil || indexed == 0 {
		return err
	}

	x.logger().Info("indexed commit", "set", commit.GetBackupSet(), "took", time.Since(start))

	return nil
}

// treeDiff walks the changed directories of a commit against the open
// rows of its set, closing the rows of versions that changed or vanished
// at the commit's receipt time and opening rows for new versions.
type treeDiff struct {
	c     *ent.Client
	store backup.ObjectStore
	setID int64
	at    time.Time
}

// ref records that the set references an object, which makes it readable.
func (d *treeDiff) ref(ctx context.Context, hash []byte) error {
	return ignoreNoRows(d.c.SetRef.Create().SetSetID(d.setID).SetRef(hash).
		OnConflict().DoNothing().Exec(ctx))
}

// flatten records a tree and its split trees and returns the flat node
// list, like backup.LoadTree.
func (d *treeDiff) flatten(ctx context.Context, ref *proto.Ref, t *proto.Tree) (*proto.Tree, error) {
	err := d.ref(ctx, ref.GetHash())
	if err != nil {
		return nil, err
	}

	if len(t.Splits) == 0 {
		return t, nil
	}

	flat := &proto.Tree{}
	for _, split := range t.Splits {
		obj, err := d.store.Get(ctx, split)
		if err != nil {
			return nil, err
		}

		if obj.GetTree() == nil {
			return nil, fmt.Errorf("split %x is not a tree", split.GetHash())
		}

		sub, err := d.flatten(ctx, split, obj.GetTree())
		if err != nil {
			return nil, err
		}

		flat.Nodes = append(flat.Nodes, sub.Nodes...)
	}

	return flat, nil
}

// refFile records a file object and, for one large enough to be split,
// the sub-file objects it names.
func (d *treeDiff) refFile(ctx context.Context, node *proto.TreeNode) error {
	err := d.ref(ctx, node.GetRef().GetHash())
	if err != nil || node.GetStat().GetSize() < backup.SplitFileSize {
		return err
	}

	obj, err := d.store.Get(ctx, node.GetRef())
	if err != nil {
		return err
	}

	for _, split := range obj.GetFile().GetSplits() {
		err = d.ref(ctx, split.GetHash())
		if err != nil {
			return err
		}
	}

	return nil
}

// sameFile reports whether an open row still describes the node, so its
// version can stay open. A symlink has no ref, so its target decides.
func sameFile(row *ent.File, node *proto.TreeNode) bool {
	info := node.GetStat()

	return bytes.Equal(row.Ref, node.GetRef().GetHash()) &&
		row.MtimeNs == info.GetMtimeNs() &&
		row.Type == uint32(info.GetType()) &&
		bytes.Equal(row.LinkTarget, info.GetLinkTarget())
}

func (d *treeDiff) openTrees(ctx context.Context, dir string) (map[string][]byte, error) {
	rows, err := d.c.Tree.Query().Where(tree.SetID(d.setID), tree.Dir(dir), tree.ValidUntilIsNil()).All(ctx)
	if err != nil {
		return nil, err
	}

	open := make(map[string][]byte, len(rows))
	for _, row := range rows {
		open[row.Path] = row.Ref
	}

	return open, nil
}

func (d *treeDiff) openFiles(ctx context.Context, dir string) (map[string]*ent.File, error) {
	rows, err := d.c.File.Query().Where(file.SetID(d.setID), file.Dir(dir), file.ValidUntilIsNil()).All(ctx)
	if err != nil {
		return nil, err
	}

	open := make(map[string]*ent.File, len(rows))
	for _, row := range rows {
		open[row.Path] = row
	}

	return open, nil
}

func (d *treeDiff) closeTree(ctx context.Context, p string) error {
	return d.c.Tree.Update().Where(tree.SetID(d.setID), tree.Path(p), tree.ValidUntilIsNil()).SetValidUntil(d.at).Exec(ctx)
}

func (d *treeDiff) closeFile(ctx context.Context, p string) error {
	return d.c.File.Update().Where(file.SetID(d.setID), file.Path(p), file.ValidUntilIsNil()).SetValidUntil(d.at).Exec(ctx)
}

// closeSubtree closes every open row below a directory that vanished.
func (d *treeDiff) closeSubtree(ctx context.Context, dir string) error {
	err := d.c.File.Update().Where(file.SetID(d.setID), file.Dir(dir), file.ValidUntilIsNil()).SetValidUntil(d.at).Exec(ctx)
	if err != nil {
		return err
	}

	children, err := d.openTrees(ctx, dir)
	if err != nil {
		return err
	}

	err = d.c.Tree.Update().Where(tree.SetID(d.setID), tree.Dir(dir), tree.ValidUntilIsNil()).SetValidUntil(d.at).Exec(ctx)
	if err != nil {
		return err
	}

	for child := range children {
		if err := d.closeSubtree(ctx, child); err != nil {
			return err
		}
	}

	return nil
}

func (d *treeDiff) dir(ctx context.Context, dir string, t *proto.Tree) error {
	trees, err := d.openTrees(ctx, dir)
	if err != nil {
		return err
	}

	files, err := d.openFiles(ctx, dir)
	if err != nil {
		return err
	}

	seen := make(map[string]bool, len(t.Nodes))
	var descend []*proto.TreeNode

	for _, node := range t.GetNodes() {
		info := node.GetStat()

		child := proto.JoinPath(dir, info.GetName())
		seen[child] = true

		if info.IsDir() {
			cur, open := trees[child]
			if open && bytes.Equal(cur, node.GetRef().GetHash()) {
				continue
			}

			if open {
				if err := d.closeTree(ctx, child); err != nil {
					return err
				}
			}

			err = d.c.Tree.Create().SetSetID(d.setID).SetPath(child).SetDir(dir).SetValidFrom(d.at).SetRef(node.GetRef().GetHash()).Exec(ctx)
			if err != nil {
				return err
			}

			descend = append(descend, node)
			continue
		}

		cur, open := files[child]
		if open && sameFile(cur, node) {
			continue
		}

		if open {
			if err := d.closeFile(ctx, child); err != nil {
				return err
			}
		}

		err = d.c.File.Create().SetSetID(d.setID).SetPath(child).SetDir(dir).SetValidFrom(d.at).SetRef(node.GetRef().GetHash()).
			SetMtimeNs(info.GetMtimeNs()).SetMode(info.GetMode()).SetUser(string(info.GetUser())).SetGroup(string(info.GetGroup())).SetSize(info.GetSize()).
			SetType(uint32(info.GetType())).SetLinkTarget(info.GetLinkTarget()).Exec(ctx)
		if err != nil {
			return err
		}

		if info.GetType() == proto.NodeType_NODE_SYMLINK {
			continue
		}

		err = d.refFile(ctx, node)
		if err != nil {
			return err
		}
	}

	for p := range trees {
		if seen[p] {
			continue
		}

		if err := d.closeTree(ctx, p); err != nil {
			return err
		}

		if err := d.closeSubtree(ctx, p); err != nil {
			return err
		}
	}

	for p := range files {
		if !seen[p] {
			if err := d.closeFile(ctx, p); err != nil {
				return err
			}
		}
	}

	subtrees := make([]*proto.Object, len(descend))
	grp, gctx := errgroup.WithContext(ctx)
	grp.SetLimit(16)

	for i, node := range descend {
		i, node := i, node
		grp.Go(func() error {
			var err error
			subtrees[i], err = d.store.Get(gctx, node.GetRef())
			if err == nil && subtrees[i].GetTree() == nil {
				err = fmt.Errorf("%x is not a tree", node.GetRef().GetHash())
			}
			return err
		})
	}

	if err := grp.Wait(); err != nil {
		return err
	}

	for i, node := range descend {
		flat, err := d.flatten(ctx, node.GetRef(), subtrees[i].GetTree())
		if err != nil {
			return err
		}

		err = d.dir(ctx, proto.JoinPath(dir, node.GetStat().GetName()), flat)
		if err != nil {
			return err
		}
	}

	return nil
}

// indexPin caches a pin and revives its target commit. A pin of a
// tombstoned commit is refused in strict mode.
func (x *Index) indexPin(ctx context.Context, p *proto.Pin, ref *proto.Ref, strict bool) error {
	return x.tx(ctx, func(tx *ent.Tx) error {
		c := tx.Client()
		target := p.GetTarget().GetHash()

		setID, tombstoned, err := x.lockCommit(ctx, tx, target)
		if err != nil && !errors.Is(err, backup.ErrNotFound) {
			return err
		}

		if !tombstoned {
			tombstoned, err = isDeleted(ctx, c, target)
			if err != nil {
				return err
			}
		}

		if tombstoned {
			if strict {
				return fmt.Errorf("%w: commit %x", backup.ErrTombstoned, target)
			}

			x.logger().Warn("pin targets a tombstoned commit", "pin", fmt.Sprintf("%x", ref.GetHash()), "commit", fmt.Sprintf("%x", target))
		}

		if setID != 0 && strict {
			err = x.checkPinScope(ctx, c, setID)
			if err != nil {
				return err
			}
		}

		err = ignoreNoRows(c.Pin.Create().SetRef(ref.Hash).SetTarget(target).SetReceivedAt(time.Unix(0, p.GetReceivedAtNs()).UTC()).
			SetMetadata(p.GetMetadata()).OnConflict().DoNothing().Exec(ctx))
		if err != nil {
			return err
		}

		if setID != 0 && !tombstoned {
			err = c.CommitRow.Update().Where(commitrow.Ref(target)).ClearRetireAt().ClearDeletedAt().ClearExpiresAt().Exec(ctx)
			if err != nil {
				return err
			}

			if strict {
				err = x.evaluateSet(ctx, tx, setID, x.now())
				if err != nil {
					return err
				}
			}
		}

		return nil
	})
}

// checkPinScope verifies that a pin's target is in a set that still
// accepts changes.
func (x *Index) checkPinScope(ctx context.Context, c *ent.Client, setID int64) error {
	cfg, err := x.loadSetConfig(ctx, c, setID)
	if err != nil {
		return err
	}

	if cfg.state != set.StateActive {
		return fmt.Errorf("%w: set %q", backup.ErrSetClosed, cfg.name)
	}

	return nil
}

// lockCommit takes the commit's row lock and reports its set and whether
// it is tombstoned, or backup.ErrNotFound.
func (x *Index) lockCommit(ctx context.Context, tx *ent.Tx, ref []byte) (int64, bool, error) {
	row, err := x.lockCommitRow(ctx, tx, ref)
	if err != nil {
		return 0, false, err
	}

	return row.SetID, row.TombstonedAt != nil, nil
}

// lockCommitRow takes the commit's set lock and then its row lock, the
// order every path that goes on to evaluate the set must follow; the
// commit's absence is backup.ErrNotFound.
func (x *Index) lockCommitRow(ctx context.Context, tx *ent.Tx, ref []byte) (*ent.CommitRow, error) {
	unlocked, err := tx.CommitRow.Query().Where(commitrow.Ref(ref)).Select(commitrow.FieldSetID).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, backup.ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	if _, err := x.lockSet(ctx, tx, unlocked.SetID); err != nil {
		return nil, err
	}

	row, err := forUpdate(x, tx.CommitRow.Query().Where(commitrow.Ref(ref))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, backup.ErrNotFound
	}

	return row, err
}

// ReIndex rebuilds the caches from the archives in three passes:
// tombstones, then pins, then commits per set in receipt order. Sets it
// has to create come up with retention paused.
func (x *Index) ReIndex(ctx context.Context) error {
	// sets that lose a commit to a tombstone this database had not seen
	touched := map[int64][][]byte{}

	if hw, ok := x.ObjectStore.(backup.HeaderWalker); ok {
		err := hw.WalkHeaders(ctx, proto.ObjectType_TOMBSTONE, func(hdr *proto.ObjectHeader) error {
			at := x.now()
			if hdr.GetTimestamp() != nil {
				at = hdr.GetTimestamp().AsTime()
			}

			ref := hdr.GetTombstoneFor().GetHash()

			setID, err := x.applyTombstone(ctx, ref, at)
			if setID != 0 {
				touched[setID] = append(touched[setID], ref)
			}

			return err
		})
		if err != nil && !errors.Is(err, backup.ErrNotImplemented) {
			return err
		}
	} else {
		x.logger().Warn("store cannot walk headers, the rebuild does not honour tombstones", "store", fmt.Sprintf("%T", x.ObjectStore))
	}

	for setID, refs := range touched {
		if err := x.pruneRefs(ctx, setID, refs); err != nil {
			return err
		}

		if err := x.dropDeadRows(ctx, setID); err != nil {
			return err
		}
	}

	err := x.ObjectStore.Walk(ctx, true, proto.ObjectType_PIN, func(obj *proto.Object) error {
		ref := obj.Ref()

		gone, err := isDeleted(ctx, x.client, ref.Hash)
		if err != nil || gone {
			return err
		}

		return x.indexPin(ctx, obj.GetPin(), ref, false)
	})
	if err != nil {
		return err
	}

	type pending struct {
		commit *proto.Commit
		ref    *proto.Ref
	}

	type setKey struct {
		id   int64
		name string
	}

	bySet := make(map[setKey][]pending)

	err = x.ObjectStore.Walk(ctx, true, proto.ObjectType_COMMIT, func(obj *proto.Object) error {
		ref := obj.Ref()

		gone, err := isDeleted(ctx, x.client, ref.Hash)
		if err != nil || gone {
			return err
		}

		commit := obj.GetCommit()
		key := setKey{id: int64(commit.GetSetId())}
		if key.id == 0 {
			key.name = commit.GetBackupSet()
		}

		bySet[key] = append(bySet[key], pending{commit: commit, ref: ref})

		return nil
	})
	if err != nil {
		return err
	}

	for _, commits := range bySet {
		sort.Slice(commits, func(i, j int) bool {
			return commits[i].commit.GetReceivedAtNs() < commits[j].commit.GetReceivedAtNs()
		})

		for _, c := range commits {
			err := x.indexCommit(ctx, c.commit, c.ref, false, false)
			if err != nil {
				return err
			}
		}

		setID, err := x.ensureSet(ctx, x.client, commits[0].commit, commits[0].ref, false)
		if err != nil {
			return err
		}

		if setID == 0 {
			continue
		}

		err = x.reevaluateSet(ctx, setID)
		if err != nil {
			return err
		}
	}

	err = x.resetSetSequence(ctx)
	if err != nil {
		return err
	}

	return x.findDamage(ctx)
}

// resetSetSequence moves the set id sequence past the ids a rebuild
// recreated, where the database generates ids from a sequence.
func (x *Index) resetSetSequence(ctx context.Context) error {
	if !x.locking {
		return nil
	}

	_, err := x.client.ExecContext(ctx, `SELECT setval(pg_get_serial_sequence('sets', 'id'), GREATEST((SELECT COALESCE(MAX(id), 1) FROM sets), 1))`)

	return err
}

// applyTombstone records a tombstone found in the archives and marks the
// commit or pin row it names when this database still has it live; it
// returns the set of a commit it marked.
func (x *Index) applyTombstone(ctx context.Context, ref []byte, at time.Time) (int64, error) {
	var setID int64

	err := x.tx(ctx, func(tx *ent.Tx) error {
		c := tx.Client()

		err := recordDeleted(ctx, c, ref, at)
		if err != nil {
			return err
		}

		n, err := c.CommitRow.Update().Where(commitrow.Ref(ref), commitrow.TombstonedAtIsNil()).SetTombstonedAt(at).ClearPresence().Save(ctx)
		if err != nil {
			return err
		}

		if n > 0 {
			row, err := c.CommitRow.Query().Where(commitrow.Ref(ref)).Only(ctx)
			if err != nil {
				return err
			}

			setID = row.SetID

			_, err = c.SetRef.Delete().Where(setref.Ref(ref)).Exec(ctx)
			if err != nil {
				return err
			}
		}

		return c.Pin.Update().Where(pin.Ref(ref), pin.DeletedAtIsNil()).SetDeletedAt(at).Exec(ctx)
	})

	return setID, err
}

// References implements backup.RefScope: a commit, tree or file ref is
// readable once an indexed commit of any set names it.
func (x *Index) References(ctx context.Context, ref *proto.Ref) (bool, error) {
	return x.client.SetRef.Query().Where(setref.Ref(ref.GetHash())).Exist(ctx)
}

// Reachable reports whether a commit of one of the named sets references
// ref, which is what a caller limited to those sets may read.
func (x *Index) Reachable(ctx context.Context, sets []string, ref *proto.Ref) (bool, error) {
	return x.client.SetRef.Query().Where(setref.Ref(ref.GetHash()), setref.HasSetWith(set.NameIn(sets...))).Exist(ctx)
}

// liveCommit is the condition under which a commits row is offered.
func liveCommit() predicate.CommitRow {
	return commitrow.And(commitrow.TombstonedAtIsNil(), commitrow.RetireAtIsNil(), commitrow.DeletedAtIsNil())
}

// heldByCommit is the condition that a versions row's validity range holds
// a commit of the set that passes filter. The files and trees tables name
// their validity columns alike, so it serves both.
func heldByCommit(filter func(t *entsql.SelectTable) *entsql.Predicate) func(s *entsql.Selector) {
	return func(s *entsql.Selector) {
		c := entsql.Table(commitrow.Table)
		s.Where(entsql.Exists(entsql.Select().From(c).Where(entsql.And(
			entsql.ColumnsEQ(c.C(commitrow.FieldSetID), s.C(file.FieldSetID)),
			filter(c),
			entsql.ColumnsGTE(c.C(commitrow.FieldReceivedAt), s.C(file.FieldValidFrom)),
			entsql.Or(entsql.IsNull(s.C(file.FieldValidUntil)), entsql.ColumnsLT(c.C(commitrow.FieldReceivedAt), s.C(file.FieldValidUntil))),
		))))
	}
}

func liveCommitColumns(c *entsql.SelectTable) *entsql.Predicate {
	return entsql.And(entsql.IsNull(c.C(commitrow.FieldTombstonedAt)), entsql.IsNull(c.C(commitrow.FieldRetireAt)), entsql.IsNull(c.C(commitrow.FieldDeletedAt)))
}

// FileInfo lists the versions of a path that a live commit of the set
// contains, newest first.
func (x *Index) FileInfo(ctx context.Context, backupSet string, name string, notAfter time.Time, count int) ([]*proto.TreeNode, error) {
	setID, err := findSet(ctx, x.client, backupSet)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	rows, err := x.client.File.Query().
		Where(file.SetID(setID), file.Path(name), file.ValidFromLTE(notAfter.UTC()), predicate.File(heldByCommit(liveCommitColumns))).
		Order(ent.Desc(file.FieldValidFrom)).Limit(count).All(ctx)
	if err != nil {
		return nil, err
	}

	return mapAll(rows, m.TreeNode), nil
}

// ReadDir lists what a set held directly under dir at notAfter, sorted by
// name. Directory entries carry a name and a ref only.
func (x *Index) ReadDir(ctx context.Context, backupSet string, dir string, notAfter time.Time) ([]*proto.TreeNode, error) {
	setID, err := findSet(ctx, x.client, backupSet)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	at := notAfter.UTC()

	fileRows, err := x.client.File.Query().Where(
		file.SetID(setID), file.Dir(dir), file.ValidFromLTE(at),
		file.Or(file.ValidUntilIsNil(), file.ValidUntilGT(at)),
		predicate.File(heldByCommit(liveCommitColumns)),
	).All(ctx)
	if err != nil {
		return nil, err
	}

	treeRows, err := x.client.Tree.Query().Where(
		tree.SetID(setID), tree.Dir(dir), tree.ValidFromLTE(at),
		tree.Or(tree.ValidUntilIsNil(), tree.ValidUntilGT(at)),
		predicate.Tree(heldByCommit(liveCommitColumns)),
	).All(ctx)
	if err != nil {
		return nil, err
	}

	entries := mapAll(fileRows, m.TreeNode)
	for _, row := range treeRows {
		entries = append(entries, &proto.TreeNode{
			Stat: &proto.FileInfo{Name: mapping.Base(row.Path), Type: proto.NodeType_NODE_DIRECTORY},
			Ref:  mapping.Ref(row.Ref),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].GetStat().GetName(), entries[j].GetStat().GetName()) < 0
	})

	return entries, nil
}

// CommitInfo lists the live, complete commits of a set, newest first.
func (x *Index) CommitInfo(ctx context.Context, backupSet string, notAfter time.Time, count int) ([]*proto.Commit, error) {
	setID, err := findSet(ctx, x.client, backupSet)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	rows, err := x.client.CommitRow.Query().
		Where(commitrow.SetID(setID), commitrow.TimestampLTE(notAfter.UTC()), commitrow.Partial(false), liveCommit()).
		WithSet().Order(ent.Desc(commitrow.FieldReceivedAt)).Limit(count).All(ctx)
	if err != nil {
		return nil, err
	}

	return mapAll(rows, m.Commit), nil
}

// CommitDetail is a commit together with what the index knows about it
// beyond the stored object: how big the set was when it was taken, and
// which retention rule is keeping it.
type CommitDetail struct {
	Commit *proto.Commit
	// Ref is the commit object's own ref, which names it to a caller that
	// wants to pin or read it.
	Ref *proto.Ref
	// LogicalSize is what the set's files held at this commit; nil when
	// nothing measured it, which is every commit written before the index
	// started recording it.
	LogicalSize *int64
	// RetainedBy names the retention rules keeping this commit, comma
	// separated: "last", "within", "pinned", "hourly", "daily", "weekly",
	// "monthly". It is empty for a commit retention has not evaluated yet
	// or has retired.
	RetainedBy string
}

// CommitDetails is CommitInfo with what the index knows about each commit
// beyond the object, for a caller reporting on a set rather than reading
// it back.
func (x *Index) CommitDetails(ctx context.Context, backupSet string, notAfter time.Time, count int) ([]CommitDetail, error) {
	setID, err := findSet(ctx, x.client, backupSet)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	rows, err := x.client.CommitRow.Query().
		Where(commitrow.SetID(setID), commitrow.TimestampLTE(notAfter.UTC()), commitrow.Partial(false), liveCommit()).
		WithSet().Order(ent.Desc(commitrow.FieldReceivedAt)).Limit(count).All(ctx)
	if err != nil {
		return nil, err
	}

	return mapAll(rows, func(row *ent.CommitRow) CommitDetail {
		return CommitDetail{
			Commit:      m.Commit(row),
			Ref:         &proto.Ref{Hash: row.Ref},
			LogicalSize: row.LogicalSize,
			RetainedBy:  row.RetainedBy,
		}
	}), nil
}

// LatestCommit returns the set's newest commit without a tombstone,
// partial or not, or backup.ErrNotFound.
func (x *Index) LatestCommit(ctx context.Context, backupSet string) (*proto.Ref, error) {
	setID, err := findSet(ctx, x.client, backupSet)
	if err != nil {
		return nil, err
	}

	row, err := x.client.CommitRow.Query().Where(commitrow.SetID(setID), commitrow.TombstonedAtIsNil()).
		Order(ent.Desc(commitrow.FieldReceivedAt), ent.Desc(commitrow.FieldTimestamp)).Select(commitrow.FieldRef).First(ctx)
	if ent.IsNotFound(err) {
		return nil, backup.ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	return &proto.Ref{Hash: row.Ref}, nil
}

// Presence implements backup.PresenceIndex.
func (x *Index) Presence(ctx context.Context, scope backup.PresenceScope, backupSet string) ([]*proto.PresenceFilter, error) {
	return loadPresence(ctx, x.client, scope, backupSet)
}

func mapAll[S, T any](in []S, f func(S) T) []T {
	if len(in) == 0 {
		return nil
	}

	out := make([]T, len(in))
	for i, s := range in {
		out[i] = f(s)
	}

	return out
}

// logicalSize is what the set's files hold at a moment: the recorded size
// of every file version open then. Directories and symlinks carry no
// content and are left out.
func logicalSize(ctx context.Context, c *ent.Client, setID int64, at time.Time) (int64, error) {
	var sums []struct {
		Sum *int64 `sql:"sum"`
	}

	err := c.File.Query().Where(
		file.SetID(setID),
		file.TypeEQ(uint32(proto.NodeType_NODE_FILE)),
		file.ValidFromLTE(at),
		file.Or(file.ValidUntilIsNil(), file.ValidUntilGT(at)),
	).Aggregate(ent.Sum(file.FieldSize)).Scan(ctx, &sums)
	if err != nil {
		return 0, err
	}

	if len(sums) == 0 {
		return 0, nil
	}

	return deref(sums[0].Sum), nil
}

// FillMissingSizes records the logical size of every commit that carries
// none, and reports how many it filled. A tombstoned commit is skipped:
// the file versions it held may already be pruned, so its size can no
// longer be worked out.
func (x *Index) FillMissingSizes(ctx context.Context) (int, error) {
	rows, err := x.client.CommitRow.Query().Where(commitrow.LogicalSizeIsNil(), commitrow.TombstonedAtIsNil()).All(ctx)
	if err != nil {
		return 0, err
	}

	filled := 0
	for _, row := range rows {
		size, err := logicalSize(ctx, x.client, row.SetID, row.ReceivedAt)
		if err != nil {
			return filled, err
		}

		err = x.client.CommitRow.UpdateOneID(row.ID).SetLogicalSize(size).Exec(ctx)
		if err != nil {
			return filled, err
		}

		filled++
	}

	return filled, nil
}
