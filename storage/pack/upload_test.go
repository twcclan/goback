package pack

import (
	"context"
	"crypto/md5"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// summed answers with the real checksum of what it holds.
type summed struct {
	ArchiveStorage
}

func (s summed) Checksum(name string) ([]byte, error) {
	file, err := s.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	sum := md5.New()
	if _, err := io.Copy(sum, file); err != nil {
		return nil, err
	}

	return sum.Sum(nil), nil
}

// wrongSum answers with a checksum of bytes nobody wrote.
type wrongSum struct {
	ArchiveStorage
}

func (wrongSum) Checksum(string) ([]byte, error) {
	sum := md5.Sum([]byte("not what was written"))

	return sum[:], nil
}

func TestAnArchiveIsCheckedAgainstWhatTheStorageKept(t *testing.T) {
	ctx := context.Background()

	store, err := NewPackStorage(
		WithArchiveStorage(summed{newLocal(t.TempDir())}),
		WithArchiveIndex(NewInMemoryIndex()),
	)
	require.NoError(t, err)
	require.NoError(t, store.Open())
	t.Cleanup(func() { _ = store.Close() })

	objects := makeTestData(t, 5)
	for _, obj := range objects {
		require.NoError(t, store.Put(ctx, obj))
	}
	require.NoError(t, store.Flush())

	for _, obj := range objects {
		got, err := store.Get(ctx, obj.Ref())
		require.NoError(t, err)
		require.Equal(t, obj.GetBlob().Data, got.GetBlob().Data)
	}
}

func TestAnArchiveTheStorageDidNotKeepIsRefused(t *testing.T) {
	ctx := context.Background()

	store, err := NewPackStorage(
		WithArchiveStorage(wrongSum{newLocal(t.TempDir())}),
		WithArchiveIndex(NewInMemoryIndex()),
	)
	require.NoError(t, err)
	require.NoError(t, store.Open())
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.Put(ctx, makeTestData(t, 1)[0]))
	require.ErrorIs(t, store.Flush(), ErrUploadMismatch)
}
