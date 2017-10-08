package storage

import (
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/syndtr/goleveldb/leveldb"
)

func NewCache(upstream backup.ObjectStore) *Cache {
	return nil
}

var _ backup.ObjectStore = (*Cache)(nil)

type Cache struct {
	database *leveldb.DB
	upstream backup.ObjectStore
}

func (c *Cache) Put(obj *proto.Object) error {
	return c.upstream.Put(obj)
}

func (c *Cache) Get(ref *proto.Ref) (*proto.Object, error) {
	return c.upstream.Get(ref)
}

func (c *Cache) Delete(ref *proto.Ref) error {
	return c.upstream.Delete(ref)
}

func (c *Cache) Walk(load bool, chunkType proto.ObjectType, fn backup.ObjectReceiver) error {
	return c.upstream.Walk(load, chunkType, fn)
}
