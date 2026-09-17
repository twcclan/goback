// Package sql is the index of a store: the commits, versions and pins of
// its sets with their retention state, the presence filters, the store
// settings and the archive index of the pack store, in one database that
// is SQLite for a local store and Postgres for a server.
package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/index/sql/migrations"
	"github.com/twcclan/goback/proto"
	"github.com/twcclan/goback/storage/pack"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

var (
	_ backup.Index         = (*Index)(nil)
	_ backup.Retention     = (*Index)(nil)
	_ backup.Retirer       = (*Index)(nil)
	_ backup.PresenceIndex = (*Index)(nil)
	_ backup.HeaderWalker  = (*Index)(nil)
	_ backup.RefScope      = (*Index)(nil)
	_ backup.CommitGate    = (*Index)(nil)
	_ backup.PolicySource  = (*Index)(nil)
	_ pack.ArchiveIndex    = (*Index)(nil)
)

// sqliteFile is the database file inside a local index directory.
const sqliteFile = "index.db"

// Index is the store index over one database.
type Index struct {
	backup.ObjectStore

	location string
	db       *sql.DB
	client   *ent.Client
	// locking is set where SELECT ... FOR UPDATE takes row locks; SQLite
	// serialises transactions as a whole instead.
	locking bool

	external  *externalDB
	noMigrate bool

	// Limits clamp every retention policy.
	Limits retention.Limits
	// DefaultPolicy is the retention policy sets inherit when the store
	// has none set; nil means retention.Default.
	DefaultPolicy *retention.Policy
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// New returns an index over store at location: a directory holds an
// SQLite database and postgres:// names a Postgres server. Open connects
// and applies the pending migrations.
func New(location string, store backup.ObjectStore, opts ...Option) *Index {
	x := &Index{ObjectStore: store, location: location}
	for _, opt := range opts {
		opt(x)
	}

	return x
}

// NewMemory returns an index over an in-memory SQLite database that lives
// as long as the index is open; name keeps two of them apart.
func NewMemory(name string, store backup.ObjectStore, opts ...Option) *Index {
	return New("memory://"+name, store, opts...)
}

func (x *Index) Open() error {
	var (
		db      *sql.DB
		dialect string
		err     error
	)

	switch {
	case x.external != nil:
		db, dialect = x.external.db, x.external.dialect
		x.locking = x.external.locking()
	case strings.HasPrefix(x.location, "postgres://"), strings.HasPrefix(x.location, "postgresql://"):
		db, dialect, err = openPostgres(x.location)
		x.locking = true
	case strings.HasPrefix(x.location, "memory://"):
		db, dialect, err = openSQLite("file:" + strings.TrimPrefix(x.location, "memory://") + "?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(300000)")
		if err == nil {
			// one connection: a shared in-memory database is dropped
			// once its last connection closes, and the pool would open
			// and close them freely
			db.SetMaxOpenConns(1)
		}
	default:
		dir := strings.TrimPrefix(x.location, "sqlite://")
		if u, perr := url.Parse(x.location); perr == nil && u.Scheme == "file" {
			dir = u.Host + u.Path
		}

		path := filepath.ToSlash(filepath.Join(dir, sqliteFile))
		db, dialect, err = openSQLite("file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(300000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate")
	}

	if err != nil {
		return err
	}

	if err := ping(db); err != nil {
		db.Close()
		return fmt.Errorf("connecting to the index: %w", err)
	}

	if !x.noMigrate {
		err = migrations.Apply(context.Background(), db, migrationDialect(dialect))
		if err != nil {
			db.Close()
			return fmt.Errorf("migrating the index schema: %w", err)
		}
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect, db)))

	x.db = db
	x.client = client

	return nil
}

// migrationDialect names the migration directory of an ent dialect.
func migrationDialect(d string) string {
	if d == dialect.Postgres {
		return migrations.Postgres
	}

	return migrations.SQLite
}

func openPostgres(dsn string) (*sql.DB, string, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, "", err
	}

	db.SetMaxOpenConns(100)

	return db, dialect.Postgres, nil
}

func openSQLite(dsn string) (*sql.DB, string, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, "", err
	}

	return db, dialect.SQLite, nil
}

func (x *Index) Close() error {
	if x.client == nil {
		return nil
	}

	client := x.client
	x.client = nil

	if x.external != nil {
		return nil
	}

	return client.Close()
}

// Client is the ent client, for tests that inspect rows.
func (x *Index) Client() *ent.Client {
	return x.client
}

func (x *Index) now() time.Time {
	now := time.Now()
	if x.Now != nil {
		now = x.Now()
	}

	// Postgres keeps microseconds; a stamp that survives the round trip
	// keeps a commit's hash stable
	return now.UTC().Truncate(time.Microsecond)
}

// ping opens the first connection. On SQLite the connection's pragmas
// need the file to themselves for a moment, which another process
// migrating it denies with SQLITE_BUSY, so a busy answer is retried.
func ping(db *sql.DB) error {
	deadline := time.Now().Add(busyWait)
	for {
		err := db.Ping()
		if err == nil || !strings.Contains(err.Error(), "SQLITE_BUSY") || time.Now().After(deadline) {
			return err
		}

		time.Sleep(50 * time.Millisecond)
	}
}

// busyWait bounds how long an open waits for another process's migration.
const busyWait = 5 * time.Minute

// tx runs fn in a transaction, retrying a serialization failure on a
// database that reports one.
func (x *Index) tx(ctx context.Context, fn func(tx *ent.Tx) error) (err error) {
	tx, err := x.client.Tx(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// lockable is a query that can take a row lock.
type lockable[Q any] interface {
	ForUpdate(...entsql.LockOption) Q
}

// forUpdate locks the rows a query selects where the database locks rows.
func forUpdate[Q lockable[Q]](x *Index, q Q) Q {
	if !x.locking {
		return q
	}

	return q.ForUpdate()
}

// Delete implements backup.ObjectStore: objects are retired through
// retention, never deleted directly.
func (x *Index) Delete(ctx context.Context, ref *proto.Ref) error {
	return errors.New("direct deletion of objects not supported")
}

// WalkHeaders implements backup.HeaderWalker when the store does.
func (x *Index) WalkHeaders(ctx context.Context, t proto.ObjectType, fn func(*proto.ObjectHeader) error) error {
	hw, ok := x.ObjectStore.(backup.HeaderWalker)
	if !ok {
		return backup.ErrNotImplemented
	}

	return hw.WalkHeaders(ctx, t, fn)
}

func (x *Index) flush() error {
	if f, ok := x.ObjectStore.(interface{ Flush() error }); ok {
		return f.Flush()
	}

	return nil
}

// storeAs finds the first store in the wrapper chain that implements T.
func storeAs[T any](store backup.ObjectStore) (T, bool) {
	for store != nil {
		if t, ok := store.(T); ok {
			return t, true
		}

		wrapper, ok := store.(interface{ Unwrap() backup.ObjectStore })
		if !ok {
			break
		}

		store = wrapper.Unwrap()
	}

	var zero T

	return zero, false
}

// ignoreNoRows drops the error a DO NOTHING upsert returns when the row
// already exists.
func ignoreNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}

	return err
}

func ptr[T any](v T) *T {
	return &v
}

func nilIfZero(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}

	return *p
}
