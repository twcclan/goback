package pack

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gobackio/goback/proto"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
)

// askingIndex counts the refs LocateCopies is asked for.
type askingIndex struct {
	*InMemoryIndex
	asked atomic.Int64
}

func (a *askingIndex) LocateCopies(refs []*proto.Ref, scope Scope) (map[string][]IndexLocation, error) {
	a.asked.Add(int64(len(refs)))

	return a.InMemoryIndex.LocateCopies(refs, scope)
}

func TestARewriteLooksEachObjectUpOncePerChunk(t *testing.T) {
	objects := makeTestData(t, numObjects)

	for _, chunk := range []int{0, 1} {
		index := &askingIndex{InMemoryIndex: NewInMemoryIndex()}
		store := newTestStore(t, t.TempDir(), WithArchiveIndex(index), WithMaxSize(64<<20),
			WithCompaction(CompactionConfig{Chunk: chunk}))

		for range 3 {
			for _, object := range objects {
				require.NoError(t, store.Put(context.Background(), object))
			}

			require.NoError(t, store.Flush())
		}

		index.asked.Store(0)
		require.NoError(t, store.doCompaction())

		if chunk == 0 {
			require.EqualValues(t, numObjects, index.asked.Load(), "one chunk asks for each object once")
		}

		for i, object := range objects {
			copies, err := index.LocateCopies([]*proto.Ref{object.Ref()}, Scope{})
			require.NoError(t, err)
			require.Len(t, copies[string(object.Ref().Hash)], 1, "chunk %d, object %d", chunk, i)

			got, err := store.Get(context.Background(), object.Ref())
			require.NoError(t, err)
			require.Equal(t, object.Bytes(), got.Bytes())
		}

		require.NoError(t, store.Close())
	}
}

func TestCompactionMergesOnlySmallArchivesAndNeverItsOwnOutput(t *testing.T) {
	const small = 64 << 10

	store := newTestStore(t, t.TempDir(), WithMaxSize(4<<20),
		WithCompaction(CompactionConfig{Small: small, Batch: 1 << 20, MinimumCandidates: 1000, Workers: 16}))
	defer store.Close()

	objects := makeTestData(t, 300)

	for i := 0; i < 200; i += 5 {
		for _, object := range objects[i : i+5] {
			require.NoError(t, store.Put(context.Background(), object))
		}
		require.NoError(t, store.Flush())
	}

	for _, object := range objects[200:] {
		require.NoError(t, store.Put(context.Background(), object))
	}
	require.NoError(t, store.Flush())

	sizes := func() map[string]uint64 {
		store.mtx.RLock()
		defer store.mtx.RUnlock()

		out := make(map[string]uint64)
		for _, a := range store.archives {
			out[a.name] = a.size
		}

		return out
	}

	var large string
	for name, size := range sizes() {
		if size >= small {
			large = name
		}
	}
	require.NotEmpty(t, large)

	require.NoError(t, store.doCompaction())

	merged := sizes()
	require.Contains(t, merged, large, "an archive that is not small stays")

	for name, size := range merged {
		require.GreaterOrEqual(t, size, uint64(small), "%s came out small", name)
	}

	require.NoError(t, store.doCompaction())
	require.Equal(t, merged, sizes(), "what compaction wrote is not merged again")

	for _, object := range objects {
		got, err := store.Get(context.Background(), object.Ref())
		require.NoError(t, err)
		require.Equal(t, object.Bytes(), got.Bytes())
	}
}

func TestARewriteTrustsOnlyAReachableCopyOutsideItsGroup(t *testing.T) {
	rw := &rewrite{
		inGroup: map[string]bool{"candidate": true},
		group: &compactionGroup{marked: func(loc *IndexLocation) bool {
			return loc.Archive != "unreachable"
		}},
	}

	at := func(archives ...string) []IndexLocation {
		var copies []IndexLocation
		for _, archive := range archives {
			copies = append(copies, IndexLocation{Archive: archive})
		}

		return copies
	}

	require.False(t, rw.elsewhere(nil))
	require.False(t, rw.elsewhere(at("candidate")), "a copy the rewrite drops is no copy")
	require.False(t, rw.elsewhere(at("candidate", "unreachable")), "an unreachable copy may be swept next")
	require.True(t, rw.elsewhere(at("unreachable", "reachable")))
}

// refusingMarker refuses to store the retirement marker of one archive.
type refusingMarker struct {
	*localArchiveStorage
	refuse string
}

func (r *refusingMarker) CreateNew(name string, data []byte) error {
	if name == r.refuse+RetiredExt {
		return errors.New("refused")
	}

	return r.localArchiveStorage.CreateNew(name, data)
}

// deletingIndex counts the calls to DeleteArchives.
type deletingIndex struct {
	*InMemoryIndex
	calls atomic.Int64
}

func (d *deletingIndex) DeleteArchives(names []string) error {
	d.calls.Add(1)

	return d.InMemoryIndex.DeleteArchives(names)
}

// batchObserved collects the batches an ArchiveBatchObserver hears.
type batchObserved struct {
	observed
	batches [][]string
}

func (b *batchObserved) ArchivesDeleted(names []string) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	b.batches = append(b.batches, names)
}

func TestARewriteRetiresItsInputsTogetherLeavingOutOneWhoseMarkerFailed(t *testing.T) {
	storage := &refusingMarker{localArchiveStorage: newLocal(t.TempDir())}
	index := &deletingIndex{InMemoryIndex: NewInMemoryIndex()}
	seen := &batchObserved{observed: *newObserved()}

	store, err := NewPackStorage(WithArchiveStorage(storage), WithArchiveIndex(index), WithMaxSize(64<<20),
		WithArchiveObserver(seen), WithCompaction(CompactionConfig{MinimumCandidates: 1}))
	require.NoError(t, err)
	require.NoError(t, store.Open())
	defer store.Close()

	objects := makeTestData(t, 40)
	for i := 0; i < len(objects); i += 10 {
		for _, object := range objects[i : i+10] {
			require.NoError(t, store.Put(context.Background(), object))
		}

		require.NoError(t, store.Flush())
	}

	var inputs []string
	for _, a := range store.archives {
		inputs = append(inputs, a.name)
	}
	require.Len(t, inputs, 4)

	storage.refuse = inputs[0]
	require.NoError(t, store.doCompaction())

	_, kept, err := index.LookupArchive(inputs[0])
	require.NoError(t, err)
	require.True(t, kept, "the archive whose marker failed keeps its rows")

	for _, name := range inputs[1:] {
		_, found, err := index.LookupArchive(name)
		require.NoError(t, err)
		require.False(t, found, "%s is forgotten", name)

		marker, err := storage.Open(name + RetiredExt)
		require.NoError(t, err, "%s is quarantined", name)
		require.NoError(t, marker.Close())

		_, err = storage.Open(name + CommittedExt)
		require.True(t, notExist(err), "%s lost its committed marker", name)
	}

	require.EqualValues(t, 1, index.calls.Load(), "the index forgets the chunk's inputs at once")
	require.Empty(t, seen.deleted, "a batch observer hears no single deletions")
	require.Len(t, seen.batches, 1)
	require.ElementsMatch(t, inputs[1:], seen.batches[0])

	for _, object := range objects {
		got, err := store.Get(context.Background(), object.Ref())
		require.NoError(t, err)
		require.Equal(t, object.Bytes(), got.Bytes())
	}
}

func TestForgettingDeletedArchivesKeepsWhatAnotherArchiveHolds(t *testing.T) {
	store, cache := newCachedStore(t)
	defer store.Close()

	ctx := context.Background()
	gone, kept := treeOf(makeGCFiles(makeTestData(t, 1))), treeOf(makeGCFiles(makeTestData(t, 2)))
	record := func(obj *proto.Object) IndexRecord {
		rec := IndexRecord{Type: uint32(obj.Type())}
		copy(rec.Sum[:], obj.Ref().Hash)

		return rec
	}

	require.NoError(t, store.index.IndexArchive(ArchiveInfo{Name: "a"}, IndexFile{record(gone), record(kept)}))
	require.NoError(t, store.index.IndexArchive(ArchiveInfo{Name: "b"}, IndexFile{record(kept)}))
	require.NoError(t, cache.Put(ctx, gone))
	require.NoError(t, cache.Put(ctx, kept))

	require.NoError(t, store.index.DeleteArchives([]string{"a"}))
	store.forgetCached(ctx, []IndexFile{{record(gone), record(kept)}})

	requireCached(t, cache, []*proto.Object{gone}, false)
	requireCached(t, cache, []*proto.Object{kept}, true)
}

// failingOpen fails to open the archive file of one archive.
type failingOpen struct {
	*localArchiveStorage
	fail  string
	opens atomic.Int64
}

func (f *failingOpen) Open(name string) (File, error) {
	if name == f.fail+ArchiveSuffix {
		return nil, errors.New("unreadable")
	}

	if strings.HasSuffix(name, ArchiveSuffix) {
		f.opens.Add(1)
	}

	return f.localArchiveStorage.Open(name)
}

func TestPrefetchHandsTheInputsOverInOrderWithinItsBudget(t *testing.T) {
	storage := &failingOpen{localArchiveStorage: newLocal(t.TempDir())}
	store, err := NewPackStorage(WithArchiveStorage(storage), WithArchiveIndex(NewInMemoryIndex()), WithMaxSize(64<<20))
	require.NoError(t, err)
	require.NoError(t, store.Open())
	defer store.Close()

	objects := makeTestData(t, 50)
	for i := 0; i < len(objects); i += 10 {
		for _, object := range objects[i : i+10] {
			require.NoError(t, store.Put(context.Background(), object))
		}

		require.NoError(t, store.Flush())
	}

	inputs := append([]*archive(nil), store.archives...)
	require.Len(t, inputs, 5)

	var largest uint64
	want := make([][]byte, len(inputs))
	for i, a := range inputs {
		largest = max(largest, a.size)
		want[i], err = a.readAll()
		require.NoError(t, err)
	}

	storage.fail = inputs[2].name
	storage.opens.Store(0)

	// room for one input at a time: the next is read only once the one
	// before is released
	reads := prefetch(context.Background(), inputs, semaphore.NewWeighted(int64(largest)))

	for i, read := range reads {
		input := <-read

		if i == 2 {
			require.Error(t, input.err, "the input that cannot be read")
		} else {
			require.NoError(t, input.err)
			require.Equal(t, want[i], input.data, "input %d", i)
		}

		opened := storage.opens.Load()
		require.LessOrEqual(t, opened, int64(i+1), "nothing is read beyond the budget")

		input.release()
	}
}
