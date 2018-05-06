package pack

import (
	"os"
	"path/filepath"
	"strings"
)

func NewLocalArchiveStorage(base string) *LocalArchiveStorage {
	return &LocalArchiveStorage{base}
}

var _ ArchiveStorage = (*LocalArchiveStorage)(nil)

type LocalArchiveStorage struct {
	base string
}

func (las *LocalArchiveStorage) MaxSize() uint64 {
	return 1024 * 1024 * 1024
}

func (las *LocalArchiveStorage) MaxParallel() uint64 {
	return 1
}

func (las *LocalArchiveStorage) NeedCompaction() {}

func (las *LocalArchiveStorage) Open(name string) (File, error) {
	return os.OpenFile(filepath.Join(las.base, name), os.O_RDONLY, 644)
}

func (las *LocalArchiveStorage) Create(name string) (File, error) {
	return os.Create(filepath.Join(las.base, name))
}

func (las *LocalArchiveStorage) Delete(name string) error {
	return os.Remove(filepath.Join(las.base, name))
}

func (las *LocalArchiveStorage) List() ([]string, error) {
	archives, err := filepath.Glob(filepath.Join(las.base, ArchivePattern))
	if err != nil {
		return nil, err
	}

	for i, archive := range archives {
		archives[i] = strings.TrimSuffix(filepath.Base(archive), ArchiveSuffix)
	}

	return archives, nil
}
