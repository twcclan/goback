package badger

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/pack"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
)

var (
	badgerIndexEndianness = binary.BigEndian
)

// indexFormatVersion is bumped whenever record keys change shape; an index
// written by another version refuses to open until Reset is called.
const indexFormatVersion = 3

// ErrIndexVersion is returned by NewBadgerIndex for an index in another
// format version.
var ErrIndexVersion = errors.New("badger index was written by another format version; reset it and re-index the archives")

func NewBadgerIndex(path string) (*BadgerIndex, error) {
	opts := badger.DefaultOptions(path).
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
		archiveInfos:    map[uint64]pack.ArchiveInfo{},
	}

	err = idx.checkVersion()
	if err != nil {
		db.Close()
		return nil, err
	}

	return idx, idx.loadArchives()
}

func (b *BadgerIndex) checkVersion() error {
	return b.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(keyVersion))
		if errors.Is(err, badger.ErrKeyNotFound) {
			it := txn.NewIterator(badger.IteratorOptions{Prefix: []byte(prefixArchive)})
			it.Rewind()
			populated := it.Valid()
			it.Close()

			if populated {
				return ErrIndexVersion
			}

			return txn.Set([]byte(keyVersion), []byte{indexFormatVersion})
		}

		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			if len(val) != 1 || val[0] != indexFormatVersion {
				return ErrIndexVersion
			}

			return nil
		})
	})
}

// Reset drops every record so the archives can be re-indexed.
func (b *BadgerIndex) Reset() error {
	err := b.db.DropAll()
	if err != nil {
		return err
	}

	b.archivesMtx.Lock()
	b.archiveNames = map[uint64]string{}
	b.archiveIds = map[string]uint64{}
	b.archiveInfos = map[uint64]pack.ArchiveInfo{}
	b.archivesMtx.Unlock()

	return b.checkVersion()
}

// BadgerIndex is an ArchiveIndex in a local badger database.
type BadgerIndex struct {
	db              *badger.DB
	archiveSequence *badger.Sequence

	archivesMtx  sync.RWMutex
	archiveNames map[uint64]string
	archiveIds   map[string]uint64
	archiveInfos map[uint64]pack.ArchiveInfo
}

func (b *BadgerIndex) CountObjects() (uint64, uint64, error) {
	var total uint64
	var unique uint64

	prefixLength := proto.HashSize + len(prefixRecord)

	var last []byte

	return total, unique, b.db.View(func(txn *badger.Txn) error {
		opts := badger.IteratorOptions{
			Prefix: b.recordPrefix(nil),
		}
		it := txn.NewIterator(opts)

		defer it.Close()

		for it.Rewind(); it.ValidForPrefix(opts.Prefix); it.Next() {
			total += 1
			prefix := it.Item().Key()[:prefixLength]
			if !bytes.Equal(last, prefix) {
				unique += 1
				last = prefix
			}
		}

		return nil
	})
}

func (b *BadgerIndex) Close() error {
	return b.db.Close()
}

var _ pack.ArchiveIndex = (*BadgerIndex)(nil)

func (b *BadgerIndex) LocateObject(ref *proto.Ref, scope pack.Scope, exclude ...string) (pack.IndexLocation, error) {
	var location pack.IndexLocation

	txErr := b.db.View(func(txn *badger.Txn) error {
		prefix := b.recordPrefix(ref.Hash)
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
			info := b.archiveInfos[id]
			b.archivesMtx.RUnlock()

			if !scope.Visible(info) {
				continue
			}

			for _, excluded := range exclude {
				if excluded == info.Name {
					continue outer
				}
			}

			location = pack.IndexLocation{
				Archive: info.Name,
				Record: pack.IndexRecord{
					Offset: value.Offset,
					Length: value.Length,
					Type:   value.Type,
				},
			}

			copy(location.Record.Sum[:], ref.Hash)
			return nil
		}

		return pack.ErrRecordNotFound
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
			name := string(it.Item().Key()[len(prefixArchive):])

			var id uint64
			var info pack.ArchiveInfo
			err := it.Item().Value(func(val []byte) error {
				var err error
				id, info, err = decodeArchive(name, val)

				return err
			})
			if err != nil {
				return err
			}

			b.remember(id, info)
		}

		return nil
	})
}

func (b *BadgerIndex) remember(id uint64, info pack.ArchiveInfo) {
	b.archivesMtx.Lock()
	b.archiveNames[id] = info.Name
	b.archiveIds[info.Name] = id
	b.archiveInfos[id] = info
	b.archivesMtx.Unlock()
}

func (b *BadgerIndex) forget(id uint64, name string) {
	b.archivesMtx.Lock()
	delete(b.archiveNames, id)
	delete(b.archiveIds, name)
	delete(b.archiveInfos, id)
	b.archivesMtx.Unlock()
}

func (b *BadgerIndex) archiveID(archive string) (uint64, bool) {
	b.archivesMtx.RLock()
	defer b.archivesMtx.RUnlock()

	id, ok := b.archiveIds[archive]

	return id, ok
}

func (b *BadgerIndex) LookupArchive(archive string) (pack.ArchiveInfo, bool, error) {
	b.archivesMtx.RLock()
	defer b.archivesMtx.RUnlock()

	id, ok := b.archiveIds[archive]
	if !ok {
		return pack.ArchiveInfo{}, false, nil
	}

	return b.archiveInfos[id], true, nil
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

// encodeArchive lays out an archive record: id, state, then the
// length-prefixed session.
func (b *BadgerIndex) encodeArchive(id uint64, info pack.ArchiveInfo) []byte {
	d := b.idValue(id)
	d = append(d, byte(info.State))
	d = binary.BigEndian.AppendUint16(d, uint16(len(info.Session)))
	d = append(d, info.Session...)

	return d
}

func decodeArchive(name string, val []byte) (uint64, pack.ArchiveInfo, error) {
	if len(val) < 9 {
		return 0, pack.ArchiveInfo{}, errors.New("short archive record")
	}

	info := pack.ArchiveInfo{Name: name, State: pack.ArchiveState(val[8])}
	id := badgerIndexEndianness.Uint64(val)
	rest := val[9:]

	for _, field := range []*string{&info.Session} {
		if len(rest) < 2 {
			return 0, pack.ArchiveInfo{}, errors.New("short archive record")
		}

		n := int(binary.BigEndian.Uint16(rest))
		rest = rest[2:]

		if len(rest) < n {
			return 0, pack.ArchiveInfo{}, errors.New("short archive record")
		}

		*field = string(rest[:n])
		rest = rest[n:]
	}

	return id, info, nil
}

func (b *BadgerIndex) IndexArchive(archive pack.ArchiveInfo, index pack.IndexFile) error {
	if _, ok := b.archiveID(archive.Name); ok {
		return nil
	}

	if archive.State == pack.ArchivePending {
		_, err := b.GetSession(archive.Session)
		if err != nil {
			return err
		}
	}

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
		err := txn.Set(b.key(prefixArchive, []byte(archive.Name)), b.encodeArchive(archiveId, archive))
		if err != nil {
			return err
		}

		b.remember(archiveId, archive)

		return nil
	})
}

func (b *BadgerIndex) DeleteArchive(archive string, index pack.IndexFile) error {
	archiveId, ok := b.archiveID(archive)
	if !ok {
		return nil
	}

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

		b.forget(archiveId, archive)

		return nil
	})
}

// deleteArchiveRecords drops every record of an archive by scanning; used
// when the archive's index file is not at hand.
func (b *BadgerIndex) deleteArchiveRecords(archiveId uint64) error {
	suffix := b.idValue(archiveId)

	var keys [][]byte
	err := b.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.IteratorOptions{Prefix: b.recordPrefix(nil)})
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			key := it.Item().KeyCopy(nil)
			if bytes.HasSuffix(key, suffix) {
				keys = append(keys, key)
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	wb := b.db.NewWriteBatch()
	defer wb.Cancel()

	for _, key := range keys {
		if err := wb.Delete(key); err != nil {
			return err
		}
	}

	return wb.Flush()
}

func (b *BadgerIndex) sessionKey(id string) []byte {
	return b.key(prefixSession, []byte(id))
}

func (b *BadgerIndex) publicRefKey(ref []byte) []byte {
	return b.key(prefixPublicRef, ref)
}

// RecordPublicRefs implements pack.PublicRefIndex.
func (b *BadgerIndex) RecordPublicRefs(refs [][]byte) error {
	batch := b.db.NewWriteBatch()
	defer batch.Cancel()

	for _, ref := range refs {
		if err := batch.Set(b.publicRefKey(ref), nil); err != nil {
			return err
		}
	}

	return batch.Flush()
}

// HasPublicRef implements pack.PublicRefIndex.
func (b *BadgerIndex) HasPublicRef(ref []byte) (bool, error) {
	err := b.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(b.publicRefKey(ref))
		return err
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return false, nil
	}

	return err == nil, err
}

// ForgetRefs implements pack.PublicRefIndex.
func (b *BadgerIndex) ForgetRefs(refs [][]byte) error {
	batch := b.db.NewWriteBatch()
	defer batch.Cancel()

	for _, ref := range refs {
		if err := batch.Delete(b.publicRefKey(ref)); err != nil {
			return err
		}
	}

	return batch.Flush()
}

func (b *BadgerIndex) BeginSession(s *backup.Session) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	return b.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get(b.sessionKey(s.ID))
		if err == nil {
			return errors.New("session already exists")
		}

		if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

		return txn.Set(b.sessionKey(s.ID), data)
	})
}

func (b *BadgerIndex) TouchSession(id string, at time.Time) error {
	return b.db.Update(func(txn *badger.Txn) error {
		s, err := b.getSession(txn, id)
		if err != nil {
			return err
		}

		s.LastSeen = at

		data, err := json.Marshal(s)
		if err != nil {
			return err
		}

		return txn.Set(b.sessionKey(id), data)
	})
}

func (b *BadgerIndex) getSession(txn *badger.Txn, id string) (*backup.Session, error) {
	item, err := txn.Get(b.sessionKey(id))
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, backup.ErrNoSession
	}

	if err != nil {
		return nil, err
	}

	s := &backup.Session{}

	return s, item.Value(func(val []byte) error {
		return json.Unmarshal(val, s)
	})
}

func (b *BadgerIndex) GetSession(id string) (*backup.Session, error) {
	var s *backup.Session

	err := b.db.View(func(txn *badger.Txn) error {
		var err error
		s, err = b.getSession(txn, id)

		return err
	})

	return s, err
}

func (b *BadgerIndex) ListSessions() ([]*backup.Session, error) {
	var sessions []*backup.Session

	err := b.db.View(func(txn *badger.Txn) error {
		prefix := []byte(prefixSession)
		it := txn.NewIterator(badger.IteratorOptions{PrefetchValues: true, Prefix: prefix})
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			s := &backup.Session{}
			err := it.Item().Value(func(val []byte) error {
				return json.Unmarshal(val, s)
			})
			if err != nil {
				return err
			}

			sessions = append(sessions, s)
		}

		return nil
	})

	return sessions, err
}

func (b *BadgerIndex) pendingArchives(session string) []pack.ArchiveInfo {
	b.archivesMtx.RLock()
	defer b.archivesMtx.RUnlock()

	var archives []pack.ArchiveInfo
	for _, info := range b.archiveInfos {
		if info.State == pack.ArchivePending && info.Session == session {
			archives = append(archives, info)
		}
	}

	return archives
}

func (b *BadgerIndex) EndSession(id string) ([]string, error) {
	var dropped []string

	for _, info := range b.pendingArchives(id) {
		archiveId, ok := b.archiveID(info.Name)
		if !ok {
			continue
		}

		err := b.deleteArchiveRecords(archiveId)
		if err != nil {
			return dropped, err
		}

		err = b.db.Update(func(txn *badger.Txn) error {
			return txn.Delete(b.key(prefixArchive, []byte(info.Name)))
		})
		if err != nil {
			return dropped, err
		}

		b.forget(archiveId, info.Name)
		dropped = append(dropped, info.Name)
	}

	return dropped, b.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(b.sessionKey(id))
	})
}

func (b *BadgerIndex) CommitSession(id string) error {
	if _, err := b.GetSession(id); err != nil {
		return err
	}

	for _, info := range b.pendingArchives(id) {
		archiveId, ok := b.archiveID(info.Name)
		if !ok {
			continue
		}

		info.State = pack.ArchiveCommitted
		info.Session = ""

		err := b.db.Update(func(txn *badger.Txn) error {
			return txn.Set(b.key(prefixArchive, []byte(info.Name)), b.encodeArchive(archiveId, info))
		})
		if err != nil {
			return err
		}

		b.remember(archiveId, info)
	}

	return nil
}

func (b *BadgerIndex) Clear() error {
	return b.Reset()
}

const (
	prefixRecord    = "record|"
	prefixArchive   = "archive|"
	prefixSession   = "session|"
	prefixPublicRef = "publicref|"
	keyVersion      = "meta|version"
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
