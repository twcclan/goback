package pack

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestACollectionHandsItsSweepToARewriter(t *testing.T) {
	base := t.TempDir()
	index := NewInMemoryIndex()
	ctx := context.Background()

	open := func() *PackStorage {
		store, err := NewPackStorage(WithArchiveStorage(newLocal(base)), WithArchiveIndex(index), WithMaxSize(256*1024))
		require.NoError(t, err)
		require.NoError(t, store.Open())
		t.Cleanup(func() { _ = store.Close() })

		return store
	}

	store := open()

	live := makeChain(makeTestData(t, 4))
	putAll(t, store, live)
	gone, _ := retiredChain(t, store)

	handoff := func(ahead time.Duration) CollectOptions {
		opts := gcOptions(t, ahead)
		opts.Handoff = true

		return opts
	}

	_, err := store.Collect(ctx, handoff(0))
	require.NoError(t, err)

	second, err := store.Collect(ctx, handoff(48*time.Hour))
	require.NoError(t, err)
	require.NotZero(t, second.Published)
	require.Zero(t, second.ReclaimedObjects)
	requireStored(t, store, gone, true)

	waiting, err := store.Collect(ctx, handoff(72*time.Hour))
	require.NoError(t, err)
	require.Equal(t, second.Generation, waiting.Waiting)

	rewriter := open()

	report, err := rewriter.RewritePlan(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.Plans)
	require.EqualValues(t, len(gone), report.ReclaimedObjects)

	requireStored(t, rewriter, gone, false)
	requireStored(t, rewriter, live, true)

	third, err := store.Collect(ctx, handoff(96*time.Hour))
	require.NoError(t, err)
	require.Zero(t, third.Waiting)
	require.Equal(t, second.Generation+1, third.Generation)
}
