package index

import (
	"database/sql"
	"log"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/pkg/errors"
	"go4.org/syncutil/singleflight"

	// load sqlite3 driver
	_ "github.com/mattn/go-sqlite3"
)

func NewSqliteIndex(base string, store backup.ObjectStore) *SqliteIndex {
	idx := &SqliteIndex{base: base, txMtx: new(sync.Mutex), ObjectStore: store, single: new(singleflight.Group)}
	return idx
}

var _ backup.Index = (*SqliteIndex)(nil)

//go:generate go-bindata -pkg index sql/
type SqliteIndex struct {
	backup.ObjectStore
	base   string
	db     *sql.DB
	single *singleflight.Group
	txMtx  *sync.Mutex
}

// TODO: use transactions for better write performance

func (s *SqliteIndex) Open() error {
	db, err := sql.Open("sqlite3", path.Join(s.base, "index.db")+"?busy_timeout=1000")

	if err != nil {
		return err
	}

	err = db.Ping()
	if err != nil {
		return err
	}

	sqlFiles, err := AssetDir("sql")
	if err != nil {
		return err
	}

	for _, file := range sqlFiles {
		queries := MustAsset("sql/" + file)

		_, err := db.Exec(string(queries))
		if err != nil {
			return err
		}
	}

	s.db = db
	return nil
}

func (s *SqliteIndex) FindMissing() error {
	refs := make([][]byte, 0)
	rows, err := s.db.Query("SELECT ref FROM objects;")
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

		obj, err := s.ObjectStore.Get(&proto.Ref{Sha1: ref})
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

func (s *SqliteIndex) Close() error {
	return s.db.Close()
}

func (s *SqliteIndex) ReIndex() error {
	return s.ObjectStore.Walk(true, proto.ObjectType_COMMIT, func(hdr *proto.ObjectHeader, obj *proto.Object) error {
		return s.index(obj.GetCommit())
	})
}

func (s *SqliteIndex) traverseTree(stmt *sql.Stmt, p string, tree *proto.Object) error {
	for _, node := range tree.GetTree().GetNodes() {
		info := node.Stat

		if info.Tree {
			// retrieve the sub-tree object
			subTree, err := s.ObjectStore.Get(node.Ref)

			if err != nil {
				return err
			}

			if subTree == nil {
				return errors.Errorf("Sub tree %x could not be retrieved", node.Ref.Sha1)
			}

			err = s.traverseTree(stmt, path.Join(p, info.Name), subTree)
			if err != nil {
				return err
			}
		} else {
			// store the relative path to this file in the index
			_, err := stmt.Exec(path.Join(p, info.Name), info.Timestamp, info.Size, info.Mode, node.Ref.Sha1)

			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *SqliteIndex) index(commit *proto.Commit) error {
	log.Printf("Indexing commit: %v", time.Unix(commit.Timestamp, 0))

	// whenever we get to index a commit
	// we'll traverse the complete backup tree
	// to create our filesystem path index

	treeObj, err := s.ObjectStore.Get(commit.Tree)
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

	stmt, err := tx.Prepare("INSERT OR IGNORE INTO files(path, timestamp, size, mode, ref) VALUES(?,?,?,?,?)")
	if err != nil {
		return nil
	}

	err = s.traverseTree(stmt, "", treeObj)
	if err != nil {
		log.Printf("Failed building tree for commit, skipping: %v", err)

		return tx.Rollback()
	}

	_, err = tx.Exec("INSERT OR IGNORE INTO commits (timestamp, tree) VALUES (?, ?)", commit.Timestamp, commit.Tree.Sha1)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *SqliteIndex) Put(object *proto.Object) error {
	// store the object first
	err := s.ObjectStore.Put(object)
	if err != nil {
		return err
	}

	switch object.Type() {
	case proto.ObjectType_COMMIT:
		return s.index(object.GetCommit())
	}

	return nil
}

func (s *SqliteIndex) FileInfo(name string, notAfter time.Time, count int) ([]proto.TreeNode, error) {
	infoList := make([]proto.TreeNode, count)

	rows, err := s.db.Query("SELECT path, timestamp, size, mode, ref FROM files WHERE path = ? AND timestamp <= ? ORDER BY timestamp DESC LIMIT ?;", name, notAfter.Unix(), count)
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

		infoList[counter] = proto.TreeNode{
			Stat: info,
			Ref:  ref,
		}

		counter++
	}

	return infoList[:counter], rows.Err()
}

func (s *SqliteIndex) CommitInfo(notAfter time.Time, count int) ([]proto.Commit, error) {
	infoList := make([]proto.Commit, count)

	rows, err := s.db.Query("SELECT timestamp, tree FROM commits WHERE timestamp <= ? ORDER BY timestamp DESC LIMIT ?;", notAfter.UTC().Unix(), count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counter := 0
	for rows.Next() {

		commit := proto.Commit{}
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
