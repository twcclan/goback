package pack

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"testing"

	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func atRestKey(t *testing.T, name string) *storekey.Key {
	t.Helper()

	key, err := storekey.Generate(name)
	require.NoError(t, err)

	return key
}

func TestAtRestSealsPayloadsOnly(t *testing.T) {
	base := t.TempDir()
	key := atRestKey(t, "server")
	ctx := context.Background()

	store := newTestStore(t, base, WithAtRestKey(key))
	secret := []byte("player coordinates and chat logs, plaintext from the agent")
	blob := proto.NewObject(&proto.Blob{Data: secret})
	require.NoError(t, store.Put(ctx, blob))
	require.NoError(t, store.Close())

	for _, name := range archiveFiles(t, base) {
		data, err := os.ReadFile(name)
		require.NoError(t, err)
		require.False(t, bytes.Contains(data, secret), "the archive holds the payload in the clear")
		require.True(t, bytes.Contains(data, blob.Ref().Hash), "the header stays readable")
	}

	store = newTestStore(t, base, WithAtRestKey(key))
	t.Cleanup(func() { _ = store.Close() })

	got, err := store.Get(ctx, blob.Ref())
	require.NoError(t, err)
	require.Equal(t, secret, got.GetBlob().Data)

	var walked int
	require.NoError(t, store.Walk(ctx, true, proto.ObjectType_BLOB, func(obj *proto.Object) error {
		require.Equal(t, secret, obj.GetBlob().Data)
		walked++
		return nil
	}))
	require.Equal(t, 1, walked)

	require.NoError(t, store.WalkHeaders(ctx, proto.ObjectType_BLOB, func(hdr *proto.ObjectHeader) error {
		require.Equal(t, key.ID(), hdr.AtRestKeyId)
		return nil
	}))

	report, err := store.Scrub(ctx)
	require.NoError(t, err)
	require.Empty(t, report.Corrupt)
}

func TestAtRestNeedsTheKey(t *testing.T) {
	base := t.TempDir()
	key := atRestKey(t, "server")
	ctx := context.Background()

	store := newTestStore(t, base, WithAtRestKey(key))
	blob := proto.NewObject(&proto.Blob{Data: []byte("sealed")})
	require.NoError(t, store.Put(ctx, blob))
	require.NoError(t, store.Close())

	// the headers are enough to rebuild the index without the key
	store = newTestStore(t, base)
	_, err := store.Get(ctx, blob.Ref())
	require.ErrorIs(t, err, ErrAtRestKeyMissing)
	var headers int
	require.NoError(t, store.WalkHeaders(ctx, proto.ObjectType_BLOB, func(*proto.ObjectHeader) error {
		headers++
		return nil
	}))
	require.Equal(t, 1, headers)
	require.NoError(t, store.Close())

	store = newTestStore(t, base, WithAtRestKey(atRestKey(t, "other")))
	_, err = store.Get(ctx, blob.Ref())
	require.ErrorIs(t, err, ErrAtRestKeyMismatch)
	require.NoError(t, store.Close())
}

func TestAtRestReadsEarlierPlaintextArchives(t *testing.T) {
	base := t.TempDir()
	ctx := context.Background()

	store := newTestStore(t, base)
	before := proto.NewObject(&proto.Blob{Data: []byte("written before the key")})
	require.NoError(t, store.Put(ctx, before))
	require.NoError(t, store.Close())

	store = newTestStore(t, base, WithAtRestKey(atRestKey(t, "server")))
	t.Cleanup(func() { _ = store.Close() })
	after := proto.NewObject(&proto.Blob{Data: []byte("written under the key")})
	require.NoError(t, store.Put(ctx, after))

	for _, obj := range []*proto.Object{before, after} {
		got, err := store.Get(ctx, obj.Ref())
		require.NoError(t, err)
		require.Equal(t, obj.GetBlob().Data, got.GetBlob().Data)
	}

	require.NoError(t, store.WalkHeaders(ctx, proto.ObjectType_BLOB, func(hdr *proto.ObjectHeader) error {
		if hdr.Ref.Equal(before.Ref()) {
			require.Empty(t, hdr.AtRestKeyId)
		} else {
			require.NotEmpty(t, hdr.AtRestKeyId)
		}
		return nil
	}))
}

func TestAtRestCompactionResealsUnderTheKey(t *testing.T) {
	base := t.TempDir()
	key := atRestKey(t, "server")
	ctx := context.Background()

	store := newTestStore(t, base)
	objects := makeTestData(t, 20)
	for _, obj := range objects {
		require.NoError(t, store.Put(ctx, obj))
		require.NoError(t, store.Flush())
	}
	require.NoError(t, store.Close())

	store = newTestStore(t, base, WithAtRestKey(key), WithCompaction(CompactionConfig{MinimumCandidates: 2}))
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.doCompaction())

	require.NoError(t, store.WalkHeaders(ctx, proto.ObjectType_BLOB, func(hdr *proto.ObjectHeader) error {
		require.Equal(t, key.ID(), hdr.AtRestKeyId)
		return nil
	}))

	for _, obj := range objects {
		got, err := store.Get(ctx, obj.Ref())
		require.NoError(t, err)
		require.Equal(t, obj.GetBlob().Data, got.GetBlob().Data)
	}
}

func TestRotationOpensTheRetiredKeyAndResealsOnRewrite(t *testing.T) {
	base := t.TempDir()
	old := atRestKey(t, "old")
	current := atRestKey(t, "current")
	ctx := context.Background()

	store := newTestStore(t, base, WithAtRestKey(old))
	objects := makeTestData(t, 20)
	for _, obj := range objects {
		require.NoError(t, store.Put(ctx, obj))
		require.NoError(t, store.Flush())
	}
	require.NoError(t, store.Close())

	// the retired key comes first: the order of the options must not matter
	store = newTestStore(t, base,
		WithRetiredAtRestKey(old),
		WithAtRestKey(current),
		WithCompaction(CompactionConfig{MinimumCandidates: 2}),
	)
	t.Cleanup(func() { _ = store.Close() })

	for _, obj := range objects {
		got, err := store.Get(ctx, obj.Ref())
		require.NoError(t, err)
		require.Equal(t, obj.GetBlob().Data, got.GetBlob().Data)
	}

	report, err := store.Scrub(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(len(objects)), report.Sealed[hex.EncodeToString(old.ID())])

	require.NoError(t, store.doCompaction())

	report, err = store.Scrub(ctx)
	require.NoError(t, err)
	require.Empty(t, report.Corrupt)
	require.Zero(t, report.Sealed[hex.EncodeToString(old.ID())], "nothing names the retired key once a rewrite is over")
	require.Equal(t, uint64(len(objects)), report.Sealed[hex.EncodeToString(current.ID())])
}

func TestRotationWithoutTheRetiredKeyCannotRead(t *testing.T) {
	base := t.TempDir()
	old := atRestKey(t, "old")
	ctx := context.Background()

	store := newTestStore(t, base, WithAtRestKey(old))
	blob := proto.NewObject(&proto.Blob{Data: []byte("sealed under the key that went")})
	require.NoError(t, store.Put(ctx, blob))
	require.NoError(t, store.Close())

	store = newTestStore(t, base, WithAtRestKey(atRestKey(t, "current")))
	t.Cleanup(func() { _ = store.Close() })

	_, err := store.Get(ctx, blob.Ref())
	require.ErrorIs(t, err, ErrAtRestKeyMismatch)
}

func TestRetiredKeysNeedAKeyToSealWith(t *testing.T) {
	_, err := NewPackStorage(
		WithArchiveStorage(newLocal(t.TempDir())),
		WithArchiveIndex(NewInMemoryIndex()),
		WithRetiredAtRestKey(atRestKey(t, "old")),
	)
	require.Error(t, err)
}
