package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"contrib.go.opencensus.io/integrations/ocsql"
	_ "github.com/mattn/go-sqlite3"
)

// schemaVersion is stored in the database's user_version; databases with an
// older schema are dropped and need a ReIndex.
const schemaVersion = 2

var tracedSQLiteDriver = ""

func init() {
	var err error

	// register openconsensus database wrapper
	tracedSQLiteDriver, err = ocsql.Register("sqlite3", ocsql.WithAllTraceOptions())
	if err != nil {
		log.Fatalf("failed to register ocsql driver: %s", err)
	}
}

func NewIndex(base, backupSet string, store backup.ObjectStore) *Index {
	idx := &Index{base: base, backupSet: backupSet, txMtx: new(sync.Mutex), ObjectStore: store}
	return idx
}

var _ backup.Index = (*Index)(nil)

type Index struct {
	backup.ObjectStore
	base      string
	backupSet string
	db        *sql.DB
	txMtx     *sync.Mutex
}

//go:embed sql/*.sql
var files embed.FS

func (s *Index) Open() error {
	db, err := sql.Open(tracedSQLiteDriver, path.Join(s.base, "index.db")+"?busy_timeout=1000")
	if err != nil {
		return err
	}

	err = db.Ping()
	if err != nil {
		return err
	}

	err = migrate(db)
	if err != nil {
		db.Close()
		return err
	}

	s.db = db
	return nil
}

func migrate(db *sql.DB) error {
	var version int
	err := db.QueryRow("PRAGMA user_version;").Scan(&version)
	if err != nil {
		return err
	}

	if version < schemaVersion {
		log.Printf("Index schema is version %d, need %d: recreating tables, run `goback fix` to reindex", version, schemaVersion)

		for _, table := range []string{"objects", "files", "commits"} {
			_, err = db.Exec("DROP TABLE IF EXISTS `" + table + "`;")
			if err != nil {
				return err
			}
		}
	}

	sqlFiles, err := files.ReadDir("sql")
	if err != nil {
		return err
	}

	for _, file := range sqlFiles {
		data, err := files.ReadFile(path.Join("sql", file.Name()))
		if err != nil {
			return err
		}

		_, err = db.Exec(string(data))
		if err != nil {
			return err
		}
	}

	_, err = db.Exec("PRAGMA user_version = " + string(rune('0'+schemaVersion)) + ";")

	return err
}

// FindMissing removes index rows whose file object is no longer in the store.
func (s *Index) FindMissing(ctx context.Context) error {
	refs := make([][]byte, 0)
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT ref FROM files;")
	if err != nil {
		return err
	}

	defer rows.Close()
	for rows.Next() {
		ref := make([]byte, 0)
		err = rows.Scan(&ref)
		if err != nil {
			return err
		}

		_, err := s.ObjectStore.Get(ctx, &proto.Ref{Hash: ref})
		if errors.Is(err, backup.ErrNotFound) {
			log.Printf("Found missing object %x", ref)
			refs = append(refs, ref)
			continue
		}

		if err != nil {
			return err
		}
	}

	err = rows.Err()
	if err != nil {
		return err
	}

	rows.Close()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	log.Printf("Deleting %d missing objects", len(refs))
	for _, ref := range refs {
		_, err := tx.Exec("DELETE FROM files WHERE ref = ?;", ref)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Index) Close() error {
	return s.db.Close()
}

func (s *Index) ReIndex(ctx context.Context) error {
	return s.ObjectStore.Walk(ctx, true, proto.ObjectType_COMMIT, func(obj *proto.Object) error {
		if obj.GetCommit().GetBackupSet() != s.backupSet {
			return nil
		}

		return s.index(ctx, obj.GetCommit(), obj.Ref(), false)
	})
}

// LatestCommit implements backup.Index.
func (s *Index) LatestCommit(ctx context.Context, set string) (*proto.Ref, error) {
	ref := &proto.Ref{}

	err := s.db.QueryRowContext(ctx, "SELECT ref FROM commits WHERE backup_set = ? ORDER BY timestamp DESC LIMIT 1;", set).Scan(&ref.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, backup.ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	return ref, nil
}

// index records a commit's files. In strict mode a commit whose tree cannot
// be traversed is rejected with backup.ErrDanglingRef; otherwise it is
// logged and skipped, which is what a re-index over old data wants.
func (s *Index) index(ctx context.Context, commit *proto.Commit, ref *proto.Ref, strict bool) error {
	log.Printf("Indexing commit: %v", time.Unix(commit.Timestamp, 0))

	// whenever we get to index a commit
	// we'll traverse the complete backup tree
	// to create our filesystem path index

	treeObj, err := s.ObjectStore.Get(ctx, commit.Tree)
	if errors.Is(err, backup.ErrNotFound) {
		if strict {
			return fmt.Errorf("%w: root tree %x", backup.ErrDanglingRef, commit.Tree.Hash)
		}

		log.Printf("Root tree %x could not be retrieved", commit.Tree.Hash)
		return nil
	}

	if err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "INSERT OR IGNORE INTO files(backup_set, path, timestamp, size, mode, ref) VALUES(?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	var txMtx sync.Mutex
	err = backup.TraverseTree(ctx, s.ObjectStore, treeObj, 64, func(filepath string, node *proto.TreeNode) error {
		info := node.Stat
		if info.IsDir() || info.GetType() == proto.NodeType_NODE_SYMLINK {
			return nil
		}

		txMtx.Lock()
		defer txMtx.Unlock()

		// store the relative path to this file in the index
		_, sqlErr := stmt.ExecContext(ctx, commit.BackupSet, filepath, info.MtimeNs, info.Size, info.Mode, node.Ref.Hash)

		return sqlErr
	})
	if errors.Is(err, backup.ErrNotFound) {
		if strict {
			return fmt.Errorf("%w: %v", backup.ErrDanglingRef, err)
		}

		log.Printf("Ignoring commit %x, failed traversing tree: %s", ref.Hash, err)
		return nil
	}

	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO commits (backup_set, ref, timestamp, tree) VALUES (?, ?, ?, ?)", commit.BackupSet, ref.Hash, commit.Timestamp, commit.Tree.Hash)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Index) Put(ctx context.Context, object *proto.Object) error {
	err := backup.CheckReferences(ctx, s.ObjectStore, object)
	if err != nil {
		return err
	}

	err = s.ObjectStore.Put(ctx, object)
	if err != nil {
		return err
	}

	switch object.Type() {
	case proto.ObjectType_COMMIT:
		return s.index(ctx, object.GetCommit(), object.Ref(), true)
	}

	return nil
}

func (s *Index) FileInfo(ctx context.Context, set string, name string, notAfter time.Time, count int) ([]*proto.TreeNode, error) {
	infoList := make([]*proto.TreeNode, 0, count)

	rows, err := s.db.QueryContext(ctx, "SELECT path, timestamp, size, mode, ref FROM files WHERE backup_set = ? AND path = ? AND timestamp <= ? ORDER BY timestamp DESC LIMIT ?;", set, name, notAfter.UnixNano(), count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		ref := &proto.Ref{}

		info := &proto.FileInfo{}

		err = rows.Scan(&info.Name, &info.MtimeNs, &info.Size, &info.Mode, &ref.Hash)
		if err != nil {
			return nil, err
		}

		info.Name = filepath.Base(info.Name)

		infoList = append(infoList, &proto.TreeNode{
			Stat: info,
			Ref:  ref,
		})
	}

	return infoList, rows.Err()
}

func (s *Index) CommitInfo(ctx context.Context, set string, notAfter time.Time, count int) ([]*proto.Commit, error) {
	infoList := make([]*proto.Commit, 0, count)

	rows, err := s.db.QueryContext(ctx, "SELECT timestamp, tree FROM commits WHERE backup_set = ? AND timestamp <= ? ORDER BY timestamp DESC LIMIT ?;", set, notAfter.UTC().Unix(), count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		commit := &proto.Commit{BackupSet: set}
		tree := proto.Ref{}

		err = rows.Scan(&commit.Timestamp, &tree.Hash)
		if err != nil {
			return nil, err
		}

		commit.Tree = &tree

		infoList = append(infoList, commit)
	}

	return infoList, rows.Err()
}
