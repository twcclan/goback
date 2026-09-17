package pack

import (
	"context"
	"testing"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func TestWalkHeadersSeesTombstones(t *testing.T) {
	store, err := NewPackStorage(
		WithArchiveStorage(newLocal(t.TempDir())),
		WithArchiveIndex(NewInMemoryIndex()),
	)
	require.NoError(t, err)

	ctx := context.Background()
	objects := makeTestData(t, 3)
	for _, obj := range objects {
		require.NoError(t, store.Put(ctx, obj))
	}

	gone := objects[1].Ref()
	require.NoError(t, store.Delete(ctx, gone))
	require.NoError(t, store.Flush())

	var tombstones []*proto.Ref
	err = store.WalkHeaders(ctx, proto.ObjectType_TOMBSTONE, func(hdr *proto.ObjectHeader) error {
		require.Equal(t, proto.ObjectType_TOMBSTONE, hdr.Type)
		tombstones = append(tombstones, hdr.TombstoneFor)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, tombstones, 1)
	require.True(t, tombstones[0].Equal(gone))

	all := 0
	err = store.WalkHeaders(ctx, proto.ObjectType_INVALID, func(*proto.ObjectHeader) error {
		all++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, len(objects)+1, all)

	require.NoError(t, store.Close())
}
