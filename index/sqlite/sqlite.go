package sqlite

import (
	"context"
	"database/sql"
	"embed"
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

func (s *Index) ReachableCommits(ctx context.Context, f func(commit *proto.Commit) error) error {
	panic("implement me")
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

	s.db = db
	return nil
}

func (s *Index) FindMissing(ctx context.Context) error {
	refs := make([][]byte, 0)
	rows, err := s.db.QueryContext(ctx, "SELECT ref FROM objects;")
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

		obj, err := s.ObjectStore.Get(ctx, &proto.Ref{Sha1: ref})
		if err != nil {
			return err
		}

		if obj == nil {
			log.Printf("Found missing object %x", ref)
			refs = append(refs, ref)
		}
	}

	rows.Close()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Commit()

	log.Printf("Deleting %d missing objects", len(refs))
	for _, ref := range refs {
		_, err := tx.Exec("DELETE FROM objects where ref = ?;", ref)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Index) Close() error {
	return s.db.Close()
}

func (s *Index) ReIndex(ctx context.Context) error {
	return s.ObjectStore.Walk(ctx, true, proto.ObjectType_COMMIT, func(obj *proto.Object) error {
		if obj.GetCommit().GetBackupSet() != s.backupSet {
			return nil
		}

		return s.index(ctx, obj.GetCommit())
	})
}

func (s *Index) index(ctx context.Context, commit *proto.Commit) error {
	log.Printf("Indexing commit: %v", time.Unix(commit.Timestamp, 0))

	// whenever we get to index a commit
	// we'll traverse the complete backup tree
	// to create our filesystem path index

	treeObj, err := s.ObjectStore.Get(ctx, commit.Tree)
	if err != nil {
		return err
	}

	if treeObj == nil {
		log.Printf("Root tree %x could not be retrieved", commit.Tree.Sha1)
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, "INSERT OR IGNORE INTO files(path, timestamp, size, mode, ref) VALUES(?,?,?,?,?)")
	if err != nil {
		return nil
	}

	err = backup.TraverseTree(ctx, s.ObjectStore, treeObj, 64, func(filepath string, node *proto.TreeNode) error {
		info := node.Stat
		if info.Tree {
			return nil
		}

		// store the relative path to this file in the index
		_, sqlErr := stmt.ExecContext(ctx, filepath, info.Timestamp, info.Size, info.Mode, node.Ref.Sha1)

		return sqlErr
	})
	if err != nil {
		log.Printf("Failed building tree for commit, skipping: %v", err)

		return tx.Rollback()
	}

	_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO commits (timestamp, tree) VALUES (?, ?)", commit.Timestamp, commit.Tree.Sha1)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Index) Put(ctx context.Context, object *proto.Object) error {
	// store the object first
	err := s.ObjectStore.Put(ctx, object)
	if err != nil {
		return err
	}

	switch object.Type() {
	case proto.ObjectType_COMMIT:
		return s.index(ctx, object.GetCommit())
	}

	return nil
}

func (s *Index) FileInfo(ctx context.Context, set string, name string, notAfter time.Time, count int) ([]*proto.TreeNode, error) {
	infoList := make([]*proto.TreeNode, count)

	rows, err := s.db.QueryContext(ctx, "SELECT path, timestamp, size, mode, ref FROM files WHERE path = ? AND timestamp <= ? ORDER BY timestamp DESC LIMIT ?;", name, notAfter.Unix(), count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counter := 0
	for rows.Next() {
		ref := &proto.Ref{}

		info := &proto.FileInfo{}

		err = rows.Scan(&info.Name, &info.Timestamp, &info.Size, &info.Mode, &ref.Sha1)
		if err != nil {
			return nil, err
		}

		info.Name = filepath.Base(info.Name)

		infoList[counter] = &proto.TreeNode{
			Stat: info,
			Ref:  ref,
		}

		counter++
	}

	return infoList[:counter], rows.Err()
}

func (s *Index) CommitInfo(ctx context.Context, set string, notAfter time.Time, count int) ([]*proto.Commit, error) {
	infoList := make([]*proto.Commit, count)

	rows, err := s.db.QueryContext(ctx, "SELECT timestamp, tree FROM commits WHERE timestamp <= ? ORDER BY timestamp DESC LIMIT ?;", notAfter.UTC().Unix(), count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counter := 0
	for rows.Next() {

		commit := &proto.Commit{}
		tree := proto.Ref{}

		err = rows.Scan(&commit.Timestamp, &tree.Sha1)
		if err != nil {
			return nil, err
		}

		commit.Tree = &tree

		infoList[counter] = commit

		counter++
	}

	return infoList[:counter], rows.Err()
}
