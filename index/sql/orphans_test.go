package sql

import (
	"context"
	"testing"

	"github.com/gobackio/goback/proto"

	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/fileblob"
)

func TestOrphanCommitsAreTheStoredCommitsWithoutARowOrTombstone(t *testing.T) {
	ctx := context.Background()
	bucket, err := fileblob.OpenBucket(t.TempDir(), nil)
	require.NoError(t, err)
	packs, index := openPacks(t, bucket)

	tree := proto.NewObject(&proto.Tree{})
	require.NoError(t, packs.Put(ctx, tree))

	commit := func(n int64) *proto.Object {
		return proto.NewObject(&proto.Commit{Tree: tree.Ref(), BackupSet: "world", Timestamp: n})
	}

	indexed, orphan, retired := commit(1), commit(2), commit(3)
	require.NoError(t, index.Put(ctx, indexed))
	require.NoError(t, packs.Put(ctx, orphan))
	require.NoError(t, packs.Put(ctx, retired))
	require.NoError(t, packs.Flush())
	require.NoError(t, packs.Delete(ctx, retired.Ref()))
	require.NoError(t, packs.Flush())

	orphans, err := index.OrphanCommits(ctx)
	require.NoError(t, err)
	require.Len(t, orphans, 1)
	require.Equal(t, orphan.Ref().Hash, orphans[0].Ref.Hash)
	require.Equal(t, 1, orphans[0].Copies)
	require.Positive(t, orphans[0].Bytes)
}
