package pack

import (
	"context"
	"testing"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/bits-and-blooms/bitset"

	"github.com/stretchr/testify/require"
)

// retiredChain stores two chains and retires the first's commit, returning
// the retired chain and the kept one.
func retiredChain(t *testing.T, store *PackStorage) ([]*proto.Object, []*proto.Object) {
	t.Helper()

	blobs := makeTestData(t, 40)
	gone := makeChain(blobs[:20])
	kept := makeChain(blobs[20:])
	putAll(t, store, append(append([]*proto.Object{}, gone...), kept...))

	require.NoError(t, store.Delete(context.Background(), gone[len(gone)-1].Ref()))
	require.NoError(t, store.Flush())

	return gone, kept
}

func TestCollectWaitsForTheSessionsThatBeganBeforeItCondemned(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	gone, kept := retiredChain(t, store)

	older, _ := beginSession(t, store, "agent-a")

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)

	blocked, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, blocked.SweepSkipped)
	requireStored(t, store, gone, true)

	require.NoError(t, store.EndSession(older))

	younger, _ := beginSession(t, store, "agent-b")
	t.Cleanup(func() { _ = store.EndSession(younger) })

	swept, err := store.Collect(ctx, gcOptions(t, 72*time.Hour))
	require.NoError(t, err)
	require.Empty(t, swept.SweepSkipped, "a session that began after the tombstones does not hold them up")
	require.EqualValues(t, len(gone), swept.ReclaimedObjects)
	requireStored(t, store, gone, false)
	requireStored(t, store, kept, true)
}

func TestCollectKeepsACopyNewerThanItsTombstone(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	gone, _ := retiredChain(t, store)

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)

	blob := gone[0]
	requirePresent(t, store, gone[:1], false)
	require.NoError(t, store.Put(ctx, blob))
	require.NoError(t, store.Flush())
	requirePresent(t, store, gone[:1], true)

	second, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, len(gone), second.ReclaimedObjects, "only the copy older than the tombstone goes")
	require.EqualValues(t, 1, second.Condemned, "the newer copy is condemned anew")
	requireStored(t, store, gone[:1], true)
	requirePresent(t, store, gone[:1], false)
}

func TestCollectKeepsWhatAnUntombstoneTookBack(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	gone, _ := retiredChain(t, store)

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)

	blob := gone[0].Ref()
	require.NoError(t, store.Delete(ctx, proto.TombstoneRef(blob)))
	require.NoError(t, store.Flush())
	requirePresent(t, store, gone[:1], true)

	second, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, len(gone)-1, second.ReclaimedObjects)
	requireStored(t, store, gone[:1], true)
}

func TestCollectDropsOnlyWhatATombstoneStoredBeforeTheHorizonCondemns(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	blobs := makeTestData(t, 20)
	chain := makeChain(blobs)
	putAll(t, store, chain)

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, chain[len(chain)-1].Ref()))
	require.NoError(t, store.Flush())

	second, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.Zero(t, second.ReclaimedObjects, "the retire came after the previous horizon")
	require.EqualValues(t, len(chain)-1, second.Condemned)

	third, err := store.Collect(ctx, gcOptions(t, 72*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, len(chain), third.ReclaimedObjects)
	requireStored(t, store, chain, false)
}

func TestCollectNeverTakesALaterTombstoneForTheOneThatCondemned(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	gone, _ := retiredChain(t, store)
	blob := gone[0].Ref()

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)

	// a session took the tombstone back after the horizon and may still
	// commit a reference; a tombstone stored after that must not count
	require.NoError(t, store.Delete(ctx, proto.TombstoneRef(blob)))
	require.NoError(t, store.Flush())
	require.NoError(t, store.Delete(ctx, blob))
	require.NoError(t, store.Flush())

	second, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, len(gone)-1, second.ReclaimedObjects)
	requireStored(t, store, gone[:1], true)

	third, err := store.Collect(ctx, gcOptions(t, 72*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, third.ReclaimedObjects)
	requireStored(t, store, gone[:1], false)
}

func TestCollectDropsOnlyCopiesOlderThanTheTombstone(t *testing.T) {
	created := time.Now().Truncate(time.Microsecond)
	tomb := Version{Time: created, Offset: 10}
	sum := keyOf(makeRef().Hash)

	run := &gcRun{condemning: map[refKey]Version{sum: tomb}, newestTomb: map[refKey]Version{}}
	ga := &gcArchive{a: &archive{created: created}}
	unmarked := &gcFile{Previous: bitset.New(1), Current: bitset.New(1)}

	older := &IndexRecord{Sum: sum, Offset: 9, Type: uint32(proto.ObjectType_BLOB)}
	newer := &IndexRecord{Sum: sum, Offset: 11, Type: uint32(proto.ObjectType_BLOB)}

	require.True(t, run.droppable(ga, unmarked, 0, older))
	require.False(t, run.droppable(ga, unmarked, 0, newer))
}

func TestCollectWaitsBeforeTheMarkThatConfirms(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	blobs := makeTestData(t, 40)
	putAll(t, store, makeChain(blobs[20:]))

	chain := makeChain(blobs[:20])
	commit := chain[len(chain)-1]
	putAll(t, store, chain[:len(chain)-1])

	sctx, _ := beginSession(t, store, "agent-a")
	requirePresent(t, store, chain[:len(chain)-1], true)

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)

	// the session commits what it deduplicated while the next mark runs
	gcAfterBatch = func(int) error {
		gcAfterBatch = nil
		return store.Put(sctx, commit)
	}
	t.Cleanup(func() { gcAfterBatch = nil })

	second, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, second.SweepSkipped)
	requireStored(t, store, chain, true)

	_, err = store.Collect(ctx, gcOptions(t, 72*time.Hour))
	require.NoError(t, err)
	requireStored(t, store, chain, true)
}

func TestCollectWaitsForACommitThatEndedButHasNotReachedTheIndex(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	chain := makeChain(makeTestData(t, 20))
	putAll(t, store, chain[:len(chain)-1])

	sctx, session := beginSession(t, store, "agent-a")
	requirePresent(t, store, chain[:len(chain)-1], true)

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)

	// the commit took its end and stopped before its archives were committed
	require.NoError(t, store.Put(sctx, makeTestData(t, 1)[0]))
	require.NoError(t, store.Flush())
	require.NoError(t, store.storage.CreateNew(session.ID+SessionEndExt, []byte(sessionCommitted)))

	second, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, second.SweepSkipped)
	requireStored(t, store, chain[:len(chain)-1], true)
	require.NoError(t, store.EndSession(sctx))
}
