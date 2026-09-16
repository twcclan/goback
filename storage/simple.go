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

func NewSimpleObjectStore(base string) *SimpleChunkStore {
	return &SimpleChunkStore{
		base: base,
	}
}

var _ backup.ObjectStore = (*SimpleChunkStore)(nil)

type SimpleChunkStore struct {
	base string
	db   *leveldb.DB
}

func (s *SimpleChunkStore) Open() (err error) {
	s.db, err = leveldb.OpenFile(s.base, &opt.Options{
		NoSync: true,
	})

	return err
}

func (s *SimpleChunkStore) Close() error {
	return s.db.Close()
}

func (s *SimpleChunkStore) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	return s.db.Has(ref.Hash, nil)
}

func (s *SimpleChunkStore) Put(ctx context.Context, obj *proto.Object) error {
	payload, err := obj.Canonical()
	if err != nil {
		return err
	}

	return s.db.Put(proto.HashPayload(obj.Type(), payload).Hash, obj.Bytes(), nil)
}

func (s *SimpleChunkStore) Delete(ctx context.Context, ref *proto.Ref) error {
	return s.db.Delete(ref.Hash, nil)
}

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

	payload, err := obj.Canonical()
	if err != nil {
		return nil, err
	}

	if !proto.HashPayload(obj.Type(), payload).Equal(ref) {
		return nil, proto.ErrRefMismatch
	}

	return obj, nil
}

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
