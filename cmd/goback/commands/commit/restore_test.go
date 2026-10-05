package commit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

type pickIndex struct {
	backup.Index
	objects map[string]*proto.Object
	newest  *proto.Commit
	asked   time.Time
}

func (x *pickIndex) Get(_ context.Context, ref *proto.Ref) (*proto.Object, error) {
	obj, ok := x.objects[string(ref.Hash)]
	if !ok {
		return nil, backup.ErrNotFound
	}

	return obj, nil
}

func (x *pickIndex) CommitInfo(_ context.Context, _ string, notAfter time.Time, _ int) ([]*proto.Commit, error) {
	x.asked = notAfter
	if x.newest == nil {
		return nil, nil
	}

	return []*proto.Commit{x.newest}, nil
}

func TestPickByRefChecksTheSet(t *testing.T) {
	tree := &proto.Ref{Hash: make([]byte, proto.HashSize)}
	ours := proto.NewObject(&proto.Commit{Timestamp: 1, Tree: tree, BackupSet: "world"})
	theirs := proto.NewObject(&proto.Commit{Timestamp: 2, Tree: tree, BackupSet: "other"})
	newest := &proto.Commit{Timestamp: 3, Tree: tree, BackupSet: "world"}
	index := &pickIndex{
		objects: map[string]*proto.Object{string(ours.Ref().Hash): ours, string(theirs.Ref().Hash): theirs},
		newest:  newest,
	}
	c := &commit{ctx: context.Background(), index: index, set: "world"}

	c.ref = ours.Ref()
	picked, err := c.pick()
	require.NoError(t, err)
	require.Same(t, ours.GetCommit(), picked, "a ref wins over the newest commit")

	c.ref = theirs.Ref()
	_, err = c.pick()
	require.Error(t, err)

	c.ref = proto.NewObject(&proto.Commit{Timestamp: 4, Tree: tree, BackupSet: "world"}).Ref()
	_, err = c.pick()
	require.Error(t, err)

	at := time.Unix(1700000000, 0)
	c.ref, c.when = nil, at
	picked, err = c.pick()
	require.NoError(t, err)
	require.Same(t, newest, picked)
	require.Equal(t, at, index.asked)

	index.newest = nil
	_, err = c.pick()
	require.Error(t, err)
}

func TestRestoreTarget(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	when, err := restoreTarget(now, "", "", "")
	require.NoError(t, err)
	require.Equal(t, now, when)

	when, err = restoreTarget(now, "2h", "", "")
	require.NoError(t, err)
	require.Equal(t, now.Add(-2*time.Hour), when)

	when, err = restoreTarget(now, "", "2026-10-01T08:30:00+02:00", "")
	require.NoError(t, err)
	require.True(t, when.Equal(time.Date(2026, 10, 1, 6, 30, 0, 0, time.UTC)))

	for _, args := range [][3]string{{"2h", "2026-10-01T08:30:00Z", ""}, {"2h", "", "ab"}, {"", "2026-10-01T08:30:00Z", "ab"}} {
		_, err = restoreTarget(now, args[0], args[1], args[2])
		require.Error(t, err, "%q", args)
	}

	_, err = restoreTarget(now, "", "yesterday", "")
	require.Error(t, err)
}

func write(t *testing.T, path string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
}

func TestRemoveUnrestoredKeepsWhatTheRestoreWroteUnderAnotherSpelling(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "world", "level.dat"))
	write(t, filepath.Join(base, "stale", "old.log"))

	// the commit spells the directory and file differently; on a
	// case-insensitive filesystem the restore wrote into the existing ones
	restored := map[string]bool{
		filepath.Join(base, "World"):              true,
		filepath.Join(base, "World", "Level.dat"): true,
	}

	_, err := os.Lstat(filepath.Join(base, "WORLD"))
	insensitive := err == nil

	require.NoError(t, removeUnrestored(base, restored, false))
	require.NoDirExists(t, filepath.Join(base, "stale"))

	if insensitive {
		require.FileExists(t, filepath.Join(base, "world", "level.dat"))
	} else {
		require.NoDirExists(t, filepath.Join(base, "world"))
	}
}

func TestRestoreDirReplacesWhatIsNotADirectory(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.Mkdir(outside, 0o755))

	link := filepath.Join(base, "world")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	require.NoError(t, restoreDir(link, 0o755))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	require.True(t, info.IsDir(), "the link is replaced by a directory")
	require.Zero(t, info.Mode()&os.ModeSymlink)

	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries, "nothing was written through the link")

	file := filepath.Join(base, "plain")
	write(t, file)
	require.NoError(t, restoreDir(file, 0o755))
	info, err = os.Lstat(file)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}
