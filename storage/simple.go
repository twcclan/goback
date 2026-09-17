package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// NewSimpleObjectStore returns a SimpleChunkStore at base; Open creates the
// database.
func NewSimpleObjectStore(base string) *SimpleChunkStore {
	return &SimpleChunkStore{
		base: base,
	}
}

var _ backup.ObjectStore = (*SimpleChunkStore)(nil)

// SimpleChunkStore keeps objects in a leveldb database keyed by ref.
type SimpleChunkStore struct {
	base string
	db   *leveldb.DB
}

// Open creates the directory.
func (s *SimpleChunkStore) Open() (err error) {
	s.db, err = leveldb.OpenFile(s.base, &opt.Options{
		NoSync: true,
	})

	return err
}

// Close implements io.Closer.
func (s *SimpleChunkStore) Close() error {
	return s.db.Close()
}

// Has implements backup.ObjectStore.
func (s *SimpleChunkStore) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	return s.db.Has(ref.Hash, nil)
}

// Put implements backup.ObjectStore.
func (s *SimpleChunkStore) Put(ctx context.Context, obj *proto.Object) error {
	err := obj.Validate()
	if err != nil {
		return err
	}

	return s.db.Put(obj.Ref().Hash, obj.Bytes(), nil)
}

// Delete implements backup.ObjectStore.
func (s *SimpleChunkStore) Delete(ctx context.Context, ref *proto.Ref) error {
	return s.db.Delete(ref.Hash, nil)
}

// Get implements backup.ObjectStore.
func (s *SimpleChunkStore) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	data, err := s.db.Get(ref.Hash, nil)

	if err != nil {
		if errors.Is(err, leveldb.ErrNotFound) {
			return nil, backup.ErrNotFound
		}

		return nil, err
	}

	obj, err := proto.NewObjectFromBytes(data)
	if err != nil {
		return nil, err
	}

	err = obj.Validate()
	if err != nil {
		return nil, err
	}

	if !obj.Ref().Equal(ref) {
		return nil, proto.ErrRefMismatch
	}

	return obj, nil
}

// Walk implements backup.ObjectStore.
func (s *SimpleChunkStore) Walk(ctx context.Context, load bool, chunkType proto.ObjectType, fn backup.ObjectReceiver) error {
	matches, err := filepath.Glob(path.Join(s.base, fmt.Sprintf("%d-*", chunkType)))
	if err != nil {
		return err
	}

	for _, match := range matches {
		var obj *proto.Object
		if load {
			data, err := os.ReadFile(match)
			if err != nil {
				return err
			}

			obj, err = proto.NewObjectFromBytes(data)
			if err != nil {
				return err
			}
		}

		err = fn(obj)
		if err != nil {
			return err
		}
	}

	return nil
}
