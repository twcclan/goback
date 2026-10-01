package badger

import (
	"encoding/binary"
	"os"
	"testing"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/storage/pack"
	"github.com/twcclan/goback/storage/pack/packtest"

	"github.com/stretchr/testify/require"
)

func tempDir(tb testing.TB) string {
	dir, err := os.MkdirTemp("", tb.Name())
	if err != nil {
		tb.Fatalf("failed creating temporary directory for test: %s", err.Error())
	}

	tb.Logf("creating temporary directory: %s", dir)

	tb.Cleanup(func() {
		tb.Logf("removing temporary directory: %s", dir)
		err := os.RemoveAll(dir)
		if err != nil {
			tb.Logf("failed removing temporary directory: %s", err.Error())
		}
	})

	return dir
}

func tempIndex(tb testing.TB) *BadgerIndex {
	dir := tempDir(tb)

	tb.Logf("Creating badger index in %s", dir)
	idx, err := NewBadgerIndex(dir)
	if err != nil {
		tb.Fatalf("failed creating index: %s", err)
	}

	tb.Cleanup(func() {
		tb.Log("closing badger index")

		err := idx.Close()
		if err != nil {
			tb.Logf("failed closing index: %s", err)
		}
	})

	return idx
}

func setupBadger(tb testing.TB) *BadgerIndex {
	tb.Helper()

	idx := tempIndex(tb)
	tb.Cleanup(func() {
		err := idx.Close()
		if err != nil {
			tb.Logf("Failed closing badger index: %s", err)
		}
	})

	return idx
}

func TestBadgerIndex(t *testing.T) {
	idx := setupBadger(t)

	packtest.TestArchiveIndex(t, idx)
}

func TestBadgerIndexExclusion(t *testing.T) {
	idx := setupBadger(t)

	packtest.TestArchiveIndexExclusion(t, idx)
}

func TestBadgerArchiveVersions(t *testing.T) {
	packtest.TestArchiveVersions(t, setupBadger(t))
}

func TestBadgerIndexCopies(t *testing.T) {
	packtest.TestArchiveIndexCopies(t, setupBadger(t))
}

func BenchmarkLookup(b *testing.B) {
	idx := setupBadger(b)

	b.Run("lookup", func(b *testing.B) {
		packtest.BenchmarkLookup(b, idx)
	})
}
func BenchmarkIndex(b *testing.B) {
	idx := setupBadger(b)

	b.Run("insert", func(b *testing.B) {
		packtest.BenchmarkIndex(b, idx)
	})
}

func TestBadgerIndexSessions(t *testing.T) {
	packtest.TestArchiveIndexSessions(t, setupBadger(t))
}

func TestBadgerIndexSessionsSurviveReopen(t *testing.T) {
	dir := tempDir(t)

	idx, err := NewBadgerIndex(dir)
	require.NoError(t, err)

	session := &backup.Session{ID: "s", AgentID: "a", Set: "world", Started: time.Now(), LastSeen: time.Now()}
	require.NoError(t, idx.BeginSession(session))

	archive := packtest.RandomArchive(10)
	require.NoError(t, idx.IndexArchive(pack.ArchiveInfo{Name: "s/" + archive.Name(), Session: "s", State: pack.ArchivePending}, archive.Index()))
	require.NoError(t, idx.Close())

	idx, err = NewBadgerIndex(dir)
	require.NoError(t, err)
	defer idx.Close()

	info, known, err := idx.LookupArchive("s/" + archive.Name())
	require.NoError(t, err)
	require.True(t, known)
	require.Equal(t, pack.ArchivePending, info.State)
	require.Equal(t, "s", info.Session)

	_, err = idx.GetSession("s")
	require.NoError(t, err)
}

func TestEntriesWrittenBeforeVersionsDecodeWithoutThem(t *testing.T) {
	value := badgerIndexEndianness.AppendUint32(nil, 7)
	value = badgerIndexEndianness.AppendUint32(value, 8)
	value = badgerIndexEndianness.AppendUint32(value, 9)

	decoded, err := decodeValue(value)
	require.NoError(t, err)
	require.Equal(t, pack.IndexRecord{Offset: 7, Length: 8, Type: 9}, decoded.record())

	archive := badgerIndexEndianness.AppendUint64(nil, 42)
	archive = append(archive, byte(pack.ArchivePending))
	archive = binary.BigEndian.AppendUint16(archive, 1)
	archive = append(archive, 's')

	id, info, err := decodeArchive("a", archive)
	require.NoError(t, err)
	require.EqualValues(t, 42, id)
	require.Equal(t, pack.ArchiveInfo{Name: "a", Session: "s", State: pack.ArchivePending}, info)
}
