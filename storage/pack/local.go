package pack

import (
	"os"
	"path/filepath"
)

func newLocal(base string) *localArchiveStorage {
	return &localArchiveStorage{base}
}

var _ ArchiveStorage = (*localArchiveStorage)(nil)

type localArchiveStorage struct {
	base string
}

func (las *localArchiveStorage) DeleteAll() error {
	err := os.RemoveAll(las.base)
	if err != nil {
		return err
	}

	return os.MkdirAll(las.base, 0644)
}

func (las *localArchiveStorage) Open(name string) (File, error) {
	return os.OpenFile(filepath.Join(las.base, name), os.O_RDONLY, 644)
}

func (las *localArchiveStorage) Create(name string) (File, error) {
	return os.Create(filepath.Join(las.base, name))
}

func (las *localArchiveStorage) Delete(name string) error {
	return os.Remove(filepath.Join(las.base, name))
}

func (las *localArchiveStorage) List(extension string) ([]string, error) {
	archives, err := filepath.Glob(filepath.Join(las.base, "*"+extension))
	if err != nil {
		return nil, err
	}

	for i, archive := range archives {
		archives[i] = filepath.Base(archive)
	}

	return archives, nil
}
