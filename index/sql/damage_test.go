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
	require.NoError(t, y.ReIndex(ctx))

	grant, err := y.BeginCommit(ctx, "world")
	require.NoError(t, err)
	require.False(t, grant.Rescan)
	require.Equal(t, []string{"r.0.0.mca"}, grant.Damaged)
}

// setID is the id of the fixture's named set.
func (f *fixture) setID(name string) int64 {
	f.t.Helper()

	id, err := findSet(f.ctx, f.x.client, name)
	require.NoError(f.t, err)

	return id
}
