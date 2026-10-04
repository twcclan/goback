package pack

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
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
