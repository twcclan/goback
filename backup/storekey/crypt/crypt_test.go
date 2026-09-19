package crypt_test

import (
	"encoding/hex"
	"testing"

	"github.com/twcclan/goback/backup/storekey/crypt"

	"github.com/stretchr/testify/require"
)

// key is the fixed key every pin below was made under.
var key = bytes("0909090909090909090909090909090909090909090909090909090909090909")

func bytes(s string) []byte {
	raw, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}

	return raw
}

// Everything sealed here is deterministic, so these pin the stored format.
// A store written last year has to keep opening, and a name has to come
// out of the browser's module the same way it went into an archive, so a
// change that moves any of these bytes has to be a deliberate new version
// rather than a refactor nobody noticed.
func TestTheSealedFormatIsWhatItWas(t *testing.T) {
	home := crypt.SealField(key, nil, crypt.FieldName, []byte("home"))
	inline, blobKey := crypt.SealInline(key, []byte("small file"))

	sealedKeys, err := crypt.SealKeys(key, [][]byte{[]byte("hash-one"), []byte("hash-two")}, [][]byte{key, key})
	require.NoError(t, err)

	derived, err := crypt.Derive(key, "s2")
	require.NoError(t, err)

	for _, pin := range []struct {
		what string
		got  []byte
		want string
	}{
		{"the key id", crypt.ID(key), "01da38ad0e765a9a"},
		{
			"a name in the root",
			home,
			"1069c3a36279046344ded265b9679f3fc959f4c2563596f40f46a3a28ff8f68265afe3f0a8411e486cfd527c",
		},
		{
			"a name inside a directory",
			crypt.SealField(key, home, crypt.FieldName, []byte("notes.txt")),
			"55597563446c8755b24a61967d6209c446963b3ce6e70f80aa6640a28c6c937fa693f417c5bba465033d55ab8f5b8ba1e1",
		},
		{
			"a symlink target",
			crypt.SealField(key, home, crypt.FieldTarget, []byte("../etc")),
			"42e66877d6a8bf6a6073282dd61d431076d5e71ffe0c1304a13a1cc386c29f1ee7fe69098654786f5dccab82c45e",
		},
		{"a store-keyed blob key", blobKey, "91f8fc2e92ef61cbb70ab24ff1d7fbe3b6a8fac618714b72fdf87db28780fd05"},
		{"an inlined file", inline, "7ec4bd8834998373a0504fb86cb35514c1bdfbe7cea96209a714"},
		{
			"a file's part keys",
			sealedKeys,
			"ef5d54c22beb1a58da9b9e816b5004b13f35683475b0395f39b4f84beeff979c1e3d20d464c4c0ecd0eef9a2c4e88d6d" +
				"c582b94f4052898d9bff9bb69a21d38dfe7606c72186c5f45ed732421ecb204a1a9f32e61756f51ef6426562f5348ce9" +
				"58632d5075b00b38",
		},
		{"another store's key under this one", derived, "ec60d995dec3d8e940190d2cd0b79b23c59357c7a3a07456f86c24b9f39ad9e5"},
	} {
		require.Equal(t, pin.want, hex.EncodeToString(pin.got), pin.what)
	}
}

// An escrow carries a random salt and nonce, so what is pinned is that one
// made before this change still opens.
func TestAnEscrowMadeEarlierStillOpens(t *testing.T) {
	escrowed := bytes(
		"676f6261636b2d657363726f772d7631088bc09c9347f3803bcb31e31511b53f04398f62b289615dcfe4849dffcf5446" +
			"234c29ad500e258c8ac06ad824eed89b74d062efd141dd6db81019065c9efe4eb1d846958b40ce524caa6abbe73f" +
			"d1cc299b84de46c7e982",
	)

	raw, err := crypt.Recover("s1", escrowed, "correct horse battery staple")
	require.NoError(t, err)
	require.Equal(t, key, raw)

	_, err = crypt.Recover("s2", escrowed, "correct horse battery staple")
	require.Error(t, err, "an escrow is bound to the store it was made for")

	_, err = crypt.Recover("s1", escrowed, "hunter2")
	require.Error(t, err)
}

func TestWhatWasSealedComesBack(t *testing.T) {
	home := crypt.SealField(key, nil, crypt.FieldName, []byte("home"))

	name, err := crypt.OpenField(key, nil, crypt.FieldName, home)
	require.NoError(t, err)
	require.Equal(t, "home", string(name))

	_, err = crypt.OpenField(key, home, crypt.FieldName, home)
	require.ErrorIs(t, err, crypt.ErrWrongKey, "a name only opens in the directory it was sealed in")

	_, err = crypt.OpenField(key, nil, crypt.FieldTarget, home)
	require.ErrorIs(t, err, crypt.ErrWrongKey, "a name is not a target")

	empty, err := crypt.OpenField(key, nil, crypt.FieldName, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
	require.Empty(t, crypt.SealField(key, nil, crypt.FieldName, nil), "an empty value stays empty")
}

func TestAFilesPartKeysComeBackInOrder(t *testing.T) {
	other := bytes("0101010101010101010101010101010101010101010101010101010101010101")
	hashes := [][]byte{[]byte("hash-one"), []byte("hash-two")}

	sealed, err := crypt.SealKeys(key, hashes, [][]byte{key, other})
	require.NoError(t, err)

	parts, err := crypt.OpenKeys(key, sealed)
	require.NoError(t, err)
	require.Equal(t, [][]byte{key, other}, parts)

	_, err = crypt.OpenKeys(other, sealed)
	require.ErrorIs(t, err, crypt.ErrWrongKey)

	_, err = crypt.SealKeys(key, hashes, [][]byte{key})
	require.Error(t, err, "a key list that does not line up with its parts is refused")
}
