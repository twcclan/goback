// Package generate writes the migration an ent schema change needs, for
// goback's own index and for anything built on goback that keeps its own
// schema the same way.
package generate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"path/filepath"

	"github.com/gobackio/goback/index/sql/migrations"
	"github.com/gobackio/goback/testing/testpg"

	"ariga.io/atlas/sql/migrate"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Schema is the ent schema to diff over db, a scratch database of the
// dialect; a generated ent migrate package's NewSchema over db fits.
type Schema func(db *sql.DB, dialect string) migrations.Differ

// Options says where migrations go and what Postgres to diff against.
type Options struct {
	// Root holds one migration directory per dialect, named after it.
	Root string
	// Name is the name of the migration written.
	Name string
	// PostgresURL is an empty Postgres database; empty starts a container.
	PostgresURL string
}

// Run appends a migration to the directory of each dialect, migrations.SQLite
// or migrations.Postgres, whose schema changed, and logs what it did.
// SQLite diffs against an in-memory database.
func Run(ctx context.Context, schema Schema, options Options, dialects ...string) error {
	for _, dialect := range dialects {
		db, done, err := scratch(ctx, dialect, options.PostgresURL)
		if err != nil {
			return err
		}

		err = diff(ctx, db, schema, dialect, filepath.Join(options.Root, dialect), options.Name)
		done()

		if err != nil {
			return err
		}
	}

	return nil
}

func scratch(ctx context.Context, dialect, postgresURL string) (*sql.DB, func(), error) {
	switch dialect {
	case migrations.SQLite:
		db, err := sql.Open("sqlite", "file:gen?mode=memory&cache=shared&_pragma=foreign_keys(1)")
		if err != nil {
			return nil, nil, err
		}

		db.SetMaxOpenConns(1)

		return db, func() { _ = db.Close() }, nil
	case migrations.Postgres:
		stop := func() {}

		if postgresURL == "" {
			var err error

			postgresURL, stop, err = testpg.Container(ctx)
			if err != nil {
				return nil, nil, fmt.Errorf("starting a Postgres container (give a Postgres URL to use a server instead): %w", err)
			}
		}

		db, err := sql.Open("pgx", postgresURL)
		if err != nil {
			stop()
			return nil, nil, err
		}

		return db, func() { _ = db.Close(); stop() }, nil
	}

	return nil, nil, fmt.Errorf("no dialect %q", dialect)
}

func diff(ctx context.Context, db *sql.DB, schema Schema, dialect, path, name string) error {
	dir, err := migrate.NewLocalDir(path)
	if err != nil {
		return err
	}

	err = migrations.DiffWith(ctx, schema(db, dialect), dir, name)
	switch {
	case errors.Is(err, migrate.ErrNoPlan):
		log.Printf("%s: no changes", dialect)
	case err != nil:
		return fmt.Errorf("%s: %w", dialect, err)
	default:
		log.Printf("%s: wrote migration %q", dialect, name)
	}

	return nil
}
