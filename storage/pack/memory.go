package pack

import (
	"errors"

	"github.com/twcclan/goback/proto"
)

func NewInMemoryIndex() *InMemoryIndex {
	return &InMemoryIndex{
		index: make(map[string]map[[20]byte]IndexRecord),
	}
}

var _ ArchiveIndex = (*InMemoryIndex)(nil)

type InMemoryIndex struct {
	index map[string]map[[20]byte]IndexRecord
}

func (i *InMemoryIndex) LocateObject(ref *proto.Ref, exclude ...string) (IndexLocation, error) {
	var sum [20]byte
	copy(sum[:], ref.Sha1)

	for archive, records := range i.index {
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
	_, ok := i.index[archive]

	return ok, nil
}

func (i *InMemoryIndex) IndexArchive(archive string, index IndexFile) error {
	a := make(map[[20]byte]IndexRecord)

	for _, record := range index {
		a[record.Sum] = record
	}

	i.index[archive] = a

	return nil
}

func (i *InMemoryIndex) DeleteArchive(archive string, index IndexFile) error {
	delete(i.index, archive)

	return nil
}

func (i *InMemoryIndex) Close() error {
	i.index = make(map[string]map[[20]byte]IndexRecord)

	return nil
}

func (i *InMemoryIndex) CountObjects() (uint64, uint64, error) {
	return 0, 0, errors.New("not implemented")
}
