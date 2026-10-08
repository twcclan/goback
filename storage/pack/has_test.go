package pack

import (
	"context"
	"strconv"
	"testing"

	"github.com/gobackio/goback/proto"

	"github.com/stretchr/testify/require"
)

func requireHas(t *testing.T, store *PackStorage, ctx context.Context, ref *proto.Ref, has bool, msg string) {
	t.Helper()

	got, err := store.Has(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, has, got, msg)
}

func TestTheNewestRecordOfAnObjectDecidesWhetherItIsPresent(t *testing.T) {
	store := newTestStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	obj := makeTestData(t, 1)[0]
	tomb := proto.TombstoneRef(obj.Ref())

	require.NoError(t, store.Put(ctx, obj))
	require.NoError(t, store.Flush())
	requireHas(t, store, ctx, obj.Ref(), true, "a copy")

	require.NoError(t, store.Delete(ctx, obj.Ref()))
	require.NoError(t, store.Flush())
	requireHas(t, store, ctx, obj.Ref(), false, "a tombstone newer than every copy")

	require.NoError(t, store.Delete(ctx, tomb))
	require.NoError(t, store.Flush())
	requireHas(t, store, ctx, obj.Ref(), true, "an un-tombstone newer than the tombstone")

	require.NoError(t, store.Delete(ctx, obj.Ref()))
	require.NoError(t, store.Flush())
	requireHas(t, store, ctx, obj.Ref(), false, "a tombstone newer than the un-tombstone")

	require.NoError(t, store.Put(ctx, obj))
	requireHas(t, store, ctx, obj.Ref(), true, "a copy in an archive still being written")

	require.NoError(t, store.Flush())
	requireHas(t, store, ctx, obj.Ref(), true, "a copy newer than the tombstone")
}

func TestARewrittenCopyStaysOlderThanTheTombstoneAfterIt(t *testing.T) {
	store := newTestStore(t, t.TempDir(), WithMaxSize(64*1024*1024), WithCompaction(CompactionConfig{MinimumCandidates: 0, Workers: 1}))
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	objects := makeTestData(t, 2)

	for _, obj := range objects {
		require.NoError(t, store.Put(ctx, obj))
		require.NoError(t, store.Flush())
	}

	require.NoError(t, store.Delete(ctx, objects[0].Ref()))
	require.NoError(t, store.Flush())

	require.NoError(t, store.doCompaction())

	loc, err := store.index.LocateObject(objects[0].Ref(), Scope{})
	require.NoError(t, err)
	require.NotZero(t, loc.Record.CarriedTime, "the copy was not rewritten")

	requireHas(t, store, ctx, objects[0].Ref(), false, "the rewrite kept the copy's version")
	requireHas(t, store, ctx, objects[1].Ref(), true, "an object without a tombstone")
}

func TestHasAllAnswersEachRefAsItsOwnHasWould(t *testing.T) {
	store := newTestStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	objects := makeTestData(t, 5)
	kept, deleted, revived, pending, absent := objects[0], objects[1], objects[2], objects[3], objects[4]

	for _, obj := range []*proto.Object{kept, deleted, revived} {
		require.NoError(t, store.Put(ctx, obj))
	}
	require.NoError(t, store.Flush())

	require.NoError(t, store.Delete(ctx, deleted.Ref()))
	require.NoError(t, store.Delete(ctx, revived.Ref()))
	require.NoError(t, store.Flush())
	require.NoError(t, store.Delete(ctx, proto.TombstoneRef(revived.Ref())))
	require.NoError(t, store.Flush())
	require.NoError(t, store.Put(ctx, pending))

	refs := []*proto.Ref{absent.Ref(), kept.Ref(), deleted.Ref(), revived.Ref(), pending.Ref(), kept.Ref()}

	has, err := store.HasAll(ctx, refs)
	require.NoError(t, err)
	require.Equal(t, []bool{false, true, false, true, true, true}, has)

	for i, ref := range refs {
		requireHas(t, store, ctx, ref, has[i], "ref "+strconv.Itoa(i))
	}
}
