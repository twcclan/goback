package storage

import (
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/syndtr/goleveldb/leveldb"
)

func NewCache(upstream backup.ChunkStore) *Cache {
	return nil
}

var _ backup.ChunkStore = (*Cache)(nil)

type Cache struct {
	database *leveldb.DB
	upstream backup.ChunkStore
}

func (c *Cache) Create(chunk *proto.Chunk) error {
	return c.upstream.Create(chunk)
}

func (c *Cache) Read(ref *proto.ChunkRef) (*proto.Chunk, error) {
	return c.upstream.Read(ref)
}

func (c *Cache) Delete(ref *proto.ChunkRef) error {
	return c.upstream.Delete(ref)
}

func (c *Cache) Walk(chunkType proto.ChunkType, fn backup.ChunkWalkFn) error {
	return c.upstream.Walk(chunkType, fn)
}
