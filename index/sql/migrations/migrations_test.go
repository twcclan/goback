package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"

	"ariga.io/atlas/sql/migrate"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

var databases atomic.Int64

func memory(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:migrations-%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", databases.Add(1)))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func TestApplyRecordsRevisions(t *testing.T) {
	ctx := context.Background()
	db := memory(t)

	require.NoError(t, Apply(ctx, db, SQLite))

	dir, err := Dir(SQLite)
	require.NoError(t, err)
	files, err := dir.Files()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	rrw := &revisions{db: db}
	require.Equal(t, "", rrw.Ident().Schema)
	revs, err := rrw.ReadRevisions(ctx)
	require.NoError(t, err)
	require.Len(t, revs, len(files))
	for i, rev := range revs {
		require.Equal(t, files[i].Version(), rev.Version)
		require.Equal(t, rev.Total, rev.Applied, "every statement of %s applied", rev.Version)
		require.Empty(t, rev.Error)
		require.Equal(t, "goback", rev.OperatorVersion)
		require.False(t, rev.ExecutedAt.IsZero())
	}

	last, err := rrw.ReadRevision(ctx, revs[len(revs)-1].Version)
	require.NoError(t, err)
	require.Equal(t, revs[len(revs)-1].Hash, last.Hash)

	_, err = rrw.ReadRevision(ctx, "nope")
	require.ErrorIs(t, err, migrate.ErrRevisionNotExist)

	require.NoError(t, Apply(ctx, db, SQLite), "an up-to-date database is left alone")

	// the tables are there
	var n int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM sets").Scan(&n))
	require.Zero(t, n)
}

func TestApplyRefusesADirtyDatabase(t *testing.T) {
	ctx := context.Background()
	db := memory(t)

	_, err := db.ExecContext(ctx, "CREATE TABLE stray (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)

	var clean *migrate.NotCleanError
	require.ErrorAs(t, Apply(ctx, db, SQLite), &clean)
}

// TestDirectoryMatchesSchema replays the SQLite migrations on an empty
// database and expects no difference to the ent schema.
func TestDirectoryMatchesSchema(t *testing.T) {
	dir, err := Dir(SQLite)
	require.NoError(t, err)

	err = Diff(context.Background(), memory(t), SQLite, dir, "drift")
	require.ErrorIs(t, err, migrate.ErrNoPlan, "regenerate the migrations: cd index/sql/migrations && go run ./gen -name <change>")
}

func TestDirRejectsUnknownDialects(t *testing.T) {
	_, err := Dir("oracle")
	require.Error(t, err)

	require.Error(t, Apply(context.Background(), memory(t), "oracle"))
}
