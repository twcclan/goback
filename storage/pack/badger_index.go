package pack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/twcclan/goback/proto"

	"github.com/dgraph-io/badger/v2"
	"github.com/dgraph-io/badger/v2/options"
)

var (
	badgerIndexEndianness = binary.BigEndian
)

func NewBadgerIndex(path string) (*BadgerIndex, error) {
	opts := badger.DefaultOptions(path).
		WithTruncate(true).
		//WithTableLoadingMode(options.FileIO).
		//WithValueLogLoadingMode(options.FileIO).
		//WithNumMemtables(1).
		//WithKeepL0InMemory(false)
		WithCompression(options.Snappy)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	seq, err := db.GetSequence([]byte("sequence|archives"), 50)
	if err != nil {
		return nil, err
	}

	idx := &BadgerIndex{
		db:              db,
		archiveSequence: seq,
		archiveNames:    map[uint64]string{},
		archiveIds:      map[string]uint64{},
	}

	return idx, idx.loadArchives()
}

type BadgerIndex struct {
	db              *badger.DB
	archiveSequence *badger.Sequence

	archivesMtx  sync.RWMutex
	archiveNames map[uint64]string
	archiveIds   map[string]uint64
}

func (b *BadgerIndex) Close() error {
	return b.db.Close()
}

var _ ArchiveIndex = (*BadgerIndex)(nil)

func (b *BadgerIndex) Locate(ref *proto.Ref, exclude ...string) (IndexLocation, error) {
	var location IndexLocation

	txErr := b.db.View(func(txn *badger.Txn) error {
		prefix := b.recordPrefix(ref.Sha1)
		iterator := txn.NewIterator(badger.IteratorOptions{
			PrefetchValues: true,
			PrefetchSize:   1,
			Prefix:         prefix,
		})

		defer iterator.Close()

	outer:
		for iterator.Seek(nil); iterator.Valid(); iterator.Next() {
			value := &badgerValue{}
			err := iterator.Item().Value(func(val []byte) error {
				return binary.Read(bytes.NewReader(val), badgerIndexEndianness, value)
			})
			if err != nil {
				return err
			}

			key := iterator.Item().Key()
			id := badgerIndexEndianness.Uint64(key[len(key)-8:])

			b.archivesMtx.RLock()
			archiveName := b.archiveNames[id]
			b.archivesMtx.RUnlock()

			for _, excluded := range exclude {
				if excluded == archiveName {
					continue outer
				}
			}

			location = IndexLocation{
				Archive: archiveName,
				Record: IndexRecord{
					Offset: value.Offset,
					Length: value.Length,
					Type:   value.Type,
				},
			}

			copy(location.Record.Sum[:], ref.Sha1)
			return nil
		}

		return ErrRecordNotFound
	})

	return location, txErr
}

func (b *BadgerIndex) loadArchives() error {
	return b.db.View(func(txn *badger.Txn) error {
		prefix := []byte(prefixArchive)

		it := txn.NewIterator(badger.IteratorOptions{
			PrefetchValues: true,
			PrefetchSize:   50,
			Prefix:         prefix,
		})

		defer it.Close()

		for it.Seek(prefix); it.Valid() && it.ValidForPrefix(prefix); it.Next() {
			var id uint64
			_ = it.Item().Value(func(val []byte) error {
				id = badgerIndexEndianness.Uint64(val)

				return nil
			})

			// store archive id => name mapping
			name := string(it.Item().Key()[len(prefixArchive):])
			b.archiveNames[id] = name
			b.archiveIds[name] = id
		}

		return nil
	})
}

func (b *BadgerIndex) Has(archive string) (bool, error) {
	b.archivesMtx.RLock()
	defer b.archivesMtx.RUnlock()

	_, ok := b.archiveIds[archive]

	return ok, nil
}

type badgerValue struct {
	Offset uint32
	Length uint32
	Type   uint32
}

func (b *BadgerIndex) idValue(id uint64) []byte {
	d := make([]byte, 8)

	badgerIndexEndianness.PutUint64(d, id)

	return d
}

func (b *BadgerIndex) Index(archive string, index IndexFile) error {
	// check if the archive already exists
	b.archivesMtx.RLock()
	if _, ok := b.archiveIds[archive]; ok {
		return nil
	}
	b.archivesMtx.RUnlock()

	archiveId, err := b.archiveSequence.Next()
	if err != nil {
		return err
	}

	txn := b.db.NewTransaction(true)
	for _, record := range index {
		buf := new(bytes.Buffer)
		value := &badgerValue{
			Offset: record.Offset,
			Length: record.Length,
			Type:   record.Type,
		}
		err := binary.Write(buf, badgerIndexEndianness, value)
		if err != nil {
			txn.Discard()
			return err
		}

		key := b.recordKey(record.Sum[:], archiveId)

		err = txn.Set(key, buf.Bytes())
		if errors.Is(err, badger.ErrTxnTooBig) {
			err = txn.Commit()

			if err != nil {
				return err
			}

			txn = b.db.NewTransaction(true)

			err = txn.Set(key, buf.Bytes())
		}

		if err != nil {
			txn.Discard()
			return err
		}
	}

	err = txn.Commit()
	if err != nil {
		return err
	}

	return b.db.Update(func(txn *badger.Txn) error {
		err := txn.Set(b.key(prefixArchive, []byte(archive)), b.idValue(archiveId))
		if err != nil {
			return err
		}

		b.archivesMtx.Lock()
		b.archiveNames[archiveId] = archive
		b.archiveIds[archive] = archiveId
		b.archivesMtx.Unlock()

		return nil
	})
}

func (b *BadgerIndex) Delete(archive string, index IndexFile) error {
	// check if the archive actually exists
	b.archivesMtx.RLock()
	archiveId, ok := b.archiveIds[archive]
	if !ok {
		return nil
	}
	b.archivesMtx.RUnlock()

	txn := b.db.NewTransaction(true)
	for _, record := range index {

		key := b.recordKey(record.Sum[:], archiveId)

		err := txn.Delete(key)
		if errors.Is(err, badger.ErrTxnTooBig) {
			err = txn.Commit()

			if err != nil {
				return err
			}

			txn = b.db.NewTransaction(true)

			err = txn.Delete(key)
		}

		if err != nil {
			txn.Discard()
			return err
		}
	}

	err := txn.Commit()
	if err != nil {
		return err
	}

	return b.db.Update(func(txn *badger.Txn) error {
		err := txn.Delete(b.key(prefixArchive, []byte(archive)))
		if err != nil {
			return err
		}

		b.archivesMtx.Lock()
		delete(b.archiveNames, archiveId)
		delete(b.archiveIds, archive)
		b.archivesMtx.Unlock()

		return nil
	})
}

func (b *BadgerIndex) Clear() error {
	return b.db.DropAll()
}

const (
	prefixRecord  = "record|"
	prefixArchive = "archive|"
)

func (b *BadgerIndex) recordKey(key []byte, archiveId uint64) []byte {
	return append(b.recordPrefix(key), b.idValue(archiveId)...)
}

func (b *BadgerIndex) recordPrefix(key []byte) []byte {
	return append([]byte(prefixRecord), key...)
}

func (b *BadgerIndex) key(prefix string, key []byte) []byte {
	return append([]byte(prefix), key...)
}
