package storage

import (
	"context"
	"testing"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

// testSealedStore checks that a store keeps a sealed blob byte for byte
// and hands it back under the client's ref; read is how the store serves
// a blob.
func testSealedStore(t *testing.T, store backup.ObjectStore, read func(context.Context, *proto.Ref) (*proto.Object, error)) {
	t.Helper()

	key, err := storekey.Generate("s1")
	require.NoError(t, err)

	sealed, blobKey := key.SealBlob(proto.Encryption_STORE_KEYED, []byte("secret save data"))
	obj := proto.NewObject(sealed)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, obj))

	got, err := read(ctx, sealed.Ref)
	require.NoError(t, err)
	require.NotNil(t, got.GetSealed())
	require.Equal(t, sealed.Data, got.GetSealed().Data)
	require.Equal(t, sealed.Encryption, got.GetSealed().Encryption)
	require.Equal(t, sealed.KeyId, got.GetSealed().KeyId)
	require.Equal(t, sealed.Compression, got.GetSealed().Compression)
	require.True(t, got.Ref().Equal(sealed.Ref))

	opened, err := storekey.OpenBlob(blobKey, got.GetSealed())
	require.NoError(t, err)
	require.Equal(t, []byte("secret save data"), opened)

	// a sealed object whose bytes do not match its ref is still stored as
	// is: the server cannot check the ref, only the client can
	forged := proto.NewObject(&proto.Sealed{Ref: proto.HashPayload(proto.ObjectType_BLOB, []byte("other")), Type: proto.ObjectType_BLOB, Data: []byte("junk"), Encryption: proto.Encryption_CONVERGENT})
	require.NoError(t, store.Put(ctx, forged))
	back, err := read(ctx, forged.Ref())
	require.NoError(t, err)
	_, err = storekey.OpenBlob(blobKey, back.GetSealed())
	require.ErrorIs(t, err, storekey.ErrWrongKey)
}

func TestSimpleStoreSealed(t *testing.T) {
	store := NewSimpleObjectStore(t.TempDir())
	require.NoError(t, store.Open())
	t.Cleanup(func() { _ = store.Close() })

	testSealedStore(t, store, store.Get)
}

func TestRemoteSealed(t *testing.T) {
	_, dial := startServer(t)
	client := dial("node-1")

	// the server serves blobs only as parts of a referenced file
	readPart := func(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
		file := proto.NewObject(&proto.File{Parts: []*proto.FilePart{{Length: 1, Ref: ref}}})
		if err := client.Put(ctx, file); err != nil {
			return nil, err
		}

		var got *proto.Object
		err := client.ReadParts(ctx, file.Ref(), nil, func(_ int, obj *proto.Object) error {
			got = obj
			return nil
		})

		return got, err
	}

	testSealedStore(t, client, readPart)
}
