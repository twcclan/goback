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

func NewSimpleChunkStore(base string) *SimpleChunkStore {
	return &SimpleChunkStore{
		base: base,
	}
}

var _ backup.ChunkStore = (*SimpleChunkStore)(nil)

type SimpleChunkStore struct {
	base string
}

func (s *SimpleChunkStore) chunkFilename(ref *proto.ChunkRef) string {
	//chunk := fmt.Sprintf("%x/%x/%x", ref.Sum[:2], ref.Sum[2:4], ref.Sum)
	chunk := fmt.Sprintf("%d-%x", ref.Type, ref.Sum)

	return path.Join(s.base, chunk)
}

func (s *SimpleChunkStore) Create(chunk *proto.Chunk) error {
	return ioutil.WriteFile(s.chunkFilename(chunk.Ref), chunk.Data, 0644)
}

func (s *SimpleChunkStore) Delete(ref *proto.ChunkRef) error {
	return os.Remove(s.chunkFilename(ref))
}

func (s *SimpleChunkStore) Read(ref *proto.ChunkRef) (*proto.Chunk, error) {
	data, err := ioutil.ReadFile(s.chunkFilename(ref))

	return &proto.Chunk{
		Ref:  ref,
		Data: data,
	}, err
}

func (s *SimpleChunkStore) Walk(chunkType proto.ChunkType, fn backup.ChunkWalkFn) error {
	matches, err := filepath.Glob(path.Join(s.base, fmt.Sprintf("%d-*", chunkType)))
	if err != nil {
		return err
	}

	for _, match := range matches {
		var hexSum []byte

		fmt.Sscanf(filepath.Base(match)[2:], "%x", &hexSum)

		err = fn(&proto.ChunkRef{Sum: hexSum, Type: chunkType})
		if err != nil {
			return err
		}
	}

	return nil
}
