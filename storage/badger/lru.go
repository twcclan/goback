package badger

import (
	"container/list"
	"encoding/binary"
	"sort"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// Option configures a Store.
type Option func(*Store)

// WithCapacity bounds the bytes of objects a Store keeps: past capacity, the
// objects least recently read or written are deleted. Only a cache should
// use it, since an evicted object is gone. 0 leaves the store unbounded.
// The order of last use survives a reopen, to within ten minutes after a
// crash.
func WithCapacity(capacity int64) Option {
	return func(s *Store) {
		if capacity > 0 {
			s.lru = &lru{
				capacity: capacity,
				order:    list.New(),
				entries:  map[string]*list.Element{},
				now:      time.Now,
			}
		}
	}
}

// stampEvery is how stale the persisted last use of an object may get
// before a read writes it again.
const stampEvery = 10 * time.Minute

const usedKeyPrefix = "used|"

func usedKey(hash []byte) []byte {
	return append([]byte(usedKeyPrefix), hash...)
}

func encodeTime(t time.Time) []byte {
	return binary.BigEndian.AppendUint64(nil, uint64(t.UnixNano()))
}

// lru keeps every object's size and last use, most recent first. All
// writes to the database go through it under mtx, so entries and the
// database agree on what is held.
type lru struct {
	mtx      sync.Mutex
	capacity int64
	size     int64
	order    *list.List
	entries  map[string]*list.Element
	now      func() time.Time
}

type lruEntry struct {
	hash    string
	size    int64
	used    time.Time
	stamped time.Time
}

func (l *lru) load(db *badger.DB) error {
	l.mtx.Lock()
	defer l.mtx.Unlock()

	var loaded []*lruEntry
	byHash := map[string]*lruEntry{}
	var orphans [][]byte

	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		opts.Prefix = []byte(objectKeyPrefix)

		it := txn.NewIterator(opts)
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			e := &lruEntry{
				hash: string(item.Key()[len(objectKeyPrefix):]),
				size: int64(len(item.Key())) + item.ValueSize(),
			}
			loaded = append(loaded, e)
			byHash[e.hash] = e
		}
		it.Close()

		opts.PrefetchValues = true
		opts.Prefix = []byte(usedKeyPrefix)

		it = txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			e, ok := byHash[string(item.Key()[len(usedKeyPrefix):])]
			if !ok {
				orphans = append(orphans, item.KeyCopy(nil))
				continue
			}

			err := item.Value(func(val []byte) error {
				if len(val) == 8 {
					e.used = time.Unix(0, int64(binary.BigEndian.Uint64(val)))
					e.stamped = e.used
				}

				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	sort.SliceStable(loaded, func(i, j int) bool { return loaded[i].used.After(loaded[j].used) })

	for _, e := range loaded {
		l.entries[e.hash] = l.order.PushBack(e)
		l.size += e.size
	}

	batch := db.NewWriteBatch()
	defer batch.Cancel()

	for _, key := range orphans {
		if err := batch.Delete(key); err != nil {
			return err
		}
	}

	if err := l.evict(batch); err != nil {
		return err
	}

	return batch.Flush()
}

func (l *lru) put(db *badger.DB, entry *badger.Entry) error {
	hash := entry.Key[len(objectKeyPrefix):]

	l.mtx.Lock()
	defer l.mtx.Unlock()

	if el, ok := l.entries[string(hash)]; ok {
		l.touch(db, el)
		return nil
	}

	now := l.now()

	err := db.Update(func(txn *badger.Txn) error {
		if err := txn.SetEntry(entry); err != nil {
			return err
		}

		return txn.Set(usedKey(hash), encodeTime(now))
	})
	if err != nil {
		return err
	}

	e := &lruEntry{hash: string(hash), size: int64(len(entry.Key) + len(entry.Value)), used: now, stamped: now}
	l.entries[e.hash] = l.order.PushFront(e)
	l.size += e.size

	if l.size <= l.capacity {
		return nil
	}

	batch := db.NewWriteBatch()
	defer batch.Cancel()

	if err := l.evict(batch); err != nil {
		return err
	}

	return batch.Flush()
}

func (l *lru) use(db *badger.DB, hash []byte) {
	l.mtx.Lock()
	defer l.mtx.Unlock()

	if el, ok := l.entries[string(hash)]; ok {
		l.touch(db, el)
	}
}

// touch makes el the most recently used and persists that only once its
// stamp is stampEvery old; a failed stamp only costs order after a reopen.
func (l *lru) touch(db *badger.DB, el *list.Element) {
	e := el.Value.(*lruEntry)
	e.used = l.now()
	l.order.MoveToFront(el)

	if e.used.Sub(e.stamped) < stampEvery {
		return
	}

	err := db.Update(func(txn *badger.Txn) error {
		return txn.Set(usedKey([]byte(e.hash)), encodeTime(e.used))
	})
	if err == nil {
		e.stamped = e.used
	}
}

func (l *lru) delete(db *badger.DB, hash []byte) error {
	l.mtx.Lock()
	defer l.mtx.Unlock()

	err := db.Update(func(txn *badger.Txn) error {
		if err := txn.Delete(objectKey(hash)); err != nil {
			return err
		}

		return txn.Delete(usedKey(hash))
	})
	if err != nil {
		return err
	}

	if el, ok := l.entries[string(hash)]; ok {
		l.forget(el)
	}

	return nil
}

func (l *lru) forget(el *list.Element) {
	e := el.Value.(*lruEntry)
	l.order.Remove(el)
	delete(l.entries, e.hash)
	l.size -= e.size
}

func (l *lru) evict(batch *badger.WriteBatch) error {
	for l.size > l.capacity && l.order.Len() > 0 {
		el := l.order.Back()
		hash := []byte(el.Value.(*lruEntry).hash)

		if err := batch.Delete(objectKey(hash)); err != nil {
			return err
		}

		if err := batch.Delete(usedKey(hash)); err != nil {
			return err
		}

		l.forget(el)
	}

	return nil
}

// flush persists the last uses not yet stamped.
func (l *lru) flush(db *badger.DB) {
	l.mtx.Lock()
	defer l.mtx.Unlock()

	batch := db.NewWriteBatch()
	defer batch.Cancel()

	for el := l.order.Front(); el != nil; el = el.Next() {
		e := el.Value.(*lruEntry)
		if e.used.After(e.stamped) {
			if batch.Set(usedKey([]byte(e.hash)), encodeTime(e.used)) != nil {
				return
			}
			e.stamped = e.used
		}
	}

	_ = batch.Flush()
}
