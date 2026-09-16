package badger

import (
	"context"
	"errors"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"go.opencensus.io/trace"
)

func New(path string) (*Store, error) {
	opts := badger.DefaultOptions(path).
		WithCompression(options.Snappy)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	return &Store{
		db: db,
	}, nil
}

// Store keeps canonical payloads in badger, keyed by ref. The entry's user
// meta byte carries the object type in its low nibble and the codec in its
// high nibble.
type Store struct {
	db *badger.DB
}

func entryMeta(t proto.ObjectType, c proto.Compression) byte {
	return byte(t) | byte(c)<<4
}

func splitMeta(meta byte) (proto.ObjectType, proto.Compression) {
	return proto.ObjectType(meta & 0x0f), proto.Compression(meta >> 4)
}

func (s *Store) Put(ctx context.Context, object *proto.Object) error {
	ctx, span := trace.StartSpan(ctx, "BadgerStore.Put")
	defer span.End()

	payload, err := object.Canonical()
	if err != nil {
		return err
	}

	ref := proto.HashPayload(object.Type(), payload)
	stored, compression := proto.Encode(payload)

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.SetEntry(badger.NewEntry(objectKey(ref.Hash), stored).WithMeta(entryMeta(object.Type(), compression)))
	})
}

func (s *Store) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	ctx, span := trace.StartSpan(ctx, "BadgerStore.Get")
	defer span.End()

	var obj *proto.Object

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(objectKey(ref.Hash))
		if err != nil {
			return err
		}

		typ, compression := splitMeta(item.UserMeta())

		return item.Value(func(val []byte) error {
			var err error
			obj, err = proto.NewVerifiedObject(val, compression, typ, ref)

			return err
		})
	})

	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, backup.ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	return obj, nil
}

func (s *Store) Delete(ctx context.Context, ref *proto.Ref) error {
	ctx, span := trace.StartSpan(ctx, "BadgerStore.Delete")
	defer span.End()

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(objectKey(ref.Hash))
	})
}

func (s *Store) Walk(ctx context.Context, load bool, filterFor proto.ObjectType, receiver backup.ObjectReceiver) error {
	ctx, span := trace.StartSpan(ctx, "BadgerStore.Walk")
	defer span.End()

	return s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = load
		opts.Prefix = []byte(objectKeyPrefix)

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			typ, compression := splitMeta(item.UserMeta())

			if typ != filterFor {
				continue
			}

			if !load {
				err := receiver(nil)
				if err != nil {
					return err
				}

				continue
			}

			ref := &proto.Ref{Hash: item.Key()[len(objectKeyPrefix):]}

			err := item.Value(func(val []byte) error {
				obj, err := proto.NewVerifiedObject(val, compression, typ, ref)
				if err != nil {
					return err
				}

				return receiver(obj)
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
}

func (s *Store) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	ctx, span := trace.StartSpan(ctx, "BadgerStore.Has")
	defer span.End()

	var exists bool

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(objectKey(ref.Hash))
		if err == nil {
			exists = !item.IsDeletedOrExpired()
		}

		return err
	})

	if errors.Is(err, badger.ErrKeyNotFound) {
		return false, nil
	}

	return exists, err
}

func (s *Store) Close() error {
	return s.db.Close()
}

const objectKeyPrefix = "objects|"

func objectKey(key []byte) []byte {
	return append([]byte(objectKeyPrefix), key...)
}
