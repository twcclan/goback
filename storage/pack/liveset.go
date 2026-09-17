package pack

import (
	"bufio"
	"bytes"
	"container/heap"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type refKey [32]byte

func keyOf(hash []byte) refKey {
	var k refKey
	copy(k[:], hash)

	return k
}

// liveRuns collects refs and spills them as sorted runs on local disk.
// Runs are named after the batch of roots they belong to, so a resumed
// mark can pick up the runs of completed batches.
type liveRuns struct {
	dir    string
	limit  int
	prefix string

	mtx   sync.Mutex
	buf   []refKey
	files []string
	added uint64
}

func newLiveRuns(dir string, limit int) *liveRuns {
	return &liveRuns{dir: dir, limit: limit}
}

func (l *liveRuns) add(refs []refKey) error {
	l.mtx.Lock()
	defer l.mtx.Unlock()

	l.buf = append(l.buf, refs...)
	l.added += uint64(len(refs))

	if len(l.buf) >= l.limit {
		return l.spillLocked()
	}

	return nil
}

func sortKeys(keys []refKey) {
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i][:], keys[j][:]) < 0 })
}

func (l *liveRuns) spillLocked() error {
	sortKeys(l.buf)

	file, err := os.CreateTemp(l.dir, l.prefix+"*.run")
	if err != nil {
		return err
	}

	w := bufio.NewWriterSize(file, 1<<20)
	for _, k := range l.buf {
		if _, err := w.Write(k[:]); err != nil {
			_ = file.Close()
			return err
		}
	}

	if err := w.Flush(); err != nil {
		_ = file.Close()
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	l.files = append(l.files, file.Name())
	l.buf = l.buf[:0]

	return nil
}

// checkpoint spills what is buffered and marks the batch complete.
func (l *liveRuns) checkpoint(batch int) error {
	l.mtx.Lock()
	defer l.mtx.Unlock()

	if len(l.buf) > 0 {
		if err := l.spillLocked(); err != nil {
			return err
		}
	}

	return os.WriteFile(filepath.Join(l.dir, doneName(batch)), nil, 0o644)
}

func batchPrefix(batch int) string { return fmt.Sprintf("batch-%d-", batch) }
func doneName(batch int) string    { return fmt.Sprintf("batch-%d.done", batch) }

func (l *liveRuns) close() {
	for _, name := range l.files {
		_ = os.Remove(name)
	}

	l.files = nil
	l.buf = nil
}

// iterator sorts what is buffered and merges it with the spilled runs into
// one deduplicated ascending stream.
func (l *liveRuns) iterator() (*liveIter, error) {
	l.mtx.Lock()
	defer l.mtx.Unlock()

	sortKeys(l.buf)

	it := &liveIter{}
	it.heads = append(it.heads, &runHead{keys: l.buf})

	for _, name := range l.files {
		file, err := os.Open(filepath.Clean(name))
		if err != nil {
			it.close()
			return nil, err
		}

		it.heads = append(it.heads, &runHead{file: file, reader: bufio.NewReaderSize(file, 1<<20)})
	}

	for _, h := range it.heads {
		if h.advance() {
			it.heap = append(it.heap, h)
		}
	}

	heap.Init(&it.heap)

	return it, nil
}

type runHead struct {
	keys   []refKey
	file   *os.File
	reader *bufio.Reader
	cur    refKey
}

func (h *runHead) advance() bool {
	if h.reader == nil {
		if len(h.keys) == 0 {
			return false
		}

		h.cur, h.keys = h.keys[0], h.keys[1:]

		return true
	}

	_, err := io.ReadFull(h.reader, h.cur[:])

	return err == nil
}

type runHeap []*runHead

func (h runHeap) Len() int            { return len(h) }
func (h runHeap) Less(i, j int) bool  { return bytes.Compare(h[i].cur[:], h[j].cur[:]) < 0 }
func (h runHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *runHeap) Push(x interface{}) { *h = append(*h, x.(*runHead)) }
func (h *runHeap) Pop() interface{} {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]

	return x
}

type liveIter struct {
	heads []*runHead
	heap  runHeap
	last  refKey
	begun bool
}

// next returns the next distinct key in ascending order.
func (it *liveIter) next() (refKey, bool) {
	for it.heap.Len() > 0 {
		top := it.heap[0]
		key := top.cur

		if top.advance() {
			heap.Fix(&it.heap, 0)
		} else {
			heap.Pop(&it.heap)
		}

		if it.begun && key == it.last {
			continue
		}

		it.last, it.begun = key, true

		return key, true
	}

	return refKey{}, false
}

func (it *liveIter) close() {
	for _, h := range it.heads {
		if h.file != nil {
			_ = h.file.Close()
		}
	}
}
