package pack

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestARewriteKeepsWhatItRetiredUntilTheQuarantineEnds(t *testing.T) {
	base := t.TempDir()
	store := newGCStore(t, base)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	gone, kept := retiredChain(t, store)

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)

	second, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.NotZero(t, second.Swept)
	requireStored(t, store, gone, false)

	retired, err := store.markerIDs(RetiredExt)
	require.NoError(t, err)
	require.NotEmpty(t, retired)

	for name := range retired {
		require.True(t, store.hasIndexFile(name), "the files of %s stay", name)
	}

	// another process loads none of them
	other := newGCStore(t, base)
	t.Cleanup(func() { _ = other.Close() })
	names, err := other.archiveNames()
	require.NoError(t, err)
	for _, name := range names {
		require.NotContains(t, retired, name)
	}

	early, err := store.PurgeQuarantine(DefaultQuarantine, time.Now().Add(DefaultQuarantine-24*time.Hour))
	require.NoError(t, err)
	require.Zero(t, early)

	purged, err := store.PurgeQuarantine(DefaultQuarantine, time.Now().Add(DefaultQuarantine+48*time.Hour))
	require.NoError(t, err)
	require.Equal(t, len(retired), purged)

	for name := range retired {
		require.False(t, store.hasIndexFile(name))
	}

	left, err := store.markerIDs(RetiredExt)
	require.NoError(t, err)
	require.Empty(t, left)

	requireStored(t, store, kept, true)
}

func TestRestoringAQuarantinedArchiveBringsItsObjectsBack(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	gone, _ := retiredChain(t, store)

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)
	_, err = store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	requireStored(t, store, gone, false)

	retired, err := store.markerIDs(RetiredExt)
	require.NoError(t, err)

	for name := range retired {
		require.NoError(t, store.RestoreQuarantined(name))
	}

	requireStored(t, store, gone, true)
	require.ErrorIs(t, store.RestoreQuarantined("no-such-archive"), ErrNotQuarantined)
}
