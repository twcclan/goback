package backup

import (
	"bytes"
	"testing"

	"github.com/gobackio/goback/backup/storekey"
	"github.com/gobackio/goback/proto"

	"github.com/stretchr/testify/require"
)

func TestASealedBlobIsCompressedAndNamedByItsKey(t *testing.T) {
	key, err := storekey.Generate("s1")
	require.NoError(t, err)
	data := bytes.Repeat([]byte("game save data "), 2000)

	sealed := SealBlob(key, data)
	require.Equal(t, key.ID(), sealed.KeyId)
	require.True(t, BlobRef(key, data).Equal(sealed.Ref))
	require.Less(t, len(sealed.Data), len(data), "compressible data is compressed before sealing")

	opened, err := OpenBlob(key, sealed)
	require.NoError(t, err)
	require.Equal(t, data, opened)
}

func TestASealedBlobDoesNotOpenUnderAnotherRefOrKey(t *testing.T) {
	key, err := storekey.Generate("s1")
	require.NoError(t, err)
	sealed := SealBlob(key, []byte("one"))

	swapped := &proto.Sealed{Ref: SealBlob(key, []byte("two")).Ref, Type: sealed.Type, Data: sealed.Data,
		Compression: sealed.Compression, Encryption: sealed.Encryption, KeyId: sealed.KeyId}
	_, err = OpenBlob(key, swapped)
	require.ErrorIs(t, err, storekey.ErrWrongKey)

	other, err := storekey.Generate("s1")
	require.NoError(t, err)
	_, err = OpenBlob(other, sealed)
	require.ErrorIs(t, err, storekey.ErrWrongKey)
}
