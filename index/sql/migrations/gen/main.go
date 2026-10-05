// Command gen appends a migration to every dialect's directory when the
// ent schema changed. SQLite diffs against an in-memory database;
// Postgres against -postgres-url, an empty scratch database, or a
// container started with Docker.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/twcclan/goback/index/sql/migrations"
	"github.com/twcclan/goback/index/sql/migrations/generate"
)

func main() {
	name := flag.String("name", "changes", "name of the migration")
	root := flag.String("dir", ".", "directory holding one migration directory per dialect")
	pgURL := flag.String("postgres-url", "", "empty Postgres database to diff against; a container is started without one")
	flag.Parse()

	err := generate.Run(context.Background(), migrations.Schema,
		generate.Options{Root: *root, Name: *name, PostgresURL: *pgURL},
		migrations.SQLite, migrations.Postgres)
	if err != nil {
		log.Fatal(err)
	}
}
