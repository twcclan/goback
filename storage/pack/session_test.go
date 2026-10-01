package pack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func beginSession(t *testing.T, store *PackStorage, agent string) (context.Context, *backup.Session) {
	t.Helper()

	s := &backup.Session{AgentID: agent, Set: "world"}
	ctx, err := store.BeginSession(context.Background(), s)
	require.NoError(t, err)

	return ctx, s
}

func commitObject() *proto.Object {
	return proto.NewObject(&proto.Commit{Timestamp: 1, Tree: makeRef(), BackupSet: "world"})
}

func requireVisible(t *testing.T, store *PackStorage, ctx context.Context, obj *proto.Object, visible bool) {
	t.Helper()

	got, err := store.Get(ctx, obj.Ref())
	if visible {
		require.NoError(t, err)
		require.Equal(t, obj.Bytes(), got.Bytes())
	} else {
		require.ErrorIs(t, err, backup.ErrNotFound)
	}

	has, err := store.Has(ctx, obj.Ref())
	require.NoError(t, err)
	require.Equal(t, visible, has)
}

func TestSessionWritesAreVisibleOnlyToTheSessionUntilCommit(t *testing.T) {
	base := t.TempDir()
	store := newTestStore(t, base, WithMaxParallel(4))

	ctxA, sessionA := beginSession(t, store, "agent-a")
	ctxB, _ := beginSession(t, store, "agent-b")
	root := context.Background()

	objects := makeTestData(t, 30)
	for _, obj := range objects {
		require.NoError(t, store.Put(ctxA, obj))
	}

	// in the open archive
	requireVisible(t, store, ctxA, objects[0], true)
	requireVisible(t, store, ctxB, objects[0], false)
	requireVisible(t, store, root, objects[0], false)

	// finalized and indexed as pending
	require.NoError(t, store.Flush())
	file := filepath.Base(archiveFiles(t, filepath.Join(base, sessionA.ID))[0])
	info, known, err := store.index.LookupArchive(sessionA.ID + "/" + strings.TrimSuffix(file, ArchiveSuffix))
	require.NoError(t, err)
	require.True(t, known)
	require.Equal(t, ArchivePending, info.State)
	require.Equal(t, sessionA.ID, info.Session)

	requireVisible(t, store, ctxA, objects[1], true)
	requireVisible(t, store, ctxB, objects[1], false)
	requireVisible(t, store, root, objects[1], false)

	// a commit flips the session's archives to committed
	commit := commitObject()
	require.NoError(t, store.Put(ctxA, commit))

	for _, obj := range append(objects, commit) {
		requireVisible(t, store, ctxA, obj, true)
		requireVisible(t, store, ctxB, obj, true)
		requireVisible(t, store, root, obj, true)
	}

	var walked int
	require.NoError(t, store.Walk(root, false, proto.ObjectType_INVALID, func(*proto.Object) error {
		walked++
		return nil
	}))
	require.Equal(t, len(objects)+1, walked, "Walk sees committed archives")

	// the session continues: later writes are pending again
	later := makeTestData(t, 1)[0]
	require.NoError(t, store.Put(ctxA, later))
	requireVisible(t, store, ctxA, later, true)
	requireVisible(t, store, ctxB, later, false)

	require.NoError(t, store.EndSession(ctxA))
	requireVisible(t, store, root, later, false)
	requireVisible(t, store, root, objects[2], true)

	_, err = store.LookupSession(root, sessionA.ID)
	require.ErrorIs(t, err, backup.ErrNoSession)

	require.NoError(t, store.Close())
}

func TestEndSessionDropsPendingArchives(t *testing.T) {
	base := t.TempDir()
	store := newTestStore(t, base)

	ctx, session := beginSession(t, store, "agent-a")
	objects := makeTestData(t, 10)
	for _, obj := range objects[:5] {
		require.NoError(t, store.Put(ctx, obj))
	}
	require.NoError(t, store.Flush())
	for _, obj := range objects[5:] {
		require.NoError(t, store.Put(ctx, obj))
	}

	prefix := filepath.Join(base, session.ID)
	require.Len(t, archiveFiles(t, prefix), 2, "one finalized, one open")

	require.NoError(t, store.EndSession(ctx))

	_, err := os.Stat(prefix)
	require.True(t, os.IsNotExist(err), "the session prefix is gone")

	names, err := store.storage.List(ArchiveSuffix)
	require.NoError(t, err)
	require.Empty(t, names)

	for _, obj := range objects {
		requireVisible(t, store, ctx, obj, false)
	}

	require.NoError(t, store.Close())
}

func TestOpenHandlesLeftoversOfSessions(t *testing.T) {
	base := t.TempDir()
	index := NewInMemoryIndex()
	store := newTestStore(t, base, WithArchiveIndex(index))

	ctx, session := beginSession(t, store, "agent-a")
	objects := makeTestData(t, 5)
	for _, obj := range objects {
		require.NoError(t, store.Put(ctx, obj))
	}

	// closing finalizes the open archive as pending; the session stays live
	require.NoError(t, store.Close())

	// an archive a crashed writer never finalized
	orphanDir := filepath.Join(base, uuid.New().String())
	require.NoError(t, os.MkdirAll(orphanDir, 0o755))
	orphan := filepath.Join(orphanDir, uuid.New().String()+ArchiveSuffix)
	require.NoError(t, os.WriteFile(orphan, archiveHeader(), 0o644))

	reopened := newTestStore(t, base, WithArchiveIndex(index))

	_, err := os.Stat(orphan)
	require.True(t, os.IsNotExist(err), "an unindexed session archive is deleted on open")

	requireVisible(t, reopened, backup.WithSession(context.Background(), session), objects[0], true)
	requireVisible(t, reopened, context.Background(), objects[0], false)
	require.NoError(t, reopened.Close())

	// with a lease, a session nobody renews is ended by a sweep
	expiring := newTestStore(t, base, WithArchiveIndex(index), WithSessionLease(time.Nanosecond))
	expiring.Sweep(time.Now())

	_, err = expiring.LookupSession(context.Background(), session.ID)
	require.ErrorIs(t, err, backup.ErrNoSession)
	requireVisible(t, expiring, backup.WithSession(context.Background(), session), objects[0], false)

	names, err := expiring.storage.List(ArchiveSuffix)
	require.NoError(t, err)
	require.Empty(t, names)

	require.NoError(t, expiring.Close())
}

func TestIdleArchivesAreFinalized(t *testing.T) {
	store := newTestStore(t, t.TempDir(), WithIdleFinalize(time.Hour))

	ctx, session := beginSession(t, store, "agent-a")
	obj := makeTestData(t, 1)[0]
	require.NoError(t, store.Put(ctx, obj))

	ws := store.lookupWriteSession(session.ID)
	require.NotNil(t, ws.archive)

	store.Sweep(time.Now().Add(30 * time.Minute))
	require.NotNil(t, ws.archive, "not idle yet")

	store.Sweep(time.Now().Add(2 * time.Hour))
	require.Nil(t, ws.archive, "finalized after the idle timeout")

	loc, err := store.index.LocateObject(obj.Ref(), Scope{Session: session.ID})
	require.NoError(t, err)
	require.Equal(t, session.ID, ParsePlacement(loc.Archive).Session)

	requireVisible(t, store, ctx, obj, true)
	requireVisible(t, store, context.Background(), obj, false)

	require.NoError(t, store.Close())
}

func TestLeaseRenewalOnWrites(t *testing.T) {
	store := newTestStore(t, t.TempDir(), WithSessionLease(time.Hour))

	ctx, session := beginSession(t, store, "agent-a")

	// age the lease an hour, in memory and in the index, so the renewal
	// is visible even on a clock that ticks coarsely
	stale := time.Now().Add(-time.Hour)
	require.NoError(t, store.index.TouchSession(session.ID, stale))
	before, err := store.LookupSession(ctx, session.ID)
	require.NoError(t, err)

	ws := store.lookupWriteSession(session.ID)
	ws.mtx.Lock()
	ws.lastTouch = stale
	ws.mtx.Unlock()

	require.NoError(t, store.Put(ctx, makeTestData(t, 1)[0]))

	after, err := store.LookupSession(ctx, session.ID)
	require.NoError(t, err)
	require.True(t, after.LastSeen.After(before.LastSeen))

	require.NoError(t, store.Close())
}

func TestPlacementParsing(t *testing.T) {
	cases := map[string]Placement{
		"abc":      {},
		"sess/abc": {Session: "sess"},
		"a/b/c":    {},
	}

	for name, want := range cases {
		require.Equal(t, want, ParsePlacement(name), name)
	}

	require.Equal(t, "sess", ParsePlacement("sess/abc").Dir())
	require.Equal(t, "", ParsePlacement("abc").Dir())
}

// inSession reports, per object, whether the index serves it from a
// session's archive rather than a root one.
func inSession(t *testing.T, store *PackStorage, objects ...*proto.Object) []bool {
	t.Helper()

	result := make([]bool, len(objects))
	for i, obj := range objects {
		loc, err := store.index.LocateObject(obj.Ref(), Scope{})
		require.NoError(t, err, "object %x", obj.Ref().Hash)
		result[i] = ParsePlacement(loc.Archive).Session != ""
	}

	return result
}

// timestamps reads the header timestamps of every object in the store's
// committed archives, one per copy.
func timestamps(t *testing.T, store *PackStorage) map[string][]time.Time {
	t.Helper()

	stamps := map[string][]time.Time{}
	for _, a := range store.archives {
		if a.state != ArchiveCommitted {
			continue
		}

		require.NoError(t, a.foreach(loadNone, func(hdr *proto.ObjectHeader, _ []byte, _, _ uint32) error {
			key := string(hdr.Ref.Hash)
			stamps[key] = append(stamps[key], hdr.Timestamp.AsTime())
			return nil
		}))
	}

	return stamps
}

// keptTimestamps checks that every surviving copy carries a timestamp one
// of the earlier copies had.
func keptTimestamps(t *testing.T, before, after map[string][]time.Time) {
	t.Helper()

	for key, stamps := range after {
		for _, stamp := range stamps {
			var found bool
			for _, earlier := range before[key] {
				found = found || stamp.Equal(earlier)
			}

			require.True(t, found, "a moved object keeps its timestamp")
		}
	}
}

func TestCompactionMergesSessionsIntoTheRoot(t *testing.T) {
	base := t.TempDir()
	store := newTestStore(t, base, WithMaxParallel(4), WithCompaction(CompactionConfig{MinimumCandidates: 0}))

	tree := func(name string) *proto.Object {
		return proto.NewObject(&proto.Tree{Nodes: []*proto.TreeNode{{Stat: &proto.FileInfo{Name: []byte(name)}, Ref: makeRef()}}})
	}

	blob := makeTestData(t, 1)[0]
	sealed := sealedBlob(t, "sealed chunk")
	onlyA := tree("a")
	onlyB := tree("b")
	both := tree("both")

	ctxA, _ := beginSession(t, store, "agent-a")
	ctxB, _ := beginSession(t, store, "agent-b")

	for _, obj := range []*proto.Object{blob, sealed, onlyA, both} {
		require.NoError(t, store.Put(ctxA, obj))
	}

	for _, obj := range []*proto.Object{blob, sealed, onlyB, both} {
		require.NoError(t, store.Put(ctxB, obj))
	}

	require.NoError(t, store.Put(ctxA, commitObject()))
	require.NoError(t, store.Put(ctxB, commitObject()))

	all := []*proto.Object{blob, sealed, onlyA, onlyB, both}
	require.Equal(t, []bool{true, true, true, true, true}, inSession(t, store, all...))

	before := timestamps(t, store)
	require.NoError(t, store.doCompaction())

	merged := []bool{false, false, false, false, false}
	require.Equal(t, merged, inSession(t, store, all...))

	// one copy survives of an object two sessions wrote
	for _, obj := range []*proto.Object{blob, sealed, both} {
		loc, err := store.index.LocateObject(obj.Ref(), Scope{})
		require.NoError(t, err)
		_, err = store.index.LocateObject(obj.Ref(), Scope{}, loc.Archive)
		require.ErrorIs(t, err, ErrRecordNotFound, "object %x", obj.Ref().Hash)
	}

	after := timestamps(t, store)
	keptTimestamps(t, before, after)

	for _, obj := range all {
		requireVisible(t, store, context.Background(), obj, true)
	}

	names, err := store.storage.List(ArchiveSuffix)
	require.NoError(t, err)
	for _, name := range names {
		require.Empty(t, ParsePlacement(name).Session, "session archives are merged away: %s", name)
	}

	markers, err := store.storage.List(CommittedExt)
	require.NoError(t, err)
	require.Empty(t, markers, "the markers go with the archives they marked")

	// a second run finds every object in place
	require.NoError(t, store.doCompaction())
	require.Equal(t, merged, inSession(t, store, all...))
	keptTimestamps(t, after, timestamps(t, store))

	require.NoError(t, store.Close())
}

// TestOpenKeepsCommittedSessionArchivesWithoutTheIndex reopens a store over
// a fresh archive index, as a reset or a new node does, and expects the
// archives a session committed to survive; only an archive without an
// index file was never finalized.
func TestOpenKeepsCommittedSessionArchivesWithoutTheIndex(t *testing.T) {
	base := t.TempDir()
	store := newTestStore(t, base, WithArchiveIndex(NewInMemoryIndex()))

	ctx, _ := beginSession(t, store, "agent-a")
	objects := makeTestData(t, 5)
	for _, obj := range objects {
		require.NoError(t, store.Put(ctx, obj))
	}
	require.NoError(t, store.Put(ctx, commitObject()))
	require.NoError(t, store.Close())

	orphanDir := filepath.Join(base, uuid.New().String())
	require.NoError(t, os.MkdirAll(orphanDir, 0o755))
	orphan := filepath.Join(orphanDir, uuid.New().String()+ArchiveSuffix)
	require.NoError(t, os.WriteFile(orphan, archiveHeader(), 0o644))

	reopened := newTestStore(t, base, WithArchiveIndex(NewInMemoryIndex()))

	_, err := os.Stat(orphan)
	require.True(t, os.IsNotExist(err), "an archive without an index file is deleted")

	for _, obj := range objects {
		requireVisible(t, reopened, context.Background(), obj, true)
	}

	require.NoError(t, reopened.Close())
}

// failingIndex fails the next IndexArchive or FinalizeArchive.
type failingIndex struct {
	*InMemoryIndex
	failNext bool
}

func (f *failingIndex) IndexArchive(info ArchiveInfo, index IndexFile) error {
	if f.failNext {
		f.failNext = false
		return errors.New("index unavailable")
	}

	return f.InMemoryIndex.IndexArchive(info, index)
}

func (f *failingIndex) FinalizeArchive(name string, within time.Duration, created time.Time) error {
	if f.failNext {
		f.failNext = false
		return errors.New("index unavailable")
	}

	return f.InMemoryIndex.FinalizeArchive(name, within, created)
}

// TestCommitRefusesASessionThatLostAnArchive lets the idle sweep fail to
// index an archive and expects the session's commit to fail rather than
// acknowledge objects no index knows.
func TestCommitRefusesASessionThatLostAnArchive(t *testing.T) {
	base := t.TempDir()
	index := &failingIndex{InMemoryIndex: NewInMemoryIndex()}
	store := newTestStore(t, base, WithArchiveIndex(index), WithIdleFinalize(time.Hour))

	ctx, session := beginSession(t, store, "agent-a")
	require.NoError(t, store.Put(ctx, makeTestData(t, 1)[0]))

	index.failNext = true
	store.Sweep(time.Now().Add(2 * time.Hour))

	ws := store.lookupWriteSession(session.ID)
	require.Nil(t, ws.archive, "the sweep finalized the archive")

	commit := commitObject()
	err := store.Put(ctx, commit)
	require.ErrorContains(t, err, "lost an archive")

	// the refused record is written already; its tombstone keeps a rebuild
	// without the index from taking it for a commit
	require.NoError(t, store.Close())

	rebuilt := newTestStore(t, base, WithArchiveIndex(NewInMemoryIndex()))
	t.Cleanup(func() { _ = rebuilt.Close() })

	var dead bool
	require.NoError(t, rebuilt.WalkHeaders(context.Background(), proto.ObjectType_TOMBSTONE, func(hdr *proto.ObjectHeader) error {
		dead = dead || hdr.GetTombstoneFor().Equal(commit.Ref())
		return nil
	}))
	require.True(t, dead)
}

func TestWritesToAnEndedSessionAreRefused(t *testing.T) {
	store := newTestStore(t, t.TempDir())

	ctx, _ := beginSession(t, store, "agent-a")
	require.NoError(t, store.Put(ctx, makeTestData(t, 1)[0]))
	require.NoError(t, store.EndSession(ctx))

	err := store.Put(ctx, makeTestData(t, 1)[0])
	require.ErrorIs(t, err, backup.ErrNoSession)

	require.NoError(t, store.Close())
}

func sealedBlob(t *testing.T, content string) *proto.Object {
	t.Helper()

	key, err := storekey.Generate("any")
	require.NoError(t, err)

	obj := proto.NewObject(backup.SealBlob(key, []byte(content)))
	obj.KeyId = key.ID()

	return obj
}
