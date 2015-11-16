package backup

import (
	"github.com/twcclan/goback/proto"
)

type ChunkWalkFn func(*proto.ChunkRef) error

type ChunkStore interface {
	Create(*proto.Chunk) error
	Read(*proto.ChunkRef) (*proto.Chunk, error)
	Delete(*proto.ChunkRef) error
	Walk(proto.ChunkType, ChunkWalkFn) error
}
