package badger

import (
	"context"
	"errors"
	"fmt"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"go.opentelemetry.io/otel"
	"google.golang.org/protobuf/encoding/protowire"
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

// Store keeps objects in badger, keyed by ref. Each value is the object
// header, length-prefixed, followed by the stored bytes; the entry's user
// meta byte carries the object type so Walk can filter without loading.
type Store struct {
	db *badger.DB
}

func encodeEntry(hdr *proto.ObjectHeader, stored []byte) []byte {
	hdrBytes := proto.Bytes(hdr)
	val := protowire.AppendVarint(nil, uint64(len(hdrBytes)))
	val = append(val, hdrBytes...)

	return append(val, stored...)
}

func objectFromEntry(val []byte, ref *proto.Ref) (*proto.Object, error) {
	hdrLen, n := protowire.ConsumeVarint(val)
	if n < 0 || uint64(len(val)-n) < hdrLen {
		return nil, fmt.Errorf("corrupt entry for %x", ref.Hash)
	}

	hdr, err := proto.NewObjectHeaderFromBytes(val[n : n+int(hdrLen)])
	if err != nil {
		return nil, err
	}

	if !hdr.Ref.Equal(ref) {
		return nil, proto.ErrRefMismatch
	}

	return proto.ObjectFromStored(hdr, val[n+int(hdrLen):])
}

var tracer = otel.Tracer("goback.io/storage/badger")

func (s *Store) Put(ctx context.Context, object *proto.Object) error {
	ctx, span := tracer.Start(ctx, "BadgerStore.Put")
	defer span.End()

	hdr, stored, err := proto.HeaderFor(object)
	if err != nil {
		return err
	}

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.SetEntry(badger.NewEntry(objectKey(hdr.Ref.Hash), encodeEntry(hdr, stored)).WithMeta(byte(hdr.Type)))
	})
}

func (s *Store) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	ctx, span := tracer.Start(ctx, "BadgerStore.Get")
	defer span.End()

	var obj *proto.Object

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(objectKey(ref.Hash))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			var err error
			obj, err = objectFromEntry(val, ref)

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
	ctx, span := tracer.Start(ctx, "BadgerStore.Delete")
	defer span.End()

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(objectKey(ref.Hash))
	})
}

func (s *Store) Walk(ctx context.Context, load bool, filterFor proto.ObjectType, receiver backup.ObjectReceiver) error {
	ctx, span := tracer.Start(ctx, "BadgerStore.Walk")
	defer span.End()

	return s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = load
		opts.Prefix = []byte(objectKeyPrefix)

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			if proto.ObjectType(item.UserMeta()) != filterFor {
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
				obj, err := objectFromEntry(val, ref)
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
	ctx, span := tracer.Start(ctx, "BadgerStore.Has")
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
