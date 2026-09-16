package pack

import (
	"errors"
	"sync"

	"github.com/twcclan/goback/proto"
)

func NewInMemoryIndex() *InMemoryIndex {
	return &InMemoryIndex{
		index: make(map[string]map[[proto.HashSize]byte]IndexRecord),
	}
}

var _ ArchiveIndex = (*InMemoryIndex)(nil)

// InMemoryIndex is an ArchiveIndex kept entirely in memory, for tests and
// short-lived tools.
type InMemoryIndex struct {
	mtx   sync.RWMutex
	index map[string]map[[proto.HashSize]byte]IndexRecord
}

func (i *InMemoryIndex) LocateObject(ref *proto.Ref, exclude ...string) (IndexLocation, error) {
	var sum [proto.HashSize]byte
	copy(sum[:], ref.Hash)

	i.mtx.RLock()
	defer i.mtx.RUnlock()

outer:
	for archive, records := range i.index {
		for _, excluded := range exclude {
			if excluded == archive {
				continue outer
			}
		}

		if record, ok := records[sum]; ok {
			return IndexLocation{
				Archive: archive,
				Record:  record,
			}, nil
		}
	}

	return IndexLocation{}, ErrRecordNotFound
}

func (i *InMemoryIndex) HasArchive(archive string) (bool, error) {
	i.mtx.RLock()
	defer i.mtx.RUnlock()

	_, ok := i.index[archive]

	return ok, nil
}

func (i *InMemoryIndex) IndexArchive(archive string, index IndexFile) error {
	a := make(map[[proto.HashSize]byte]IndexRecord)

	for _, record := range index {
		a[record.Sum] = record
	}

	i.mtx.Lock()
	i.index[archive] = a
	i.mtx.Unlock()

	return nil
}

func (i *InMemoryIndex) DeleteArchive(archive string, index IndexFile) error {
	i.mtx.Lock()
	delete(i.index, archive)
	i.mtx.Unlock()

	return nil
}

func (i *InMemoryIndex) Close() error {
	i.mtx.Lock()
	i.index = make(map[string]map[[proto.HashSize]byte]IndexRecord)
	i.mtx.Unlock()

	return nil
}

func (i *InMemoryIndex) CountObjects() (uint64, uint64, error) {
	return 0, 0, errors.New("not implemented")
}
