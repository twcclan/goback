package sql

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/presence"
	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/index"
	"github.com/twcclan/goback/index/sql/ent/commitrow"
	"github.com/twcclan/goback/index/sql/ent/pin"
	"github.com/twcclan/goback/index/sql/ent/set"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func TestIndexerRangesFollowVersions(t *testing.T) {
	f := newFixture(t)

	t0 := f.clock
	rootA := f.tree(f.file("a.txt", "one"), f.dir("world", f.file("level.dat", "w1")))
	f.commit("world", rootA, false)

	f.advance(time.Hour)
	t1 := f.clock
	rootB := f.tree(f.file("a.txt", "two"), f.dir("world", f.file("level.dat", "w1")))
	f.commit("world", rootB, false)

	f.advance(time.Hour)
	t2 := f.clock
	rootC := f.tree(f.dir("world", f.file("level.dat", "w2")), f.dir("logs", f.file("x.log", "l")))
	c := f.commit("world", rootC, false)

	files := f.ranges(f.x, "files")
	require.Len(t, files, 5)

	require.Equal(t, "a.txt", files[0].path)
	require.True(t, files[0].validFrom.Equal(t0))
	closedAt(t, files[0], t1)
	require.Equal(t, "a.txt", files[1].path)
	require.True(t, files[1].validFrom.Equal(t1))
	closedAt(t, files[1], t2)

	require.Equal(t, "logs/x.log", files[2].path)
	require.Nil(t, files[2].validUntil)

	require.Equal(t, "world/level.dat", files[3].path)
	require.True(t, files[3].validFrom.Equal(t0))
	closedAt(t, files[3], t2)
	require.Equal(t, "world/level.dat", files[4].path)
	require.True(t, files[4].validFrom.Equal(t2))
	require.Nil(t, files[4].validUntil)

	trees := f.ranges(f.x, "trees")
	require.Len(t, trees, 3, "world is closed and reopened once, logs opened once: %+v", trees)

	versions, err := f.x.FileInfo(f.ctx, "world", "world/level.dat", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	require.Equal(t, "level.dat", string(versions[0].Stat.Name))
	require.EqualValues(t, 2, versions[0].Stat.Size)
	require.Equal(t, f.epoch.UnixNano(), versions[0].Stat.MtimeNs)

	versions, err = f.x.FileInfo(f.ctx, "world", "a.txt", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, versions, 2, "both versions are held by live commits")

	commits, err := f.x.CommitInfo(f.ctx, "world", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, commits, 3)
	require.Equal(t, t2.UnixNano(), commits[0].ReceivedAtNs)
	require.Equal(t, "world", commits[0].BackupSet)
	require.NotZero(t, commits[0].SetId)
	require.True(t, commits[0].Tree.Equal(rootC.Ref()))

	latest, err := f.x.LatestCommit(f.ctx, "world")
	require.NoError(t, err)
	require.True(t, latest.Equal(c))

	// the same tree at a later commit changes nothing
	f.advance(time.Hour)
	f.commit("world", rootC, false)
	require.Len(t, f.ranges(f.x, "files"), 5)
}

func TestIndexerKeepsOrderAndRefusesClosedSets(t *testing.T) {
	f := newFixture(t)

	root := f.tree(f.file("a.txt", "one"))
	first := f.commit("world", root, false)

	// the index clock stepping back cannot reorder a set's commits: the
	// next receipt is stamped after the newest
	f.advance(-time.Hour)
	stale := proto.NewObject(&proto.Commit{Timestamp: f.clock.Unix(), Tree: root.Ref(), BackupSet: "world", AgentId: "node-1"})
	require.NoError(t, f.x.Put(f.ctx, stale))
	latest, err := f.x.LatestCommit(f.ctx, "world")
	require.NoError(t, err)
	require.False(t, latest.Equal(first), "the commit received last is the newest")
	f.advance(time.Hour)

	require.NoError(t, f.x.DeleteSet(f.ctx, "world", false))

	f.advance(time.Hour)
	fresh := proto.NewObject(&proto.Commit{Timestamp: f.clock.Unix(), Tree: root.Ref(), BackupSet: "world", AgentId: "node-1"})
	require.ErrorIs(t, f.x.Put(f.ctx, fresh), backup.ErrSetClosed)

	require.NoError(t, f.x.UndeleteSet(f.ctx, "world"))
	require.NoError(t, f.x.Put(f.ctx, fresh))
}

func TestRetentionLifecycle(t *testing.T) {
	f := newFixture(t)

	a := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	require.NoError(t, f.x.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 2}))
	f.advance(time.Hour)
	b := f.commit("world", f.tree(f.file("a.txt", "two")), false)
	f.advance(time.Hour)
	c := f.commit("world", f.tree(f.file("a.txt", "three")), false)

	row := f.commitRow(a)
	require.Empty(t, row.RetainedBy)
	require.NotNil(t, row.RetireAt, "a is beyond keep_last 2")
	sameInstant(t, row.ExpiresAt, f.clock.Add(14*24*time.Hour))

	row = f.commitRow(c)
	require.Equal(t, "latest,last", row.RetainedBy)
	require.Nil(t, row.RetireAt)

	commits, err := f.x.CommitInfo(f.ctx, "world", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, commits, 2, "the retired commit is hidden")

	versions, err := f.x.FileInfo(f.ctx, "world", "a.txt", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, versions, 2, "the version only a retired commit holds is not offered")

	require.ErrorIs(t, f.x.DeleteCommit(f.ctx, c), backup.ErrNewestCommit)
	require.NoError(t, f.x.DeleteCommit(f.ctx, b))

	row = f.commitRow(b)
	require.NotNil(t, row.DeletedAt)
	sameInstant(t, row.ExpiresAt, f.clock.Add(14*24*time.Hour))

	require.NoError(t, f.x.UndeleteCommit(f.ctx, b))
	require.Nil(t, f.commitRow(b).DeletedAt)

	n, err := f.x.Retire(f.ctx, f.clock)
	require.NoError(t, err)
	require.Zero(t, n, "nothing is past its window yet")

	n, err = f.x.Retire(f.ctx, f.clock.Add(15*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.True(t, f.store.tombstoned(a))
	require.False(t, f.store.tombstoned(b))
	require.Equal(t, 1, f.store.flushed)

	require.NotNil(t, f.commitRow(a).TombstonedAt)

	require.Len(t, f.ranges(f.x, "files"), 2, "the version only a held was dropped")

	require.ErrorIs(t, f.x.UndeleteCommit(f.ctx, a), backup.ErrTombstoned)
	_, err = f.pin(a)
	require.ErrorIs(t, err, backup.ErrTombstoned)

	latest, err := f.x.LatestCommit(f.ctx, "world")
	require.NoError(t, err)
	require.True(t, latest.Equal(c))
}

func TestRetireMarksRowsOnlyAfterFlush(t *testing.T) {
	f := newFixture(t)

	a := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	require.NoError(t, f.x.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 1}))
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)

	f.store.flushErr = errors.New("disk full")
	later := f.clock.Add(15 * 24 * time.Hour)

	_, err := f.x.Retire(f.ctx, later)
	require.ErrorIs(t, err, f.store.flushErr)
	require.True(t, f.store.tombstoned(a))

	require.Nil(t, f.commitRow(a).TombstonedAt, "the row waits for the tombstone to be durable")
	require.True(t, f.deleted(f.x, a), "but the commit is spoken for from the moment the tombstone is written")

	// nothing revives a commit whose tombstone is on its way
	require.ErrorIs(t, f.x.UndeleteCommit(f.ctx, a), backup.ErrTombstoned)
	require.ErrorIs(t, f.x.DeleteCommit(f.ctx, a), backup.ErrTombstoned)
	_, err = f.pin(a)
	require.ErrorIs(t, err, backup.ErrTombstoned)

	f.store.flushErr = nil
	n, err := f.x.Retire(f.ctx, later)
	require.NoError(t, err)
	require.Equal(t, 1, n, "the next run tombstones the commit again")

	require.NotNil(t, f.commitRow(a).TombstonedAt)
	require.True(t, f.deleted(f.x, a))

	readable, err := f.x.References(f.ctx, a)
	require.NoError(t, err)
	require.False(t, readable, "a tombstoned commit is no longer readable")

	_, err = f.pin(a)
	require.ErrorIs(t, err, backup.ErrTombstoned)
}

// TestPinsFollowTheSetState: a closing set accepts neither pins nor
// undeletes, or its commits would outlive the deletion.
func TestPinsFollowTheSetState(t *testing.T) {
	f := newFixture(t)

	a := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)

	require.NoError(t, f.x.DeleteSet(f.ctx, "world", false))
	_, err := f.pin(a)
	require.ErrorIs(t, err, backup.ErrSetClosed)
	require.ErrorIs(t, f.x.UndeleteCommit(f.ctx, a), backup.ErrSetClosed)

	n, err := f.x.Retire(f.ctx, f.clock.Add(15*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestDeleteCommitRefusesAPinnedCommit(t *testing.T) {
	f := newFixture(t)

	a := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)

	p, err := f.pin(a)
	require.NoError(t, err)
	require.ErrorIs(t, f.x.DeleteCommit(f.ctx, a), backup.ErrPinned)

	require.NoError(t, f.x.Unpin(f.ctx, p))
	require.NoError(t, f.x.DeleteCommit(f.ctx, a))
}

func TestRebuiltSetKeepsEverythingWhilePaused(t *testing.T) {
	f := newFixture(t)

	var refs []*proto.Ref
	for i := 0; i < 3; i++ {
		refs = append(refs, f.commit("world", f.tree(f.file("a.txt", fmt.Sprint(i))), false))
		f.advance(time.Hour)
	}

	y := f.index()
	require.NoError(t, y.ReIndex(f.ctx))

	commits, err := y.CommitInfo(f.ctx, "world", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, commits, 3, "no policy applies while retention is paused")

	// the operator takes longer than the hold window to set a policy; the
	// commit it retires still gets a full window from then
	f.advance(20 * 24 * time.Hour)
	require.NoError(t, y.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 2}))

	row := f.commitRowIn(y, refs[0])
	sameInstant(t, row.RetireAt, f.clock)
	sameInstant(t, row.ExpiresAt, f.clock.Add(14*24*time.Hour))

	n, err := y.Retire(f.ctx, f.clock)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestDeletedSetProceedsWhilePaused(t *testing.T) {
	f := newFixture(t)

	f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)

	y := f.index()
	require.NoError(t, y.ReIndex(f.ctx))
	require.NoError(t, y.DeleteSet(f.ctx, "world", true))

	n, err := y.Retire(f.ctx, f.clock)
	require.NoError(t, err)
	require.Equal(t, 2, n, "erasure does not wait for a policy")

	require.Equal(t, index.SetDeleted, f.setState(y, "world"))
}

// TestTombstonePrunesTheRefsOnlyItReaches retires a commit and expects the
// store to lose read access to its tree and file, but not to the subtree
// it shares with the commit that stays.
func TestTombstonePrunesTheRefsOnlyItReaches(t *testing.T) {
	f := newFixture(t)

	shared := f.dir("shared", f.file("s.txt", "same"))
	one := f.file("a.txt", "one")
	old := f.tree(shared, one)
	a := f.commit("world", old, false)

	f.advance(time.Hour)
	two := f.file("a.txt", "two")
	next := f.tree(shared, two)
	b := f.commit("world", next, false)

	readable := func(ref *proto.Ref) bool {
		ok, err := f.x.References(f.ctx, ref)
		require.NoError(t, err)
		return ok
	}

	for _, ref := range []*proto.Ref{a, old.Ref(), one.Ref, shared.Ref, b, next.Ref(), two.Ref} {
		require.True(t, readable(ref), "%x before the tombstone", ref.Hash)
	}

	require.NoError(t, f.x.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 1}))
	n, err := f.x.Retire(f.ctx, f.clock.Add(15*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, n)

	for _, ref := range []*proto.Ref{a, old.Ref(), one.Ref} {
		require.False(t, readable(ref), "%x only the tombstoned commit reaches", ref.Hash)
	}

	for _, ref := range []*proto.Ref{shared.Ref, b, next.Ref(), two.Ref} {
		require.True(t, readable(ref), "%x the live commit reaches", ref.Hash)
	}
}

func TestReIndexReconcilesTombstonesOnAnExistingDatabase(t *testing.T) {
	f := newFixture(t)

	old := f.tree(f.file("a.txt", "one"))
	a := f.commit("world", old, false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)

	// a database backup taken while both commits were live
	y := f.index()
	require.NoError(t, y.ReIndex(f.ctx))
	require.NoError(t, y.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 100}))
	require.Len(t, f.ranges(y, "files"), 2)

	// the live database retires the older commit meanwhile
	require.NoError(t, f.x.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 1}))
	n, err := f.x.Retire(f.ctx, f.clock.Add(15*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// the backup, restored and rebuilt, honours the tombstone it predates
	require.NoError(t, y.ReIndex(f.ctx))

	require.NotNil(t, f.commitRowIn(y, a).TombstonedAt)

	commits, err := y.CommitInfo(f.ctx, "world", f.clock.Add(time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, commits, 1)
	require.Len(t, f.ranges(y, "files"), 1, "the tombstoned version's row is gone")

	readable, err := y.References(f.ctx, a)
	require.NoError(t, err)
	require.False(t, readable)

	readable, err = y.References(f.ctx, old.Ref())
	require.NoError(t, err)
	require.False(t, readable, "the tombstoned commit's tree is unreadable too")
}

func TestRestoreLeasesHoldRetirement(t *testing.T) {
	f := newFixture(t)

	a := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	require.NoError(t, f.x.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 1}))
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)

	later := f.clock.Add(15 * 24 * time.Hour)
	f.store.leases = []*proto.Ref{a}

	n, err := f.x.Retire(f.ctx, later)
	require.NoError(t, err)
	require.Zero(t, n, "a commit a restore reads is not tombstoned")
	require.False(t, f.store.tombstoned(a))

	f.store.leases = nil
	n, err = f.x.Retire(f.ctx, later)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.True(t, f.store.tombstoned(a))
}

func TestPinsKeepCommitsAlive(t *testing.T) {
	f := newFixture(t)

	a := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	require.NoError(t, f.x.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 1}))
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)

	require.NotNil(t, f.commitRow(a).RetireAt)

	p, err := f.pin(a)
	require.NoError(t, err)

	row := f.commitRow(a)
	require.Equal(t, "pinned", row.RetainedBy)
	require.Nil(t, row.RetireAt)
	require.Nil(t, row.ExpiresAt)

	pins, err := f.x.Pins(f.ctx)
	require.NoError(t, err)
	require.Len(t, pins, 1)
	require.True(t, pins[0].Ref.Equal(p))
	require.True(t, pins[0].Target.Equal(a))
	require.NotZero(t, pins[0].ReceivedAtNs)

	n, err := f.x.Retire(f.ctx, f.clock.Add(30*24*time.Hour))
	require.NoError(t, err)
	require.Zero(t, n)

	f.advance(24 * time.Hour)
	require.NoError(t, f.x.Unpin(f.ctx, p))
	require.True(t, f.store.tombstoned(p))
	require.ErrorIs(t, f.x.Unpin(f.ctx, p), backup.ErrTombstoned)

	pins, err = f.x.Pins(f.ctx)
	require.NoError(t, err)
	require.Empty(t, pins)

	row = f.commitRow(a)
	require.Empty(t, row.RetainedBy)
	sameInstant(t, row.RetireAt, f.clock)

	n, err = f.x.Retire(f.ctx, f.clock.Add(15*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.True(t, f.store.tombstoned(a))
}

func TestPartialCheckpointRetiresWhenSuperseded(t *testing.T) {
	f := newFixture(t)

	full := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	partial := f.commit("world", f.tree(f.file("a.txt", "one"), f.file("b.txt", "half")), true)

	latest, err := f.x.LatestCommit(f.ctx, "world")
	require.NoError(t, err)
	require.True(t, latest.Equal(partial), "a checkpoint is the diff base")

	commits, err := f.x.CommitInfo(f.ctx, "world", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, commits, 1, "but not listed")

	row := f.commitRow(partial)
	require.Equal(t, "latest", row.RetainedBy)
	require.Nil(t, row.RetireAt)

	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "one"), f.file("b.txt", "full")), false)

	row = f.commitRow(partial)
	require.NotNil(t, row.RetireAt)
	sameInstant(t, row.ExpiresAt, f.clock)

	require.Nil(t, f.commitRow(full).RetireAt)

	n, err := f.x.Retire(f.ctx, f.clock)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.True(t, f.store.tombstoned(partial))
}

func TestDeleteSetAndRebuild(t *testing.T) {
	f := newFixture(t)

	worldA := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	worldB := f.commit("world", f.tree(f.file("a.txt", "two")), false)
	f.advance(time.Hour)
	logs := f.commit("logs", f.tree(f.file("x.log", "l")), false)
	p, err := f.pin(logs)
	require.NoError(t, err)

	require.NoError(t, f.x.DeleteSet(f.ctx, "world", true))

	n, err := f.x.Retire(f.ctx, f.clock)
	require.NoError(t, err)
	require.Equal(t, 2, n, "erasure uses a zero window")
	require.True(t, f.store.tombstoned(worldA))
	require.True(t, f.store.tombstoned(worldB))
	_, erased := f.store.erased[string(worldA.Hash)]
	require.True(t, erased, "an erased set writes erase tombstones")

	require.Equal(t, index.SetDeleted, f.setState(f.x, "world"))
	require.Len(t, f.ranges(f.x, "files"), 1, "only the logs row is left")

	require.ErrorIs(t, f.x.UndeleteSet(f.ctx, "world"), backup.ErrTombstoned)

	// a rebuild from the archives alone
	y := f.index()
	require.NoError(t, y.ReIndex(f.ctx))

	count, err := y.client.CommitRow.Query().Count(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "tombstoned commits are not indexed")
	require.True(t, f.deleted(y, worldA), "the rebuild records the tombstones")
	require.True(t, f.deleted(y, worldB))
	require.False(t, f.deleted(y, logs))

	// a resubmitted commit is stamped afresh: a new commit, not the tombstoned one
	obj, err := f.store.Get(f.ctx, worldA)
	require.NoError(t, err)
	require.NoError(t, y.Put(f.ctx, obj))
	require.False(t, obj.Ref().Equal(worldA))
	require.False(t, f.deleted(y, obj.Ref()))

	count, err = y.client.Pin.Query().Where(pin.DeletedAtIsNil()).Count(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	logsSet, err := y.client.Set.Query().Where(set.Name("logs")).Only(f.ctx)
	require.NoError(t, err)
	require.True(t, logsSet.RetentionPaused, "a rebuilt set waits for its policy")

	pins, err := y.Pins(f.ctx)
	require.NoError(t, err)
	require.Len(t, pins, 1)
	require.True(t, pins[0].Ref.Equal(p))

	require.NoError(t, y.SetPolicy(f.ctx, "logs", &retention.Policy{KeepLast: 1}))
	logsSet, err = y.client.Set.Get(f.ctx, logsSet.ID)
	require.NoError(t, err)
	require.False(t, logsSet.RetentionPaused)
}

func TestReIndexReproducesRanges(t *testing.T) {
	f := newFixture(t)

	f.commit("world", f.tree(f.file("a.txt", "one"), f.dir("world", f.file("level.dat", "w1"))), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two"), f.dir("world", f.file("level.dat", "w1"))), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.dir("world", f.file("level.dat", "w2"), f.dir("region", f.file("r.0", "r")))), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.dir("world", f.file("level.dat", "w2"))), false)

	y := f.index()
	require.NoError(t, y.ReIndex(f.ctx))

	for _, table := range []string{"files", "trees"} {
		require.Equal(t, f.ranges(f.x, table), f.ranges(y, table), table)
	}

	require.Len(t, f.ranges(f.x, "files"), 5)
	require.Len(t, f.ranges(f.x, "trees"), 4)
}

// TestPresenceFilterNeverLandsOnATombstonedCommit stores a presence
// filter for a commit after its tombstone and expects the row to stay
// without one.
func TestPresenceFilterNeverLandsOnATombstonedCommit(t *testing.T) {
	f := newFixture(t)

	a := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)

	require.NoError(t, f.x.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 1}))
	n, err := f.x.Retire(f.ctx, f.clock.Add(15*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, n)

	setID, err := findSet(f.ctx, f.x.client, "world")
	require.NoError(t, err)

	filter := presence.New(1)
	filter.Add(a.Hash)
	require.NoError(t, storePresence(f.ctx, f.x, setID, a, filter.Proto()))

	require.Empty(t, f.commitRow(a).Presence, "a tombstoned commit carries no filter")

	stored, err := f.x.client.CommitRow.Query().Where(commitrow.PresenceNotNil()).Count(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, stored, "the live head keeps its filter")
}

// TestCommitInfoReproducesTheRef: a listed commit carries its whole hashed
// body, so a restore can name it by ref again.
func TestCommitInfoReproducesTheRef(t *testing.T) {
	f := newFixture(t)

	parent := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)

	obj := proto.NewObject(&proto.Commit{
		Timestamp:     f.clock.Unix(),
		Tree:          f.tree(f.file("a.txt", "two")).Ref(),
		BackupSet:     "world",
		Parent:        parent,
		AgentId:       "node-1",
		ScanStartNs:   f.clock.Add(-time.Minute).UnixNano(),
		PolicyVersion: 3,
		Consistent:    true,
	})
	require.NoError(t, f.x.Put(f.ctx, obj))
	f.presence()

	commits, err := f.x.CommitInfo(f.ctx, "world", f.clock, 1)
	require.NoError(t, err)
	require.Len(t, commits, 1)
	require.True(t, proto.NewObject(commits[0]).Ref().Equal(obj.Ref()), "listed commit %+v hashes differently", commits[0])
	require.True(t, commits[0].GetParent().Equal(parent))
}

// TestConcurrentPinsOfOneSet pins several commits of a set at once; every
// pin re-evaluates the set, which must not deadlock on the commit rows.
func TestConcurrentPinsOfOneSet(t *testing.T) {
	f := newFixture(t)

	var commits []*proto.Ref
	for i := range 6 {
		commits = append(commits, f.commit("world", f.tree(f.file("a.txt", fmt.Sprint(i))), false))
		f.advance(time.Minute)
	}

	for range 3 {
		var wg sync.WaitGroup
		results := make([]error, len(commits))
		for i, ref := range commits {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, results[i] = f.pin(ref)
			}()
		}
		wg.Wait()

		for i, err := range results {
			require.NoError(t, err, "pinning commit %d", i)
		}

		pins, err := f.x.Pins(f.ctx)
		require.NoError(t, err)
		for _, p := range pins {
			require.NoError(t, f.x.Unpin(f.ctx, p.Ref))
		}
	}
}

func TestPinsFollowTheirSet(t *testing.T) {
	f := newFixture(t)

	a := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	p, err := f.pin(a)
	require.NoError(t, err)

	pins, err := f.x.Pins(f.ctx)
	require.NoError(t, err)
	require.Len(t, pins, 1)

	// a pin whose commit's set the query cannot see is not there; the
	// row goes behind the foreign keys' back, as a policy would hide it
	_, err = f.x.db.ExecContext(f.ctx, "PRAGMA foreign_keys = OFF")
	require.NoError(t, err)
	require.NoError(t, f.x.client.Set.DeleteOneID(f.commitRow(a).SetID).Exec(f.ctx))

	pins, err = f.x.Pins(f.ctx)
	require.NoError(t, err)
	require.Empty(t, pins)
	require.ErrorIs(t, f.x.Unpin(f.ctx, p), backup.ErrNotFound)
}

func TestSymlinksAreIndexed(t *testing.T) {
	f := newFixture(t)

	t0 := f.clock
	rootA := f.tree(f.file("a.txt", "one"), f.symlink("current", "worlds/2026"))
	f.commit("world", rootA, false)

	versions, err := f.x.FileInfo(f.ctx, "world", "current", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, proto.NodeType_NODE_SYMLINK, versions[0].Stat.Type)
	require.Equal(t, "worlds/2026", string(versions[0].Stat.LinkTarget))
	require.Nil(t, versions[0].Ref, "a symlink has no object of its own")

	// a symlink that still points at the same place is one version
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "one"), f.symlink("current", "worlds/2026")), false)

	versions, err = f.x.FileInfo(f.ctx, "world", "current", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, versions, 1)

	// repointing it closes the old version and opens a new one
	f.advance(time.Hour)
	t2 := f.clock
	f.commit("world", f.tree(f.file("a.txt", "one"), f.symlink("current", "worlds/2027")), false)

	versions, err = f.x.FileInfo(f.ctx, "world", "current", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	require.Equal(t, "worlds/2027", string(versions[0].Stat.LinkTarget))
	require.Equal(t, "worlds/2026", string(versions[1].Stat.LinkTarget))

	var links []rangeRow
	for _, row := range f.ranges(f.x, "files") {
		if row.path == "current" {
			links = append(links, row)
		}
	}

	require.Len(t, links, 2)
	require.True(t, links[0].validFrom.Equal(t0))
	closedAt(t, links[0], t2)
	require.Nil(t, links[1].validUntil)
}

func TestASymlinkReplacedByAFileIsANewVersion(t *testing.T) {
	f := newFixture(t)

	f.commit("world", f.tree(f.symlink("current", "worlds/2026")), false)

	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("current", "no longer a link")), false)

	versions, err := f.x.FileInfo(f.ctx, "world", "current", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	require.Equal(t, proto.NodeType_NODE_FILE, versions[0].Stat.Type)
	require.NotNil(t, versions[0].Ref)
	require.Empty(t, versions[0].Stat.LinkTarget)
	require.Equal(t, proto.NodeType_NODE_SYMLINK, versions[1].Stat.Type)
}

func TestReadDirListsWhatTheSetHeldThen(t *testing.T) {
	f := newFixture(t)

	t0 := f.clock
	f.commit("world", f.tree(
		f.file("a.txt", "one"),
		f.symlink("current", "worlds/2026"),
		f.dir("world", f.file("level.dat", "w1"), f.dir("region", f.file("r.0", "r"))),
	), false)

	f.advance(time.Hour)
	t1 := f.clock
	f.commit("world", f.tree(
		f.file("b.txt", "two"),
		f.dir("world", f.file("level.dat", "w2")),
	), false)

	x, ok := interface{}(f.x).(backup.DirLister)
	require.True(t, ok)

	names := func(entries []*proto.TreeNode) []string {
		out := make([]string, len(entries))
		for i, e := range entries {
			out[i] = string(e.GetStat().GetName())
		}
		return out
	}

	entries, err := x.ReadDir(f.ctx, "world", "", t0)
	require.NoError(t, err)
	require.Equal(t, []string{"a.txt", "current", "world"}, names(entries))
	require.Equal(t, proto.NodeType_NODE_DIRECTORY, entries[2].Stat.Type)
	require.NotNil(t, entries[2].Ref, "a directory entry can be descended into")

	entries, err = x.ReadDir(f.ctx, "world", "", t1)
	require.NoError(t, err)
	require.Equal(t, []string{"b.txt", "world"}, names(entries), "the old root is gone at t1")

	entries, err = x.ReadDir(f.ctx, "world", "world", t0)
	require.NoError(t, err)
	require.Equal(t, []string{"level.dat", "region"}, names(entries))
	require.EqualValues(t, 2, entries[0].Stat.Size)

	entries, err = x.ReadDir(f.ctx, "world", "world", t1)
	require.NoError(t, err)
	require.Equal(t, []string{"level.dat"}, names(entries), "region went away")

	entries, err = x.ReadDir(f.ctx, "world", "nowhere", t1)
	require.NoError(t, err)
	require.Empty(t, entries)

	entries, err = x.ReadDir(f.ctx, "other", "", t1)
	require.NoError(t, err)
	require.Empty(t, entries, "an unknown set lists nothing")
}

func TestCommitDetailsCarryTheSizeAndWhyEachCommitIsKept(t *testing.T) {
	f := newFixture(t)

	a := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	require.NoError(t, f.x.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 2}))
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two"), f.file("b.txt", "three")), false)

	held, err := f.x.CommitDetails(f.ctx, "world", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, held, 2)

	require.Equal(t, "latest,last", held[0].RetainedBy, "the newest commit, and within keep_last")
	require.Equal(t, "last", held[1].RetainedBy)
	require.NotNil(t, held[0].LogicalSize)
	require.EqualValues(t, 8, *held[0].LogicalSize, "both files of the newest commit")
	require.EqualValues(t, 3, *held[1].LogicalSize, "only a.txt was there then")

	// the commit itself is the same one CommitInfo returns
	commits, err := f.x.CommitInfo(f.ctx, "world", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, commits, 2)
	require.Equal(t, commits[0].GetReceivedAtNs(), held[0].Commit.GetReceivedAtNs())

	// a commit retention has retired is not offered, as with CommitInfo
	require.NoError(t, f.x.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 1}))

	held, err = f.x.CommitDetails(f.ctx, "world", f.clock, 10)
	require.NoError(t, err)
	require.Len(t, held, 1)
	require.NotEqual(t, a.String(), held[0].Commit.GetTree().String())
}
