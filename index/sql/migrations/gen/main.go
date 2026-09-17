// Command gen appends a migration to every dialect's directory when the
// ent schema changed. SQLite diffs against an in-memory database;
// Postgres against -postgres-url, an empty scratch database, or a
// container started with Docker.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/twcclan/goback/index/sql/migrations"

	"ariga.io/atlas/sql/migrate"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	_ "modernc.org/sqlite"
)

func main() {
	name := flag.String("name", "changes", "name of the migration")
	root := flag.String("dir", ".", "directory holding one migration directory per dialect")
	pgURL := flag.String("postgres-url", "", "empty Postgres database to diff against; a container is started without one")
	flag.Parse()

	ctx := context.Background()

	db, err := sql.Open("sqlite", "file:gen?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)

	diff(ctx, db, migrations.SQLite, filepath.Join(*root, migrations.SQLite), *name)
	_ = db.Close()

	url := *pgURL
	if url == "" {
		var stop func()
		url, stop = container(ctx)
		defer stop()
	}

	pg, err := sql.Open("pgx", url)
	if err != nil {
		log.Fatal(err)
	}
	defer pg.Close()

	diff(ctx, pg, migrations.Postgres, filepath.Join(*root, migrations.Postgres), *name)
}

func diff(ctx context.Context, db *sql.DB, dialect, path, name string) {
	dir, err := migrate.NewLocalDir(path)
	if err != nil {
		log.Fatal(err)
	}

	err = migrations.Diff(ctx, db, dialect, dir, name)
	switch {
	case errors.Is(err, migrate.ErrNoPlan):
		log.Printf("%s: no changes", dialect)
	case err != nil:
		log.Fatalf("%s: %v", dialect, err)
	default:
		log.Printf("%s: wrote migration %q", dialect, name)
	}
}

// container starts a Postgres container and returns its URL and a stop
// func.
func container(ctx context.Context) (string, func()) {
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:17-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env:          map[string]string{"POSTGRES_PASSWORD": "goback", "POSTGRES_DB": "goback"},
			WaitingFor:   wait.ForListeningPort("5432/tcp"),
		},
		Started: true,
	})
	if err != nil {
		log.Fatalf("starting a Postgres container (pass -postgres-url to use a server instead): %v", err)
	}

	stop := func() { _ = c.Terminate(context.Background()) }

	host, err := c.Host(ctx)
	if err != nil {
		stop()
		log.Fatal(err)
	}

	port, err := c.MappedPort(ctx, "5432")
	if err != nil {
		stop()
		log.Fatal(err)
	}

	url := fmt.Sprintf("postgres://postgres:goback@%s:%s/goback?sslmode=disable", host, port.Port())

	db, err := sql.Open("pgx", url)
	if err != nil {
		stop()
		log.Fatal(err)
	}
	defer db.Close()

	deadline := time.Now().Add(time.Minute)
	for db.PingContext(ctx) != nil {
		if time.Now().After(deadline) {
			stop()
			log.Fatal("Postgres did not come up within a minute")
		}

		time.Sleep(time.Second)
	}

	return url, stop
}
