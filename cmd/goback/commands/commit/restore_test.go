package commit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
