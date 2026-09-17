package packtest

import (
	"testing"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/pack"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestArchiveIndexPublicRefs exercises the public prefix's reference set.
func TestArchiveIndexPublicRefs(t *testing.T, idx pack.ArchiveIndex) {
	a, b := RandomArchive(2).index[0].Sum, RandomArchive(2).index[1].Sum

	has, err := idx.HasPublicRef(a[:])
	require.NoError(t, err)
	require.False(t, has)

	require.NoError(t, idx.RecordPublicRefs([][]byte{a[:], b[:]}))
	require.NoError(t, idx.RecordPublicRefs([][]byte{a[:]}), "recording twice is fine")

	for _, ref := range [][]byte{a[:], b[:]} {
		has, err := idx.HasPublicRef(ref)
		require.NoError(t, err)
		require.True(t, has)
	}

	require.NoError(t, idx.ForgetRefs([][]byte{a[:]}))

	has, err = idx.HasPublicRef(a[:])
	require.NoError(t, err)
	require.False(t, has)

	has, err = idx.HasPublicRef(b[:])
	require.NoError(t, err)
	require.True(t, has, "other refs stay")
}

// TestArchiveIndexSessions checks session visibility, commit and abort.
func TestArchiveIndexSessions(t *testing.T, idx pack.ArchiveIndex) {
	session := &backup.Session{ID: uuid.New().String(), AgentID: "a", Set: "world", Started: time.Now(), LastSeen: time.Now()}
	other := &backup.Session{ID: uuid.New().String(), AgentID: "b", Set: "world", Started: time.Now(), LastSeen: time.Now()}

	pending := RandomArchive(50)
	committed := RandomArchive(50)

	err := idx.IndexArchive(pack.ArchiveInfo{Name: session.ID + "/" + pending.name, Session: session.ID, State: pack.ArchivePending}, pending.index)
	require.ErrorIs(t, err, backup.ErrNoSession, "a pending archive needs a live session")

	require.NoError(t, idx.BeginSession(session))
	require.NoError(t, idx.BeginSession(other))
	require.Error(t, idx.BeginSession(session), "session ids are unique")

	got, err := idx.GetSession(session.ID)
	require.NoError(t, err)
	require.Equal(t, session.ID, got.ID)
	require.Equal(t, "a", got.AgentID)
	require.Equal(t, "world", got.Set)

	later := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	require.NoError(t, idx.TouchSession(session.ID, later))
	got, err = idx.GetSession(session.ID)
	require.NoError(t, err)
	require.True(t, got.LastSeen.Equal(later))
	require.ErrorIs(t, idx.TouchSession("nope", later), backup.ErrNoSession)

	sessions, err := idx.ListSessions()
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	pendingName := session.ID + "/" + pending.name
	require.NoError(t, idx.IndexArchive(pack.ArchiveInfo{Name: pendingName, Session: session.ID, State: pack.ArchivePending}, pending.index))
	require.NoError(t, idx.IndexArchive(pack.ArchiveInfo{Name: committed.name}, committed.index))

	info, known, err := idx.LookupArchive(pendingName)
	require.NoError(t, err)
	require.True(t, known)
	require.Equal(t, pack.ArchiveInfo{Name: pendingName, Session: session.ID, State: pack.ArchivePending}, info)

	ref := &proto.Ref{Hash: pending.index[0].Sum[:]}
	mine := pack.Scope{Session: session.ID}
	theirs := pack.Scope{Session: other.ID}

	_, err = idx.LocateObject(ref, mine)
	require.NoError(t, err, "a session sees its own pending archives")
	_, err = idx.LocateObject(ref, theirs)
	require.ErrorIs(t, err, pack.ErrRecordNotFound, "another session does not")
	_, err = idx.LocateObject(ref, pack.Scope{})
	require.ErrorIs(t, err, pack.ErrRecordNotFound, "nor an unscoped caller")

	committedRef := &proto.Ref{Hash: committed.index[0].Sum[:]}
	_, err = idx.LocateObject(committedRef, theirs)
	require.NoError(t, err, "committed archives are visible to every session")
	_, err = idx.LocateObject(committedRef, pack.Scope{})
	require.NoError(t, err)

	require.ErrorIs(t, idx.CommitSession("nope"), backup.ErrNoSession, "a session the index does not hold cannot commit")
	require.NoError(t, idx.CommitSession(session.ID))

	_, err = idx.LocateObject(ref, theirs)
	require.NoError(t, err, "committed archives are visible to everyone")
	info, _, err = idx.LookupArchive(pendingName)
	require.NoError(t, err)
	require.Equal(t, pack.ArchiveCommitted, info.State)
	require.Empty(t, info.Session)

	// a second pending archive of the same session, then abort
	second := RandomArchive(20)
	secondName := session.ID + "/" + second.name
	require.NoError(t, idx.IndexArchive(pack.ArchiveInfo{Name: secondName, Session: session.ID, State: pack.ArchivePending}, second.index))

	dropped, err := idx.EndSession(session.ID)
	require.NoError(t, err)
	require.Equal(t, []string{secondName}, dropped)

	_, err = idx.LocateObject(&proto.Ref{Hash: second.index[0].Sum[:]}, mine)
	require.ErrorIs(t, err, pack.ErrRecordNotFound)
	_, known, err = idx.LookupArchive(secondName)
	require.NoError(t, err)
	require.False(t, known)
	_, err = idx.LocateObject(ref, pack.Scope{})
	require.NoError(t, err, "committed archives of the session stay")

	_, err = idx.GetSession(session.ID)
	require.ErrorIs(t, err, backup.ErrNoSession)

	dropped, err = idx.EndSession(other.ID)
	require.NoError(t, err)
	require.Empty(t, dropped)
}
