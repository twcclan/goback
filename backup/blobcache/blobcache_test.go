package blobcache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func blob(data string) *proto.Object {
	return proto.NewObject(&proto.Blob{Data: []byte(data)})
}

func TestCacheRoundTrip(t *testing.T) {
	cache, err := Open(t.TempDir(), "s1", 0)
	require.NoError(t, err)

	obj := blob("hello")
	_, ok := cache.Get(obj.Ref())
	require.False(t, ok)

	require.NoError(t, cache.Put(obj.Ref(), obj))
	require.NoError(t, cache.Put(obj.Ref(), obj), "a second put is a no-op")

	got, ok := cache.Get(obj.Ref())
	require.True(t, ok)
	require.Equal(t, "hello", string(got.GetBlob().Data))

	cache.Drop(obj.Ref())
	_, ok = cache.Get(obj.Ref())
	require.False(t, ok)
}

func TestCacheRejectsCorruptEntries(t *testing.T) {
	cache, err := Open(t.TempDir(), "s1", 0)
	require.NoError(t, err)

	obj := blob("hello")
	require.NoError(t, cache.Put(obj.Ref(), obj))

	other := blob("other")
	require.NoError(t, os.WriteFile(cache.path(obj.Ref()), other.Bytes(), 0o644))

	_, ok := cache.Get(obj.Ref())
	require.False(t, ok)

	_, err = os.Stat(cache.path(obj.Ref()))
	require.True(t, os.IsNotExist(err), "the corrupt entry is removed")
}

func TestCacheSweepsOldestFirst(t *testing.T) {
	cache, err := Open(t.TempDir(), "s1", 30)
	require.NoError(t, err)

	old := blob("old-old-old-old-old")
	recent := blob("new-new-new-new-new")
	require.NoError(t, cache.Put(old.Ref(), old))
	require.NoError(t, cache.Put(recent.Ref(), recent))

	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(cache.path(old.Ref()), past, past))

	removed, err := cache.Sweep()
	require.NoError(t, err)
	require.Equal(t, 1, removed)

	_, ok := cache.Get(old.Ref())
	require.False(t, ok)
	_, ok = cache.Get(recent.Ref())
	require.True(t, ok)

	entries, err := filepath.Glob(filepath.Join(cache.Dir(), "*", "*"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}
