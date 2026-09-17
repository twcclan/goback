package sql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/index/sql/migrations"

	"ariga.io/atlas/sql/migrate"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// concurrentOpen opens several indexes on one empty database at once; the
// migration runs once and every open succeeds.
func concurrentOpen(t *testing.T, open func() *Index) {
	t.Helper()

	var wg sync.WaitGroup
	results := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()

			x := open()
			results[i] = x.Open()
			if results[i] == nil {
				results[i] = x.Close()
			}
		}()
	}
	wg.Wait()

	for i, err := range results {
		require.NoError(t, err, "open %d", i)
	}
}

func TestConcurrentOpenOfADirectory(t *testing.T) {
	dir := t.TempDir()
	concurrentOpen(t, func() *Index { return New(dir, newMemStore()) })
}

// TestOpensADirectory keeps an SQLite database in the directory the CLI
// names and finds it again on the next open.
func TestOpensADirectory(t *testing.T) {
	dir := t.TempDir()
	store := newMemStore()

	x := New(dir, store)
	require.NoError(t, x.Open())
	_, err := ensureSet(context.Background(), x.client, "world", "node-1", 0, true)
	require.NoError(t, err)
	require.NoError(t, x.Close())

	_, err = os.Stat(filepath.Join(dir, sqliteFile))
	require.NoError(t, err)

	y := New("sqlite://"+filepath.ToSlash(dir), store)
	require.NoError(t, y.Open())
	t.Cleanup(func() { _ = y.Close() })

	sets, err := y.ListSets(context.Background())
	require.NoError(t, err)
	require.Len(t, sets, 1)
	require.Equal(t, "world", sets[0].Name)
	require.Equal(t, "node-1", sets[0].AgentID)
}

// TestStoredDefaultPolicyOutranksTheBuiltIn: the CLI's keep-all default
// applies until an operator stores a default policy, which every set
// without one of its own then follows.
func TestStoredDefaultPolicyOutranksTheBuiltIn(t *testing.T) {
	l := newLocalIndex(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		l.commit("a", "world.dat", int64(i), 0, fmt.Sprint(i))
	}

	commits, err := l.x.CommitInfo(l.ctx, "a", time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, commits, 5)

	require.NoError(t, l.x.SetDefaultPolicy(ctx, &retention.Policy{KeepLast: 3}))
	commits, err = l.x.CommitInfo(l.ctx, "a", time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, commits, 3)

	require.NoError(t, l.x.SetPolicy(ctx, "a", &retention.Policy{KeepLast: 4}))
	commits, err = l.x.CommitInfo(l.ctx, "a", time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, commits, 4, "a set's own policy wins")

	require.NoError(t, l.x.SetPolicy(ctx, "a", nil))
	commits, err = l.x.CommitInfo(l.ctx, "a", time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, commits, 3, "and inherits the default again once cleared")

	require.NoError(t, l.x.SetDefaultPolicy(ctx, nil))
	commits, err = l.x.CommitInfo(l.ctx, "a", time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, commits, 5, "without a stored default the built-in keep-all applies")
}

// TestPostgres runs the suite against a Postgres container; it is skipped
// without Docker.
func TestPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}

	ctx := context.Background()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:17-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_PASSWORD": "goback", "POSTGRES_DB": "goback"},
			WaitingFor:   wait.ForListeningPort("5432/tcp"),
		},
		Started: true,
	})
	if err != nil {
		t.Skipf("no Docker: %v", err)
	}

	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	dsn := fmt.Sprintf("postgres://postgres:goback@%s:%s/goback?sslmode=disable", host, port.Port())

	// wait for the server to accept connections
	deadline := time.Now().Add(time.Minute)
	for {
		x := New(dsn, newMemStore())
		err := x.Open()
		if err == nil {
			require.NoError(t, x.Close())
			break
		}

		t.Logf("opening %s: %v", dsn, err)
		require.False(t, time.Now().After(deadline), "postgres did not come up: %v", err)
		time.Sleep(time.Second)
	}

	var databases int
	previous := openIndex
	openIndex = func(t testing.TB, store backup.ObjectStore) *Index {
		t.Helper()

		databases++
		name := fmt.Sprintf("goback_%d", databases)
		admin := New(dsn, nil)
		require.NoError(t, admin.Open())
		_, err := admin.db.ExecContext(ctx, "CREATE DATABASE "+name)
		require.NoError(t, err)
		require.NoError(t, admin.Close())

		x := New(fmt.Sprintf("postgres://postgres:goback@%s:%s/%s?sslmode=disable", host, port.Port(), name), store)
		require.NoError(t, x.Open())
		t.Cleanup(func() { _ = x.Close() })

		return x
	}
	t.Cleanup(func() { openIndex = previous })

	t.Run("MigrationsMatchSchema", func(t *testing.T) {
		databases++
		name := fmt.Sprintf("goback_%d", databases)
		admin := New(dsn, nil)
		require.NoError(t, admin.Open())
		_, err := admin.db.ExecContext(ctx, "CREATE DATABASE "+name)
		require.NoError(t, err)
		require.NoError(t, admin.Close())

		scratch, err := sql.Open("pgx", fmt.Sprintf("postgres://postgres:goback@%s:%s/%s?sslmode=disable", host, port.Port(), name))
		require.NoError(t, err)
		t.Cleanup(func() { _ = scratch.Close() })

		dir, err := migrations.Dir(migrations.Postgres)
		require.NoError(t, err)
		require.ErrorIs(t, migrations.Diff(ctx, scratch, migrations.Postgres, dir, "drift"), migrate.ErrNoPlan)
	})

	t.Run("ConcurrentSetCreation", func(t *testing.T) {
		x := openIndex(t, newMemStore())

		var (
			wg      sync.WaitGroup
			results = make([]error, 8)
		)

		for i := range results {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, results[i] = ensureSet(ctx, x.client, "world", fmt.Sprintf("node-%d", i), 0, true)
			}(i)
		}
		wg.Wait()

		winners := 0
		for _, err := range results {
			if err == nil {
				winners++
				continue
			}

			require.ErrorIs(t, err, backup.ErrSetOwned, "a loser is refused as any other agent, not with a constraint error")
		}
		require.Equal(t, 1, winners)
	})

	t.Run("ConcurrentOpen", func(t *testing.T) {
		databases++
		name := fmt.Sprintf("goback_%d", databases)
		admin := New(dsn, nil)
		require.NoError(t, admin.Open())
		_, err := admin.db.ExecContext(ctx, "CREATE DATABASE "+name)
		require.NoError(t, err)
		require.NoError(t, admin.Close())

		url := fmt.Sprintf("postgres://postgres:goback@%s:%s/%s?sslmode=disable", host, port.Port(), name)
		concurrentOpen(t, func() *Index { return New(url, newMemStore()) })
	})

	for name, test := range map[string]func(*testing.T){
		"RangesFollowVersions":             TestIndexerRangesFollowVersions,
		"KeepsOrderAndRefusesClosedSets":   TestIndexerKeepsOrderAndRefusesClosedSets,
		"RetentionLifecycle":               TestRetentionLifecycle,
		"RetireMarksRowsOnlyAfterFlush":    TestRetireMarksRowsOnlyAfterFlush,
		"PinsFollowTheSetState":            TestPinsFollowTheSetState,
		"DeleteCommitRefusesAPinnedCommit": TestDeleteCommitRefusesAPinnedCommit,
		"RebuiltSetKeepsEverything":        TestRebuiltSetKeepsEverythingWhilePaused,
		"DeletedSetProceedsWhilePaused":    TestDeletedSetProceedsWhilePaused,
		"TombstonePrunesRefs":              TestTombstonePrunesTheRefsOnlyItReaches,
		"ReIndexReconcilesTombstones":      TestReIndexReconcilesTombstonesOnAnExistingDatabase,
		"RestoreLeasesHoldRetirement":      TestRestoreLeasesHoldRetirement,
		"PinsKeepCommitsAlive":             TestPinsKeepCommitsAlive,
		"PartialCheckpointRetires":         TestPartialCheckpointRetiresWhenSuperseded,
		"DeleteSetAndRebuild":              TestDeleteSetAndRebuild,
		"ReIndexReproducesRanges":          TestReIndexReproducesRanges,
		"PresenceFilterNeverLands":         TestPresenceFilterNeverLandsOnATombstonedCommit,
		"EnsureSetOwnership":               TestEnsureSetOwnership,
		"EnsureSetAdoptsUnownedSet":        TestEnsureSetAdoptsUnownedSet,
		"EnsureSetRecreatesUnderCarriedID": TestEnsureSetRecreatesUnderCarriedID,
		"PresenceFollowsTheHead":           TestPresenceFollowsTheHead,
		"LogicalSizeFollowsCommits":        TestLogicalSizeFollowsCommits,
		"BeginCommitGates":                 TestBeginCommitGates,
		"SetRefsFollowCommits":             TestSetRefsFollowCommits,
		"SameSecondCommitsAreKept":         TestSameSecondCommitsAreKept,
		"LatestCommitFollowsReceiptOrder":  TestLatestCommitFollowsReceiptOrder,
		"PutStampsReceiptTime":             TestPutStampsReceiptTime,
		"PutRejectsDanglingCommit":         TestPutRejectsDanglingCommit,
		"FileInfoOrdersVersions":           TestFileInfoOrdersVersionsByBackupTime,
		"CommitInfoSkipsCheckpoints":       TestCommitInfoSkipsCheckpoints,
		"IndexesASplitRoot":                TestIndexesASplitRoot,
		"UnknownSetIsEmpty":                TestUnknownSetIsEmpty,
		"OperatorRetentionSurface":         TestOperatorRetentionSurface,
		"StoredDefaultPolicy":              TestStoredDefaultPolicyOutranksTheBuiltIn,
		"ArchiveIndex":                     TestArchiveIndex,
		"ArchiveIndexExclusion":            TestArchiveIndexExclusion,
		"ArchiveIndexSessions":             TestArchiveIndexSessions,
		"ArchiveIndexCounts":               TestArchiveIndexCountsAndRestoreSessions,
	} {
		t.Run(name, test)
	}
}
