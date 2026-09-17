package sql

import (
	"database/sql"

	"entgo.io/ent/dialect"
)

// Option configures an index before it is opened.
type Option func(*Index)

// WithDB opens the index over a database the caller connected, instead
// of the location's; d is dialect.SQLite or dialect.Postgres. Open still
// applies the migrations, and Close leaves db open.
func WithDB(db *sql.DB, d string) Option {
	return func(x *Index) {
		x.external = &externalDB{db: db, dialect: d}
	}
}

// externalDB is a caller-connected database.
type externalDB struct {
	db      *sql.DB
	dialect string
}

func (e *externalDB) locking() bool {
	return e.dialect == dialect.Postgres
}
