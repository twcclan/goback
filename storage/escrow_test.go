package storage

import (
	"context"
	"testing"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/storage/pack"

	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/fileblob"
)

func bucketPack(t *testing.T, dir string) *pack.PackStorage {
	t.Helper()

	bucket, err := fileblob.OpenBucket(dir, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = bucket.Close() })

	store, err := pack.NewPackStorage(
		pack.WithArchiveStorage(NewBucketStore(bucket)),
		pack.WithArchiveIndex(pack.NewInMemoryIndex()),
	)
	require.NoError(t, err)
	require.NoError(t, store.Open())
	t.Cleanup(func() { _ = store.Close() })

	return store
}

func escrowed(t *testing.T, key *storekey.Key, passphrase string) backup.EscrowedKey {
	t.Helper()

	data, err := key.Escrow(passphrase)
	require.NoError(t, err)

	return backup.EscrowedKey{KeyID: key.IDString(), Escrowed: data}
}

func TestTheEscrowedKeyLivesInTheBucket(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := bucketPack(t, dir)

	key, err := storekey.Generate("s1")
	require.NoError(t, err)
	first := escrowed(t, key, "correct horse")

	require.NoError(t, store.PutEscrowedKey(ctx, "", first))
	require.NoError(t, store.PutEscrowedKey(ctx, "", first), "keeping the same copy again changes nothing")

	second := escrowed(t, key, "battery staple")
	require.NoError(t, store.PutEscrowedKey(ctx, "", second), "another copy of the same key is kept beside the first")

	other, err := storekey.Generate("s1")
	require.NoError(t, err)
	require.ErrorIs(t, store.PutEscrowedKey(ctx, "", escrowed(t, other, "correct horse")), backup.ErrOtherKeyEscrowed)

	require.ErrorIs(t, store.PutEscrowedKey(ctx, "", backup.EscrowedKey{KeyID: key.IDString(), Escrowed: []byte("{}")}), backup.ErrInvalidEscrow)
	require.ErrorIs(t, store.PutEscrowedKey(ctx, "", backup.EscrowedKey{KeyID: "../x", Escrowed: first.Escrowed}), backup.ErrInvalidEscrow)

	// a store opened over the same bucket with a fresh index still has both
	kept, err := bucketPack(t, dir).EscrowedKeys(ctx, "")
	require.NoError(t, err)
	require.ElementsMatch(t, []backup.EscrowedKey{first, second}, kept)
}

func TestEachOwnerSeesOnlyItsOwnEscrowedKeys(t *testing.T) {
	ctx := context.Background()
	store := bucketPack(t, t.TempDir())

	a, err := storekey.Generate("s1")
	require.NoError(t, err)
	b, err := storekey.Generate("s1")
	require.NoError(t, err)

	require.NoError(t, store.PutEscrowedKey(ctx, "owner-a", escrowed(t, a, "correct horse")))
	require.NoError(t, store.PutEscrowedKey(ctx, "owner-b", escrowed(t, b, "correct horse")), "owners keep keys apart")

	kept, err := store.EscrowedKeys(ctx, "owner-a")
	require.NoError(t, err)
	require.Len(t, kept, 1)
	require.Equal(t, a.IDString(), kept[0].KeyID)

	kept, err = store.EscrowedKeys(ctx, "")
	require.NoError(t, err)
	require.Empty(t, kept)

	require.ErrorIs(t, store.PutEscrowedKey(ctx, "../owner-b", escrowed(t, a, "correct horse")), backup.ErrInvalidEscrow)
}

type escrowIndex struct {
	*memIndex
	backup.KeyEscrow
}

func TestAnAgentFetchesTheEscrowedKey(t *testing.T) {
	ctx := context.Background()
	store := bucketPack(t, t.TempDir())

	key, err := storekey.Generate("s1")
	require.NoError(t, err)
	held := escrowed(t, key, "correct horse")
	require.NoError(t, store.PutEscrowedKey(ctx, "", held))

	client := startServerWith(t, escrowIndex{memIndex: newMemIndex(), KeyEscrow: store}, nil)("node-1")

	kept, err := client.EscrowedKeys(ctx)
	require.NoError(t, err)
	require.Equal(t, []backup.EscrowedKey{held}, kept)

	opened, err := storekey.Recover(kept[0].Escrowed, "correct horse")
	require.NoError(t, err)
	require.Equal(t, key.ID(), opened.ID())
}
