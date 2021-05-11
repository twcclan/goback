package badger

import (
	"context"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/dgraph-io/badger/v2"
	"github.com/dgraph-io/badger/v2/options"
	"go.opencensus.io/trace"
)

func New(path string) (*Store, error) {
	opts := badger.DefaultOptions(path).
		WithTruncate(true).
		WithCompression(options.Snappy)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	return &Store{
		db: db,
	}, nil
}

type Store struct {
	db *badger.DB
}

func (s *Store) Put(ctx context.Context, object *proto.Object) error {
	ctx, span := trace.StartSpan(ctx, "BadgerStore.Put")
	defer span.End()

	bytes := proto.CompressedBytes(object)
	meta := byte(object.Type())

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.SetEntry(badger.NewEntry(objectKey(object.Ref().Sha1), bytes).WithMeta(meta))
	})
}

func (s *Store) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	ctx, span := trace.StartSpan(ctx, "BadgerStore.Get")
	defer span.End()

	var obj *proto.Object

	return obj, s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(objectKey(ref.Sha1))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			var err error
			obj, err = proto.NewObjectFromCompressedBytes(val)

			return err
		})
	})
}

func (s *Store) Delete(ctx context.Context, ref *proto.Ref) error {
	ctx, span := trace.StartSpan(ctx, "BadgerStore.Delete")
	defer span.End()

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(objectKey(ref.Sha1))
	})
}

func (s *Store) Walk(_ context.Context, _ bool, _ proto.ObjectType, _ backup.ObjectReceiver) error {
	return backup.ErrNotImplemented
}

func (s *Store) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	ctx, span := trace.StartSpan(ctx, "BadgerStore.Has")
	defer span.End()

	var exists bool

	return exists, s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(objectKey(ref.Sha1))
		if err == nil {
			exists = !item.IsDeletedOrExpired()
		}

		return err
	})
}

const objectKeyPrefix = "objects|"

func objectKey(key []byte) []byte {
	return append([]byte(objectKeyPrefix), key...)
}

var _ backup.ObjectStore = (*Store)(nil)
