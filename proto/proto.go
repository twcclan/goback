package proto

//go:generate protoc --go_out=. *.proto

import (
	"crypto/sha1"
	"os"

	"github.com/golang/protobuf/proto"
)

func GetMetaChunk(msg proto.Message, chunkType ChunkType) *Chunk {
	data, err := proto.Marshal(msg)
	if err != nil {
		panic(err)
	}

	sum := sha1.Sum(data)

	chunk := &Chunk{
		Ref: &ChunkRef{
			Sum:  sum[:],
			Type: chunkType,
		},
		Data: data,
	}

	return chunk
}

func ReadMetaChunk(chunk *Chunk, msg proto.Message) {
	err := proto.Unmarshal(chunk.Data, msg)
	if err != nil {
		panic(err)
	}
}

func GetTypedRef(ref *ChunkRef, chunkType ChunkType) *ChunkRef {
	return &ChunkRef{
		Sum:  ref.Sum,
		Type: chunkType,
	}
}

func GetUntypedRef(ref *ChunkRef) *ChunkRef {
	return &ChunkRef{
		Sum: ref.Sum,
	}
}

func GetFileInfo(info os.FileInfo) *FileInfo {
	return &FileInfo{
		Name:      info.Name(),
		Mode:      uint32(info.Mode()),
		Timestamp: info.ModTime().UTC().Unix(),
		Size:      info.Size(),
	}
}
