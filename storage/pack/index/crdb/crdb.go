package crdb

import (
	"database/sql"
	"embed"
	"net/url"

	"github.com/twcclan/goback/pkg/migrations"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/pack"

	_ "github.com/golang-migrate/migrate/v4/database/cockroachdb"
)


//go:embed migrations/*.sql
var files embed.FS

func New(dsn string) *Index {
	return &Index{
		dsn: dsn,
	}
}

type Index struct {
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
	if err != nil {
		return err
	}

	c.db = db

	c.insertArchive, err = db.Prepare("INSERT INTO archives (name) VALUES ($1) ON CONFLICT DO NOTHING")
	if err != nil {
		return err
	}

	c.insertObject, err = db.Prepare(`INSERT INTO objects (ref, start, length, type, archive_id) VALUES($1, $2, $3, $4, $5)`)
	if err != nil {
		return err
	}

	c.locate, err = db.Prepare(`SELECT ref, start, length, type, archive_id FROM objects WHERE ref = $1`) //AND archive_id NOT IN ($2)`)
	if err != nil {
		return err
	}

	return nil
}

func (c *Index) Locate(ref *proto.Ref, exclude ...string) (pack.IndexLocation, error) {
	loc := pack.IndexLocation{}

	rows, err := c.locate.Query(ref.Sha1)
	if err != nil {
		return loc, err
	}

	defer rows.Close()
	sum := make([]byte, 20)

	for rows.Next() {

		err = rows.Scan(
			&sum,
			&loc.Record.Offset,
			&loc.Record.Length,
			&loc.Record.Type,
			&loc.Archive,
		)
		if err != nil {
			return loc, err
		}

		copy(loc.Record.Sum[:], sum)
	}

	return loc, nil
}

func (c *Index) Has(archive string) (bool, error) {
	panic("implement me")
}

func (c *Index) Index(archive string, index pack.IndexFile) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}

	defer tx.Rollback()

	result, err := tx.Stmt(c.insertArchive).Exec(archive)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affected == 0 {
		// archive already existed, nothing to do here
		return nil
	}

	insertStmt := tx.Stmt(c.insertObject)

	for _, record := range index {
		_, err = insertStmt.Exec(record.Sum[:], record.Offset, record.Length, record.Type, archive)

		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (c *Index) Delete(archive string, index pack.IndexFile) error {
	panic("implement me")
}

func (c *Index) Close() error {
	db := c.db
	c.db = nil

	return db.Close()
}

func (c *Index) Count() (uint64, uint64, error) {
	panic("implement me")
}
