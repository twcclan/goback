package pack

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// indexCounting counts the index files opened from the storage.
type indexCounting struct {
	ArchiveStorage
	indexReads atomic.Int64
}

func (c *indexCounting) Open(name string) (File, error) {
	if strings.HasSuffix(name, IndexExt) {
		c.indexReads.Add(1)
	}

	return c.ArchiveStorage.Open(name)
}

func TestTheIndexCacheServesIndexesWithTheirCreationTime(t *testing.T) {
	bucket := newMemBucket()
	index := NewInMemoryIndex()
	cache := t.TempDir()
	ctx := context.Background()

	open := func(cached bool) (*PackStorage, *indexCounting) {
		storage := &indexCounting{ArchiveStorage: bucket.view()}

		options := []PackOption{WithArchiveStorage(storage), WithArchiveIndex(index), WithMaxSize(64 * 1024)}
		if cached {
			options = append(options, WithIndexCache(cache))
		}

		store, err := NewPackStorage(options...)
		require.NoError(t, err)
		require.NoError(t, store.Open())
		t.Cleanup(func() { _ = store.Close() })

		return store, storage
	}

	writer, _ := open(false)
	live := makeChain(makeTestData(t, 20))
	putAll(t, writer, live)
	gone, _ := retiredChain(t, writer)

	first, _ := open(true)
	_, err := first.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)

	second, reads := open(true)
	_, err = second.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)

	read := reads.indexReads.Load()
	require.NotZero(t, read, "the archives the first collection wrote are read once")

	third, again := open(true)
	names, err := third.archiveNames()
	require.NoError(t, err)

	for _, name := range names {
		a, err := third.archiveByName(name)
		require.NoError(t, err)
		_, err = a.getIndex()
		require.NoError(t, err)
	}
	require.Zero(t, again.indexReads.Load(), "every index is cached by now")

	requireStored(t, second, gone, false)
	requireStored(t, second, live, true)

	uncached, _ := open(false)

	for _, name := range names {
		want, err := uncached.archiveByName(name)
		require.NoError(t, err)
		got, err := second.archiveByName(name)
		require.NoError(t, err)

		wantCreated, err := want.indexCreated()
		require.NoError(t, err)
		gotCreated, err := got.indexCreated()
		require.NoError(t, err)
		require.Equal(t, wantCreated, gotCreated, "archive %s", name)
	}
}
