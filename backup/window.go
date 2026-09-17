package backup

import (
	"container/list"
	"sync"

	"github.com/twcclan/goback/proto"
)

// DefaultWindowBytes bounds the chunk bytes an agent keeps for files whose
// assumed parts the store has not confirmed yet.
const DefaultWindowBytes = 64 << 20

// chunkWindow keeps the blobs of skipped chunks until their file is
// confirmed, dropping the oldest when the budget is exceeded.
type chunkWindow struct {
	mtx    sync.Mutex
	budget int
	used   int
	order  *list.List
	items  map[string]*list.Element
}

type windowEntry struct {
	key  string
	blob *proto.Object
	size int
}

func newChunkWindow(budget int) *chunkWindow {
	return &chunkWindow{budget: budget, order: list.New(), items: map[string]*list.Element{}}
}

func (w *chunkWindow) put(key string, blob *proto.Object, size int) {
	if w == nil || size > w.budget {
		return
	}

	w.mtx.Lock()
	defer w.mtx.Unlock()

	if _, ok := w.items[key]; ok {
		return
	}

	for w.used+size > w.budget && w.order.Len() > 0 {
		w.evict(w.order.Front())
	}

	w.items[key] = w.order.PushBack(&windowEntry{key: key, blob: blob, size: size})
	w.used += size
}

func (w *chunkWindow) evict(e *list.Element) {
	entry := w.order.Remove(e).(*windowEntry)
	delete(w.items, entry.key)
	w.used -= entry.size
}

// take returns and forgets the blob under key, or nil.
func (w *chunkWindow) take(key string) *proto.Object {
	if w == nil {
		return nil
	}

	w.mtx.Lock()
	defer w.mtx.Unlock()

	e, ok := w.items[key]
	if !ok {
		return nil
	}

	blob := e.Value.(*windowEntry).blob
	w.evict(e)

	return blob
}

// drop forgets the given keys.
func (w *chunkWindow) drop(keys []string) {
	if w == nil {
		return
	}

	w.mtx.Lock()
	defer w.mtx.Unlock()

	for _, key := range keys {
		if e, ok := w.items[key]; ok {
			w.evict(e)
		}
	}
}
