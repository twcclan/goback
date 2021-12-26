package crdb

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/twcclan/goback/pkg/migrations"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/pack"

	"github.com/cockroachdb/cockroach-go/v2/crdb"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/cockroachdb"
	"github.com/lib/pq"
)

//go:embed migrations/*.sql
var files embed.FS

func New(dsn string) *Index {
	return &Index{
		dsn: dsn,
	}
}

var _ pack.ArchiveIndex = (*Index)(nil)

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

func (c *Index) Locate(ref *proto.Ref, exclude ...string) (pack.IndexLocation, error) {
	loc := pack.IndexLocation{}

	// TODO: need to add a dummy value to the array here, otherwise the query does not work. need to figure out why!
	rows, err := c.locate.Query(ref.Sha1, pq.StringArray(append(exclude, "dummy")))
	if err != nil {
		log.Printf("failed locating object %x: %v", ref.Sha1, err)
		return loc, err
	}

	defer rows.Close()
	sum := make([]byte, 20)

	// we get either one or zero results, no need to loop
	if !rows.Next() {
		return loc, pack.ErrRecordNotFound
	}

	err = rows.Scan(
		&sum,
		&loc.Record.Offset,
		&loc.Record.Length,
		&loc.Record.Type,
		&loc.Archive,
	)
	if err != nil {
		log.Printf("failed scanning object %x: %v", ref.Sha1, err)
		return loc, err
	}

	copy(loc.Record.Sum[:], sum)

	return loc, rows.Err()
}

func (c *Index) Has(archive string) (bool, error) {
	row := c.db.QueryRow(`SELECT * FROM archives WHERE name = $1`, archive)

	var dummy string
	err := row.Scan(&dummy)

	if err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return true, nil
}

func (c *Index) Index(archive string, index pack.IndexFile) error {
	var archiveExists bool
	err := crdb.ExecuteTx(context.Background(), c.db, nil, func(tx *sql.Tx) error {
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
			archiveExists = true
		}

		return nil
	})

	if err != nil {
		return err
	}

	if archiveExists {
		return nil
	}

	batchSize := 1000
	batch := 0

	for len(index) > 0 {
		stop := len(index)
		if stop > batchSize {
			stop = batchSize
		}

		err := crdb.ExecuteTx(context.Background(), c.db, nil, c.indexBatch(archive, batch*batchSize, index[:stop]))

		if err != nil {
			return err
		}

		index = index[stop:]
		batch++
	}

	return nil
}

func (c *Index) indexBatch(archive string, offset int, index pack.IndexFile) func(tx *sql.Tx) error {
	return func(tx *sql.Tx) error {
		values := []interface{}{archive}

		placeholder := 2
		var placeholders []string

		for i, record := range index {
			values = append(values, index[i].Sum[:], record.Offset, record.Length, record.Type, offset+i)

			placeholders = append(
				placeholders,
				fmt.Sprintf(
					"($%d, $%d, $%d, $%d, $%d, $1)",
					placeholder,
					placeholder+1,
					placeholder+2,
					placeholder+3,
					placeholder+4,
				),
			)

			placeholder += 5
		}

		_, err := tx.Exec(`INSERT INTO objects (ref, start, length, type, rank, archive_id) VALUES `+strings.Join(placeholders, ",")+` ON CONFLICT DO NOTHING`, values...)

		return err
	}
}

func (c *Index) Delete(archive string, _ pack.IndexFile) error {
	return crdb.ExecuteTx(context.Background(), c.db, nil, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM archives WHERE name = $1`, archive)

		return err
	})
}

func (c *Index) Close() error {
	db := c.db
	c.db = nil

	return db.Close()
}

func (c *Index) Count() (uint64, uint64, error) {
	panic("implement me")
}
