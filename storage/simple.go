package storage

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

func NewSimpleObjectStore(base string) *SimpleChunkStore {
	return &SimpleChunkStore{
		base: base,
	}
}

var _ backup.ObjectStore = (*SimpleChunkStore)(nil)

type SimpleChunkStore struct {
	base string
}

func (s *SimpleChunkStore) chunkFilename(ref *proto.Ref) string {
	//chunk := fmt.Sprintf("%x/%x/%x", ref.Sum[:2], ref.Sum[2:4], ref.Sum)
	chunk := fmt.Sprintf("%x", ref.Sha1)

	return path.Join(s.base, chunk)
}

func (s *SimpleChunkStore) Put(chunk *proto.Object) error {
	return ioutil.WriteFile(s.chunkFilename(chunk.Ref()), chunk.Bytes(), 0644)
}

func (s *SimpleChunkStore) Delete(ref *proto.Ref) error {
	return os.Remove(s.chunkFilename(ref))
}

func (s *SimpleChunkStore) Get(ref *proto.Ref) (*proto.Object, error) {
	data, err := ioutil.ReadFile(s.chunkFilename(ref))

	if err != nil {
		return nil, err
	}

	return proto.NewObjectFromBytes(data)
}

func (s *SimpleChunkStore) Walk(chunkType proto.ObjectType, fn backup.ChunkWalkFn) error {
	matches, err := filepath.Glob(path.Join(s.base, fmt.Sprintf("%d-*", chunkType)))
	if err != nil {
		return err
	}

	for _, match := range matches {
		var hexSum []byte

		fmt.Sscanf(filepath.Base(match), "%x", &hexSum)

		err = fn(&proto.Ref{Sha1: hexSum})
		if err != nil {
			return err
		}
	}

	return nil
}
