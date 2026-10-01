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

// TestArchiveIndexCopies checks that LocateCopies finds every committed
// copy of each ref, however many refs it is asked about, and those in a
// pending archive only for its own session.
func TestArchiveIndexCopies(t *testing.T, idx pack.ArchiveIndex) {
	big := RandomArchive(1500)
	other := RandomArchive(5)
	other.index = append(other.index, big.index[:10]...)

	require.NoError(t, idx.IndexArchive(pack.ArchiveInfo{Name: big.name}, big.index))
	require.NoError(t, idx.IndexArchive(pack.ArchiveInfo{Name: other.name}, other.index))

	session := &backup.Session{ID: uuid.New().String(), AgentID: "a", Set: "world", Started: time.Now(), LastSeen: time.Now()}
	require.NoError(t, idx.BeginSession(session))

	pending := RandomArchive(1)
	pending.index = append(pending.index, big.index[0])
	require.NoError(t, idx.IndexArchive(pack.ArchiveInfo{Name: session.ID + "/" + pending.name, Session: session.ID, State: pack.ArchivePending}, pending.index))

	var refs []*proto.Ref
	for _, record := range append(append(pack.IndexFile{}, big.index...), other.index[:5]...) {
		refs = append(refs, &proto.Ref{Hash: append([]byte(nil), record.Sum[:]...)})
	}

	unknown := RandomIndexFile(1)[0].Sum
	refs = append(refs, &proto.Ref{Hash: unknown[:]})

	copies, err := idx.LocateCopies(refs, pack.Scope{})
	require.NoError(t, err)
	require.NotContains(t, copies, string(unknown[:]))

	where := func(record pack.IndexRecord) map[string]pack.IndexRecord {
		found := map[string]pack.IndexRecord{}
		for _, loc := range copies[string(record.Sum[:])] {
			found[loc.Archive] = loc.Record
		}

		return found
	}

	for i, record := range big.index {
		found := where(record)
		require.Equal(t, record, found[big.name], "record %d", i)

		if i < 10 {
			require.Len(t, found, 2, "record %d is in both archives", i)
		} else {
			require.Len(t, found, 1, "record %d", i)
		}
	}

	for _, record := range other.index[:5] {
		require.Equal(t, map[string]pack.IndexRecord{other.name: record}, where(record))
	}

	shared := &proto.Ref{Hash: big.index[0].Sum[:]}
	pendingName := session.ID + "/" + pending.name

	for _, scope := range []pack.Scope{{}, {Session: uuid.New().String()}} {
		copies, err = idx.LocateCopies([]*proto.Ref{shared}, scope)
		require.NoError(t, err)

		for _, loc := range copies[string(shared.Hash)] {
			require.NotEqual(t, pendingName, loc.Archive, "a pending copy is seen outside its session")
		}
	}

	copies, err = idx.LocateCopies([]*proto.Ref{shared}, pack.Scope{Session: session.ID})
	require.NoError(t, err)

	var archives []string
	for _, loc := range copies[string(shared.Hash)] {
		archives = append(archives, loc.Archive)
	}

	require.ElementsMatch(t, []string{big.name, other.name, pendingName}, archives, "its session sees its pending copy")
}
