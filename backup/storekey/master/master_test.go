package master

import (
	"path/filepath"
	"testing"

	"github.com/twcclan/goback/backup/storekey"

	"github.com/stretchr/testify/require"
)

func TestAMasterKeyFileIsNotAStoreKey(t *testing.T) {
	m, err := Generate()
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "master.key")
	require.NoError(t, m.Save(path))

	_, err = storekey.Load(path)
	require.ErrorContains(t, err, "not a store key")

	loaded, err := Load(path)
	require.NoError(t, err)

	a, err := m.Derive("s1")
	require.NoError(t, err)
	b, err := loaded.Derive("s1")
	require.NoError(t, err)
	require.Equal(t, a.ID(), b.ID())
}

func TestADerivedKeyDependsOnTheMasterAndTheName(t *testing.T) {
	m, err := Generate()
	require.NoError(t, err)

	a, err := m.Derive("s1")
	require.NoError(t, err)
	again, err := m.Derive("s1")
	require.NoError(t, err)
	b, err := m.Derive("s2")
	require.NoError(t, err)

	data := []byte("game save data")
	require.Equal(t, "s1", a.Name)
	require.Equal(t, a.SealInline(data), again.SealInline(data))
	require.NotEqual(t, a.ID(), b.ID(), "key ids tell derived keys apart")
	require.NotEqual(t, a.Digest(data), b.Digest(data))

	other, err := Generate()
	require.NoError(t, err)
	fromOther, err := other.Derive("s1")
	require.NoError(t, err)
	require.NotEqual(t, a.ID(), fromOther.ID(), "the name alone does not determine the key")

	_, err = m.Derive("")
	require.Error(t, err)
}
