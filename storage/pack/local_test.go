package pack

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalStorageNestedNames(t *testing.T) {
	base := t.TempDir()
	storage := newLocal(base)

	file, err := storage.Create("s1/t1/session/archive" + ArchiveSuffix)
	require.NoError(t, err)
	_, err = file.Write([]byte("data"))
	require.NoError(t, err)
	require.NoError(t, file.Close())

	flat, err := storage.Create("root" + ArchiveSuffix)
	require.NoError(t, err)
	require.NoError(t, flat.Close())

	names, err := storage.List(ArchiveSuffix)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"s1/t1/session/archive" + ArchiveSuffix, "root" + ArchiveSuffix}, names)

	file, err = storage.Open("s1/t1/session/archive" + ArchiveSuffix)
	require.NoError(t, err)
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, "data", string(data))
	require.NoError(t, file.Close())

	require.NoError(t, storage.Delete("s1/t1/session/archive"+ArchiveSuffix))

	_, err = os.Stat(filepath.Join(base, "s1"))
	require.True(t, os.IsNotExist(err), "empty directories are removed with the last file")

	_, err = os.Stat(base)
	require.NoError(t, err, "the base directory stays")
}
