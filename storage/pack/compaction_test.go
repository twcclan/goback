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
