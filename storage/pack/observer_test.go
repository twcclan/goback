package pack

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// observed collects what an ArchiveObserver hears.
type observed struct {
	mtx     sync.Mutex
	stored  map[string]int64
	session map[string]string
	deleted []string
}

func newObserved() *observed {
	return &observed{stored: map[string]int64{}, session: map[string]string{}}
}

func (o *observed) ArchiveStored(name string, bytes int64, session string) {
	o.mtx.Lock()
	defer o.mtx.Unlock()

	o.stored[name] = bytes
	o.session[name] = session
}

func (o *observed) ArchiveDeleted(name string) {
	o.mtx.Lock()
	defer o.mtx.Unlock()

	o.deleted = append(o.deleted, name)
}

// archiveBytes is what an archive and its index take up in the storage.
func archiveBytes(t *testing.T, store *PackStorage, name string) int64 {
	t.Helper()

	var total int64

	for _, file := range []string{name + ArchiveSuffix, name + IndexExt} {
		f, err := store.storage.Open(file)
		require.NoError(t, err)

		info, err := f.Stat()
		require.NoError(t, err)
		require.NoError(t, f.Close())

		total += info.Size()
	}

	return total
}

func TestTheObserverHearsEveryArchiveStoredWithTheBytesItTakes(t *testing.T) {
	seen := newObserved()
	store := closing(t, newTestStore(t, t.TempDir(), WithArchiveObserver(seen), WithMaxSize(64*1024)))

	ctx, s := beginSession(t, store, "agent")
	for _, obj := range makeTestData(t, 20) {
		require.NoError(t, store.Put(ctx, obj))
	}
	require.NoError(t, store.Put(ctx, commitObject()))

	require.NotEmpty(t, seen.stored)

	for name, bytes := range seen.stored {
		require.Equal(t, archiveBytes(t, store, name), bytes, "archive %s", name)
		require.Equal(t, s.ID, seen.session[name])
	}
}

func TestTheObserverHearsTheArchivesAnAbortedSessionLeavesBehind(t *testing.T) {
	seen := newObserved()
	store := closing(t, newTestStore(t, t.TempDir(), WithArchiveObserver(seen)))

	ctx, _ := beginSession(t, store, "agent")
	require.NoError(t, store.Put(ctx, makeTestData(t, 1)[0]))
	require.NoError(t, store.Flush())
	require.Len(t, seen.stored, 1)

	require.NoError(t, store.EndSession(ctx))

	for name := range seen.stored {
		require.Equal(t, []string{name}, seen.deleted)
	}
}
