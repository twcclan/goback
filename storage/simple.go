package storage

import (
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"

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

func (s *SimpleChunkStore) Put(obj *proto.Object) error {
	err := s.db.Put(obj.Ref().Sha1, obj.Bytes(), nil)
	if err != nil {
		return err
	}

	return err
}

func (s *SimpleChunkStore) Delete(ref *proto.Ref) error {
	return s.db.Delete(ref.Sha1, nil)
}

func (s *SimpleChunkStore) Get(ref *proto.Ref) (*proto.Object, error) {
	data, err := s.db.Get(ref.Sha1, nil)

	if err != nil {
		return nil, err
	}

	return proto.NewObjectFromBytes(data)
}

func (s *SimpleChunkStore) Walk(chunkType proto.ObjectType, fn backup.ObjectReceiver) error {
	matches, err := filepath.Glob(path.Join(s.base, fmt.Sprintf("%d-*", chunkType)))
	if err != nil {
		return err
	}

	for _, match := range matches {

		data, err := ioutil.ReadFile(match)
		if err != nil {
			return err
		}

		obj, err := proto.NewObjectFromBytes(data)
		if err != nil {
			return err
		}

		err = fn(obj)
		if err != nil {
			return err
		}
	}

	return nil
}
