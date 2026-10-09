package pack

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/gobackio/goback/proto"

	"github.com/stretchr/testify/require"
)

func atRestKey(t *testing.T) *AtRestKey {
	t.Helper()

	key, err := GenerateAtRestKey()
	require.NoError(t, err)

	return key
}

func TestAtRestSealsPayloadsOnly(t *testing.T) {
	base := t.TempDir()
	key := atRestKey(t)
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
	key := atRestKey(t)
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

	store = newTestStore(t, base, WithAtRestKey(atRestKey(t)))
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

	store = newTestStore(t, base, WithAtRestKey(atRestKey(t)))
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
	key := atRestKey(t)
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
	compact(t, context.Background(), store)

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
	old := atRestKey(t)
	ctx := context.Background()

	current, err := old.Rotate()
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "at-rest.key")
	require.NoError(t, current.Save(path))
	current, err = LoadAtRestKey(path)
	require.NoError(t, err)

	store := newTestStore(t, base, WithAtRestKey(old))
	objects := makeTestData(t, 20)
	for _, obj := range objects {
		require.NoError(t, store.Put(ctx, obj))
		require.NoError(t, store.Flush())
	}
	require.NoError(t, store.Close())

	store = newTestStore(t, base, WithAtRestKey(current), WithCompaction(CompactionConfig{MinimumCandidates: 2}))
	t.Cleanup(func() { _ = store.Close() })

	for _, obj := range objects {
		got, err := store.Get(ctx, obj.Ref())
		require.NoError(t, err)
		require.Equal(t, obj.GetBlob().Data, got.GetBlob().Data)
	}

	report, err := store.Scrub(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(len(objects)), report.Sealed[hex.EncodeToString(old.ID())])

	compact(t, context.Background(), store)

	report, err = store.Scrub(ctx)
	require.NoError(t, err)
	require.Empty(t, report.Corrupt)
	require.Zero(t, report.Sealed[hex.EncodeToString(old.ID())], "nothing names the retired key once a rewrite is over")
	require.Equal(t, uint64(len(objects)), report.Sealed[hex.EncodeToString(current.ID())])
}
