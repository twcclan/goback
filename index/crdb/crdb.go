package crdb

import (
	"database/sql"
	"embed"
	"errors"
	"net/url"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/pkg/migrations"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/cockroachdb"
	"github.com/lib/pq"
)

// ensure imports remain
var _ pq.Driver
var _ cockroachdb.Config

//go:embed migrations/*.sql
var files embed.FS

func New(dsn string) *Index {
	return &Index{
		dsn: dsn,
	}
}

type Index struct {
	backup.ObjectStore

	dsn string
	db  *sql.DB

	insertArchive *sql.Stmt
	insertObject  *sql.Stmt
	locate        *sql.Stmt
}

func (c *Index) Open() error {
	db, err := sql.Open("postgres", c.dsn)
	if err != nil {
		return err
	}

	if err = db.Ping(); err != nil {
		return err
	}

	u, err := url.Parse(c.dsn)
	if err != nil {
		return err
	}

	u.Scheme = "cockroachdb"

	err = migrations.Run(u.String(), files, "migrations")
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}

	c.db = db
	c.db.SetMaxOpenConns(100)

	c.insertArchive, err = db.Prepare("INSERT INTO archives (name) VALUES ($1) ON CONFLICT DO NOTHING")
	if err != nil {
		return err
	}

	c.insertObject, err = db.Prepare(`INSERT INTO objects (ref, start, length, type, archive_id) VALUES($1, $2, $3, $4, $5)`)
	if err != nil {
		return err
	}

	c.locate, err = db.Prepare(`SELECT ref, start, length, type, archive_id FROM objects WHERE ref = $1 AND NOT (archive_id = ANY ($2)) LIMIT 1`)
	if err != nil {
		return err
	}

	return nil
}
