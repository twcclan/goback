package pack

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newLocal(base string) *localArchiveStorage {
	return &localArchiveStorage{base}
}

var _ ArchiveStorage = (*localArchiveStorage)(nil)

// localArchiveStorage keeps archives in a directory; a name with slashes
// lands in a subdirectory.
type localArchiveStorage struct {
	base string
}

func (las *localArchiveStorage) DeleteAll() error {
	err := os.RemoveAll(las.base)
	if err != nil {
		return err
	}

	return os.MkdirAll(las.base, 0755)
}

func (las *localArchiveStorage) path(name string) string {
	return filepath.Join(las.base, filepath.FromSlash(name))
}

func (las *localArchiveStorage) Open(name string) (File, error) {
	return os.OpenFile(las.path(name), os.O_RDONLY, 0644)
}

func (las *localArchiveStorage) Create(name string) (File, error) {
	err := os.MkdirAll(filepath.Dir(las.path(name)), 0755)
	if err != nil {
		return nil, err
	}

	return os.Create(las.path(name))
}

// Delete removes the file and the directories it leaves empty.
func (las *localArchiveStorage) Delete(name string) error {
	err := os.Remove(las.path(name))
	if err != nil {
		return err
	}

	for dir := filepath.Dir(las.path(name)); dir != las.base && strings.HasPrefix(dir, las.base); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}

	return nil
}

func (las *localArchiveStorage) List(extension string) ([]string, error) {
	var names []string

	err := filepath.WalkDir(las.base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !strings.HasSuffix(p, extension) {
			return nil
		}

		rel, err := filepath.Rel(las.base, p)
		if err != nil {
			return err
		}

		names = append(names, filepath.ToSlash(rel))

		return nil
	})

	return names, err
}

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
