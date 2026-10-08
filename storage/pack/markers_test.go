package pack

import (
	"context"
	"io"
	"testing"

	"github.com/gobackio/goback/backup"

	"github.com/stretchr/testify/require"
)

func readMarker(t *testing.T, store *PackStorage, name string) string {
	t.Helper()

	file, err := store.storage.Open(name)
	require.NoError(t, err)
	defer file.Close()

	data, err := io.ReadAll(file)
	require.NoError(t, err)

	return string(data)
}

func TestASessionLeavesABeginAndTheOutcomeItEndedWith(t *testing.T) {
	store := newTestStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })

	ctx, committed := beginSession(t, store, "agent-a")
	require.Contains(t, readMarker(t, store, committed.ID+SessionBeginExt), "agent-a")

	require.NoError(t, store.Put(ctx, makeTestData(t, 1)[0]))
	require.NoError(t, store.Put(ctx, commitObject()))
	require.Equal(t, string(sessionCommitted), readMarker(t, store, committed.ID+SessionEndExt))

	require.NoError(t, store.EndSession(ctx))
	require.Equal(t, string(sessionCommitted), readMarker(t, store, committed.ID+SessionEndExt), "an end is never replaced")

	ctx, aborted := beginSession(t, store, "agent-b")
	require.NoError(t, store.EndSession(ctx))
	require.Equal(t, string(sessionAborted), readMarker(t, store, aborted.ID+SessionEndExt))
}

func TestACommitLosesToAnEndAlreadyTaken(t *testing.T) {
	store := newTestStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })

	ctx, session := beginSession(t, store, "agent-a")
	obj := makeTestData(t, 1)[0]
	require.NoError(t, store.Put(ctx, obj))
	require.NoError(t, store.Flush())

	// another process's reaper ended the session
	require.NoError(t, store.storage.CreateNew(session.ID+SessionEndExt, []byte(sessionAborted)))

	require.ErrorIs(t, store.Put(ctx, commitObject()), backup.ErrNoSession)
	requireVisible(t, store, context.Background(), obj, false)
}

func TestACommittedEndKeepsTheArchivesTheIndexStillHadPending(t *testing.T) {
	store := newTestStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })

	ctx, session := beginSession(t, store, "agent-a")
	obj := makeTestData(t, 1)[0]
	require.NoError(t, store.Put(ctx, obj))
	require.NoError(t, store.Flush())

	// the commit won the end and stopped before it marked its archives
	require.NoError(t, store.storage.CreateNew(session.ID+SessionEndExt, []byte(sessionCommitted)))

	require.NoError(t, store.EndSession(ctx))
	requireVisible(t, store, context.Background(), obj, true)
}

func TestOpeningAStoreBringsTheIndexInLineWithTheMarkers(t *testing.T) {
	base := t.TempDir()

	first := newTestStore(t, base)
	_, lost := beginSession(t, first, "agent-a")
	_, ended := beginSession(t, first, "agent-b")
	require.NoError(t, first.Close())

	require.NoError(t, first.storage.CreateNew(ended.ID+SessionEndExt, []byte(sessionAborted)))

	index := NewInMemoryIndex()
	require.NoError(t, index.BeginSession(ended))

	second := newTestStore(t, base, WithArchiveIndex(index))
	t.Cleanup(func() { _ = second.Close() })

	found, err := second.LookupSession(context.Background(), lost.ID)
	require.NoError(t, err, "a session the index lost is indexed again")
	require.Equal(t, "agent-a", found.AgentID)

	_, err = second.LookupSession(context.Background(), ended.ID)
	require.ErrorIs(t, err, backup.ErrNoSession, "an indexed session that ended is ended")
}
