package sql

import (
	"time"

	"testing"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func TestDamagedPathsTravelWithTheGrantUntilARunCoversThem(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	_, err := f.x.BeginCommit(ctx, "world")
	require.NoError(t, err)

	world := f.setID("world")
	require.NoError(t, f.x.MarkDamaged(ctx, world, []string{"world/level.dat", "world/region/r.0.0.mca"}))
	require.NoError(t, f.x.MarkDamaged(ctx, world, []string{"world/level.dat"}), "the same path twice is one row")
	require.NoError(t, f.x.MarkDamaged(ctx, world+1, []string{"anything"}), "a set that does not exist is ignored")

	grant, err := f.x.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.False(t, grant.Rescan)
	require.Equal(t, []string{"world/level.dat", "world/region/r.0.0.mca"}, grant.Damaged)

	// a checkpoint does not cover the set, so the paths stay
	tree := f.tree(f.file("a.txt", "one")).Ref()
	checkpoint := proto.NewObject(&proto.Commit{
		Timestamp: f.clock.Unix(), Tree: tree, BackupSet: "world", AgentId: "node-1",
		ScanStartNs: f.clock.UnixNano(), Partial: true,
	})
	require.NoError(t, f.x.Put(ctx, checkpoint))

	grant, err = f.x.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.Len(t, grant.Damaged, 2)

	full := proto.NewObject(&proto.Commit{
		Timestamp: f.clock.Add(time.Second).Unix(), Tree: f.tree(f.file("b.txt", "two")).Ref(),
		BackupSet: "world", AgentId: "node-1", ScanStartNs: f.clock.Add(time.Second).UnixNano(),
	})
	require.NoError(t, f.x.Put(ctx, full))

	grant, err = f.x.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.Empty(t, grant.Damaged, "a run that covered the set forgets them")
}

func TestARescanIsClearedByAFullRun(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	_, err := f.x.BeginCommit(ctx, "world")
	require.NoError(t, err)

	require.NoError(t, f.x.MarkRescan(ctx, f.setID("world")))

	grant, err := f.x.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.True(t, grant.Rescan)

	commit := proto.NewObject(&proto.Commit{
		Timestamp: f.clock.Unix(), Tree: f.tree(f.file("a.txt", "one")).Ref(),
		BackupSet: "world", AgentId: "node-1", ScanStartNs: f.clock.UnixNano(),
	})
	require.NoError(t, f.x.Put(ctx, commit))

	grant, err = f.x.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.False(t, grant.Rescan)
}

func TestAClosedVersionIsLostInTheCommitsThatHoldIt(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	old := f.file("level.dat", "the version that will be lost")
	first := f.commit("world", f.tree(old), false)

	f.clock = f.clock.Add(time.Hour)
	current := f.file("level.dat", "the version on disk today")
	second := f.commit("world", f.tree(current), false)

	require.NoError(t, f.x.MarkLost(ctx, []backup.FilePath{
		{Ref: old.Ref, SetID: f.setID("world"), Path: "level.dat"},
	}))

	lost, err := f.x.LostVersions(ctx, "world")
	require.NoError(t, err)
	require.Len(t, lost, 1)
	require.Equal(t, "level.dat", lost[0].Path)
	require.True(t, lost[0].Ref.Equal(old.Ref))
	require.Len(t, lost[0].Commits, 1, "only the commit holding that version is damaged")
	require.True(t, lost[0].Commits[0].Equal(first))

	grant, err := f.x.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.Empty(t, grant.Damaged, "nothing can read an old version again")

	// the newest version is untouched, so its commit restores whole
	require.NotEqual(t, first, second)
}

func TestAnOpenVersionStopsBeingLostOnceARunCoversIt(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	current := f.file("level.dat", "the version on disk")
	f.commit("world", f.tree(current), false)

	require.NoError(t, f.x.MarkLost(ctx, []backup.FilePath{
		{Ref: current.Ref, SetID: f.setID("world"), Path: "level.dat", Open: true},
	}))
	require.NoError(t, f.x.MarkDamaged(ctx, f.setID("world"), []string{"level.dat"}))

	lost, err := f.x.LostVersions(ctx, "world")
	require.NoError(t, err)
	require.Len(t, lost, 1)

	f.clock = f.clock.Add(time.Hour)
	f.commit("world", f.tree(f.file("level.dat", "the version on disk")), false)

	lost, err = f.x.LostVersions(ctx, "world")
	require.NoError(t, err)
	require.Empty(t, lost, "the run stored the open version again")
}

func TestARebuildFindsWhatTheStoreLost(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	kept := proto.NewObject(&proto.Blob{Data: []byte("kept")})
	lost := proto.NewObject(&proto.Blob{Data: []byte("lost")})
	require.NoError(t, f.store.Put(ctx, kept))
	require.NoError(t, f.store.Put(ctx, lost))

	parted := proto.NewObject(&proto.File{Parts: []*proto.FilePart{
		{Offset: 0, Length: 4, Ref: kept.Ref()},
		{Offset: 4, Length: 4, Ref: lost.Ref()},
	}})
	require.NoError(t, f.store.Put(ctx, parted))

	f.commit("world", f.tree(f.file("level.dat", "intact"), &proto.TreeNode{
		Stat: &proto.FileInfo{Name: []byte("r.0.0.mca"), Type: proto.NodeType_NODE_FILE, MtimeNs: f.epoch.UnixNano(), Size: 8, Mode: 0644},
		Ref:  parted.Ref(),
	}), false)

	f.store.mu.Lock()
	delete(f.store.objects, string(lost.Ref().Hash))
	f.store.mu.Unlock()

	y := f.index()
	require.NoError(t, reindexErr(y.ReIndex(ctx)))

	grant, err := y.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.False(t, grant.Rescan)
	require.Equal(t, []string{"r.0.0.mca"}, grant.Damaged)
}

func TestARebuildAsksAboutAPartTheVersionsShareOnce(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	shared := proto.NewObject(&proto.Blob{Data: []byte("shared")})
	lost := proto.NewObject(&proto.Blob{Data: []byte("lost")})
	require.NoError(t, f.store.Put(ctx, shared))
	require.NoError(t, f.store.Put(ctx, lost))

	for i := range 3 {
		tail := proto.NewObject(&proto.Blob{Data: []byte{byte(i)}})
		require.NoError(t, f.store.Put(ctx, tail))

		version := proto.NewObject(&proto.File{Parts: []*proto.FilePart{
			{Offset: 0, Length: 6, Ref: shared.Ref()},
			{Offset: 6, Length: 4, Ref: lost.Ref()},
			{Offset: 10, Length: 1, Ref: tail.Ref()},
		}})
		require.NoError(t, f.store.Put(ctx, version))

		f.clock = f.clock.Add(time.Hour)
		f.commit("world", f.tree(&proto.TreeNode{
			Stat: &proto.FileInfo{Name: []byte("r.0.0.mca"), Type: proto.NodeType_NODE_FILE, MtimeNs: f.clock.UnixNano(), Size: 11, Mode: 0644},
			Ref:  version.Ref(),
		}), false)
	}

	f.forget(lost.Ref())

	f.store.mu.Lock()
	clear(f.store.asked)
	f.store.mu.Unlock()

	y := f.index()
	require.NoError(t, reindexErr(y.ReIndex(ctx)))

	f.store.mu.Lock()
	require.Equal(t, 1, f.store.asked[string(shared.Ref().Hash)])
	require.Equal(t, 1, f.store.asked[string(lost.Ref().Hash)])
	f.store.mu.Unlock()

	grant, err := y.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.Equal(t, []string{"r.0.0.mca"}, grant.Damaged)
}

// setID is the id of the fixture's named set.
func (f *fixture) setID(name string) int64 {
	f.t.Helper()

	id, err := findSet(f.ctx, f.x.client, name)
	require.NoError(f.t, err)

	return id
}

// forget drops an object from the fixture's store, as a repair that could
// not keep it does.
func (f *fixture) forget(ref *proto.Ref) {
	f.store.mu.Lock()
	delete(f.store.objects, string(ref.Hash))
	f.store.mu.Unlock()
}

func TestARebuildIndexesACommitAroundALostDirectory(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	region := f.dir("region", f.file("r.0.0.mca", "chunks"))
	commit := f.commit("world", f.tree(f.file("level.dat", "intact"), region), false)
	f.forget(region.Ref)

	y := f.index()
	require.NoError(t, reindexErr(y.ReIndex(ctx)))

	held, err := y.CommitDetails(ctx, "world", f.clock.Add(time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, held, 1, "the commit is indexed, not skipped")
	require.True(t, held[0].Ref.Equal(commit))
	require.True(t, held[0].Incomplete)

	versions, err := y.FileInfo(ctx, "world", "level.dat", f.clock.Add(time.Hour), 1)
	require.NoError(t, err)
	require.Len(t, versions, 1, "what the store still holds is restorable")

	grant, err := y.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.True(t, grant.Rescan, "nothing can say what the lost directory held")
}

func TestARebuildIndexesACommitWhoseRootIsLost(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	root := f.tree(f.file("level.dat", "intact"))
	f.commit("world", root, false)
	f.forget(root.Ref())

	y := f.index()
	require.NoError(t, reindexErr(y.ReIndex(ctx)))

	held, err := y.CommitDetails(ctx, "world", f.clock.Add(time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, held, 1)
	require.True(t, held[0].Incomplete)

	grant, err := y.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.True(t, grant.Rescan)
}

func TestAWholeRebuildIsComplete(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx

	f.commit("world", f.tree(f.file("level.dat", "intact")), false)

	y := f.index()
	require.NoError(t, reindexErr(y.ReIndex(ctx)))

	held, err := y.CommitDetails(ctx, "world", f.clock.Add(time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, held, 1)
	require.False(t, held[0].Incomplete)
}
