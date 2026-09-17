package storekey

import (
	"bytes"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func testKey(t *testing.T) *Key {
	t.Helper()

	key, err := Generate("s1")
	require.NoError(t, err)

	return key
}

func lowEntropy(size int) []byte {
	return bytes.Repeat([]byte("game save data "), size/15+1)[:size]
}

func highEntropy(size int) []byte {
	buf := make([]byte, size)
	rand.New(rand.NewSource(1)).Read(buf)
	return buf
}

func TestChooseFollowsThePolicy(t *testing.T) {
	key := testKey(t)
	small := lowEntropy(1024)
	big := lowEntropy(1 << 20)

	packed := highEntropy(1 << 20)

	require.Equal(t, proto.Encryption_STORE_KEYED, key.Choose(1024, small), "small files are private")
	require.Equal(t, proto.Encryption_CONVERGENT, key.Choose(1<<20, packed), "large packed assets dedup across stores")
	require.Equal(t, proto.Encryption_STORE_KEYED, key.Choose(1<<20, big), "large low-entropy chunks are brute-forceable and stay private")
	require.Equal(t, proto.Encryption_CONVERGENT, key.Choose(DefaultThreshold, packed[:DefaultThreshold]), "the threshold is inclusive")
	require.Equal(t, proto.Encryption_STORE_KEYED, key.Choose(DefaultThreshold-1, packed[:DefaultThreshold-1]))

	key.Policy.Mode = ModeConvergentAll
	require.Equal(t, proto.Encryption_CONVERGENT, key.Choose(1, small))
	key.Policy.Mode = ModeStoreKeyedAll
	require.Equal(t, proto.Encryption_STORE_KEYED, key.Choose(1<<20, packed))
}

func TestEntropy(t *testing.T) {
	require.Equal(t, 0.0, Entropy(nil))
	require.Equal(t, 0.0, Entropy(bytes.Repeat([]byte{7}, 100)))
	require.Less(t, Entropy(lowEntropy(4096)), DefaultEntropyBits)
	require.Greater(t, Entropy(highEntropy(4096)), DefaultEntropyBits)
}

func TestBlobKeysAreDeterministicPerMode(t *testing.T) {
	key := testKey(t)
	other := testKey(t)
	data := lowEntropy(5000)

	require.Equal(t, key.BlobKey(proto.Encryption_STORE_KEYED, data), key.BlobKey(proto.Encryption_STORE_KEYED, data))
	require.NotEqual(t, key.BlobKey(proto.Encryption_STORE_KEYED, data), other.BlobKey(proto.Encryption_STORE_KEYED, data), "store-keyed blobs differ between stores")
	require.Equal(t, key.BlobKey(proto.Encryption_CONVERGENT, data), other.BlobKey(proto.Encryption_CONVERGENT, data), "convergent blobs are shared between stores")
	require.NotEqual(t, key.BlobKey(proto.Encryption_CONVERGENT, data), key.BlobKey(proto.Encryption_STORE_KEYED, data))
}

func TestSealBlobRoundTrip(t *testing.T) {
	key := testKey(t)

	for _, mode := range []proto.Encryption{proto.Encryption_STORE_KEYED, proto.Encryption_CONVERGENT} {
		data := lowEntropy(20000)
		sealed, blobKey := key.SealBlob(mode, data)

		require.True(t, RefOf(blobKey).Equal(sealed.Ref))
		require.Equal(t, proto.ObjectType_BLOB, sealed.Type)
		require.Equal(t, mode, sealed.Encryption)
		require.NotContains(t, string(sealed.Data), "game save")
		require.Less(t, len(sealed.Data), len(data), "compressible data is compressed before sealing")

		if mode == proto.Encryption_STORE_KEYED {
			require.Equal(t, key.ID(), sealed.KeyId)
		} else {
			require.Empty(t, sealed.KeyId)
		}

		opened, err := OpenBlob(blobKey, sealed)
		require.NoError(t, err)
		require.Equal(t, data, opened)

		again, _ := key.SealBlob(mode, data)
		require.Equal(t, sealed.Data, again.Data, "sealing is deterministic")
	}
}

func TestOpenBlobRejectsTampering(t *testing.T) {
	key := testKey(t)
	data := lowEntropy(3000)
	sealed, blobKey := key.SealBlob(proto.Encryption_STORE_KEYED, data)

	_, err := OpenBlob(bytes.Repeat([]byte{1}, KeySize), sealed)
	require.ErrorIs(t, err, ErrWrongKey)

	_, err = OpenBlob(blobKey[:5], sealed)
	require.ErrorIs(t, err, ErrWrongKey)

	flipped := proto.NewObject(sealed).GetSealed()
	flipped.Data = append([]byte(nil), sealed.Data...)
	flipped.Data[len(flipped.Data)/2] ^= 1
	_, err = OpenBlob(blobKey, flipped)
	require.ErrorIs(t, err, ErrWrongKey)

	// a payload moved under another blob's ref does not open
	other, _ := key.SealBlob(proto.Encryption_STORE_KEYED, lowEntropy(3001))
	swapped := &proto.Sealed{Ref: other.Ref, Type: sealed.Type, Data: sealed.Data, Compression: sealed.Compression, Encryption: sealed.Encryption}
	_, err = OpenBlob(blobKey, swapped)
	require.ErrorIs(t, err, ErrWrongKey)
}

func TestInlineRoundTrip(t *testing.T) {
	key := testKey(t)

	ciphertext, blobKey := key.SealInline([]byte("hello"))
	require.NotEqual(t, []byte("hello"), ciphertext)

	opened, err := OpenInline(blobKey, ciphertext)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), opened)

	_, err = OpenInline(testKey(t).BlobKey(proto.Encryption_STORE_KEYED, []byte("hello")), ciphertext)
	require.ErrorIs(t, err, ErrWrongKey)
}

func TestFieldTokensBindTheParent(t *testing.T) {
	key := testKey(t)
	parent := key.SealField(nil, FieldName, []byte("sub"))

	token := key.SealField(parent, FieldName, []byte("save.dat"))
	require.Equal(t, token, key.SealField(parent, FieldName, []byte("save.dat")), "tokens are deterministic")
	require.NotEqual(t, token, key.SealField(nil, FieldName, []byte("save.dat")), "the same name elsewhere gets another token")
	require.NotEqual(t, token, key.SealField(parent, FieldUser, []byte("save.dat")), "fields do not collide")
	require.NotEqual(t, token, testKey(t).SealField(parent, FieldName, []byte("save.dat")))
	require.Empty(t, key.SealField(parent, FieldUser, nil), "empty fields stay empty")

	opened, err := key.OpenField(parent, FieldName, token)
	require.NoError(t, err)
	require.Equal(t, []byte("save.dat"), opened)

	_, err = key.OpenField(nil, FieldName, token)
	require.ErrorIs(t, err, ErrWrongKey)

	_, err = testKey(t).OpenField(parent, FieldName, token)
	require.ErrorIs(t, err, ErrWrongKey)

	opened, err = key.OpenField(parent, FieldName, nil)
	require.NoError(t, err)
	require.Empty(t, opened)
}

func TestKeysRoundTrip(t *testing.T) {
	key := testKey(t)

	var refs []*proto.Ref
	var keys [][]byte
	for i := 0; i < 3; i++ {
		sealed, blobKey := key.SealBlob(proto.Encryption_STORE_KEYED, lowEntropy(100+i))
		refs = append(refs, sealed.Ref)
		keys = append(keys, blobKey)
	}

	sealed, err := key.SealKeys(refs, keys)
	require.NoError(t, err)

	again, err := key.SealKeys(refs, keys)
	require.NoError(t, err)
	require.Equal(t, sealed, again, "the File ref must not depend on a random nonce")

	opened, err := key.OpenKeys(sealed)
	require.NoError(t, err)
	require.Equal(t, keys, opened)

	_, err = testKey(t).OpenKeys(sealed)
	require.ErrorIs(t, err, ErrWrongKey)

	_, err = key.SealKeys(refs, keys[:2])
	require.Error(t, err)

	empty, err := key.SealKeys(nil, nil)
	require.NoError(t, err)
	require.Nil(t, empty)

	none, err := key.OpenKeys(nil)
	require.NoError(t, err)
	require.Nil(t, none)
}

func TestSaveLoad(t *testing.T) {
	key := testKey(t)
	path := filepath.Join(t.TempDir(), "store.key")
	require.NoError(t, key.Save(path))

	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, key.Bytes(), loaded.Bytes())
	require.Equal(t, key.ID(), loaded.ID())
	require.Equal(t, "s1", loaded.StoreID)
	require.Equal(t, DefaultPolicy(), loaded.Policy)
	require.Len(t, key.ID(), IDSize)
}

func TestEscrowRecover(t *testing.T) {
	key := testKey(t)

	escrowed, err := key.Escrow("correct horse")
	require.NoError(t, err)

	recovered, err := Recover("s1", escrowed, "correct horse", key.Policy)
	require.NoError(t, err)
	require.Equal(t, key.Bytes(), recovered.Bytes())

	_, err = Recover("s1", escrowed, "wrong", key.Policy)
	require.Error(t, err)

	_, err = Recover("s2", escrowed, "correct horse", key.Policy)
	require.Error(t, err, "the escrow is bound to its store")

	_, err = Recover("s1", escrowed[:10], "correct horse", key.Policy)
	require.Error(t, err)
}
