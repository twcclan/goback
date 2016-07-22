package index

import (
	"database/sql"
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

//go:generate go-bindata -pkg backup sql/
type SqliteIndex struct {
	backup.ObjectStore
	base   string
	db     *sql.DB
	single *singleflight.Group
	txMtx  *sync.Mutex
}

// TODO: use transactions for better write performance

func (s *SqliteIndex) Open() error {
	db, err := sql.Open("sqlite3", path.Join(s.base, "index.db"))

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

func (s *SqliteIndex) Close() error {
	return s.db.Close()
}

func (s *SqliteIndex) traverseTree(p string, tree *proto.Object) error {

	if ok, err := s.HasIndexed(tree.Ref()); ok || err != nil {
		return err
	}

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

			err = s.traverseTree(path.Join(p, info.Name), subTree)
			if err != nil {
				return err
			}
		} else {
			// store the relative path to this file in the index
			_, err := s.db.Exec("INSERT INTO fileInfo(path, timestamp, size, mode, ref) VALUES(?,?,?,?,?)",
				path.Join(p, info.Name), info.Timestamp, info.Size, info.Mode, node.Ref.Sha1)

			if err != nil {
				return err
			}

			// mark file object indexed
			err = s.markIndexed(node.Ref)
			if err != nil {
				return err
			}
		}
	}

	// mark tree indexed
	return s.markIndexed(tree.Ref())
}

func (s *SqliteIndex) Put(object *proto.Object) error {
	// store the object first
	err := s.ObjectStore.Put(object)
	if err != nil {
		return err
	}

	s.txMtx.Lock()
	defer s.txMtx.Unlock()

	if ok, err := s.HasObject(object.Ref()); ok || err != nil {
		return err
	}

	_, err = s.db.Exec("INSERT INTO objects (ref, type, indexed) VALUES(?, ?, ?)", object.Ref().Sha1, object.Type(), false)
	if err != nil {
		return errors.Wrap(err, "Failed to create object index")
	}

	switch object.Type() {
	case proto.ObjectType_COMMIT:

		commit := object.GetCommit()
		// whenever we get to index a commit
		// we'll traverse the complete backup tree
		// to create our filesystem path index

		treeObj, err := s.ObjectStore.Get(commit.Tree)
		if err != nil {
			return err
		}

		if treeObj == nil {
			return errors.Errorf("Root tree %x could not be retrieved", commit.Tree.Sha1)
		}

		err = s.traverseTree("", treeObj)
		if err != nil {
			return err
		}

		// mark commit indexed

		err = s.markIndexed(object.Ref())
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *SqliteIndex) HasObject(ref *proto.Ref) (bool, error) {
	return s.hasRow("SELECT 1 FROM objects WHERE ref = ? LIMIT 1;", ref.Sha1)
}

func (s *SqliteIndex) HasIndexed(ref *proto.Ref) (bool, error) {
	return s.hasRow("SELECT 1 FROM objects WHERE ref = ? AND indexed = 1", ref.Sha1)
}

func (s *SqliteIndex) markIndexed(ref *proto.Ref) error {
	_, err := s.db.Exec("UPDATE objects SET indexed = 1 WHERE ref = ?", ref.Sha1)
	return err
}

func (s *SqliteIndex) hasRow(query string, params ...interface{}) (bool, error) {
	var unused int

	err := s.db.QueryRow(query, params...).Scan(&unused)

	if err == sql.ErrNoRows {
		return false, nil
	}

	return err == nil, err
}

func (s *SqliteIndex) FileInfo(name string, notAfter time.Time, count int) ([]proto.TreeNode, error) {
	infoList := make([]proto.TreeNode, count)

	rows, err := s.db.Query("SELECT path, timestamp, size, mode, ref FROM fileInfo WHERE path = ? AND timestamp <= ? ORDER BY timestamp DESC LIMIT ?;", name, notAfter.Unix(), count)
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

/*
func (s *SqliteIndex) index(chunk *proto.Blob, tx *sql.Tx) (err error) {
	switch chunk.Ref.Type {
	case proto.ChunkType_FILE_INFO:
		info := new(proto.FileInfo)
		proto.ReadMetaChunk(chunk, info)

		_, err = tx.Exec("INSERT INTO fileInfo(name, timestamp, chunk, data, size, mode) VALUES(?, ?, ?, ?, ?, ?);",
			info.Name, info.Timestamp, chunk.Ref.Sum, info.Data.Sum, info.Size, info.Mode)
		if err != nil {
			return
		}

	case proto.ChunkType_SNAPSHOT_INFO:
		snapshot := new(proto.SnapshotInfo)
		proto.ReadMetaChunk(chunk, snapshot)

		_, err = tx.Exec("INSERT INTO snapshotInfo(timestamp, data, chunk) VALUES(?, ?, ?);", snapshot.Timestamp, snapshot.Data.Sum, chunk.Ref.Sum)
		if err != nil {
			return
		}
	}

	_, err = tx.Exec("INSERT INTO chunks(sum) VALUES(?);", chunk.Ref.Sum)

	return
}

func (s *SqliteIndex) ReIndex(store ChunkStore) (err error) {
	types := []proto.ChunkType{
		proto.ChunkType_DATA,
		proto.ChunkType_FILE,
		proto.ChunkType_FILE_INFO,
		proto.ChunkType_SNAPSHOT,
		proto.ChunkType_SNAPSHOT_INFO,
	}

	tx, err := s.db.Begin()
	if err != nil {
		return
	}

	// commit or rollback transaction depending return value
	defer func() {
		if err != nil {
			tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	for _, t := range types {
		err = store.Walk(t, func(ref *proto.ChunkRef) error {

			chunk, e := store.Read(ref)
			if e != nil {
				return err
			}

			return s.index(chunk, tx)
		})

		if err != nil {
			tx.Rollback()
			return err
		}
	}

	return
}

func (s *SqliteIndex) Index(chunk *proto.Object) (err error) {
	s.txMtx.Lock()
	defer s.txMtx.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	// commit or rollback transaction depending return value
	defer func() {
		if err != nil {
			tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	err = s.index(chunk, tx)

	return
}

func (s *SqliteIndex) SnapshotInfo(notAfter time.Time, count int) ([]SnapshotPointer, error) {
	infoList := make([]SnapshotPointer, count)

	rows, err := s.db.Query("SELECT timestamp, chunk, data FROM snapshotInfo WHERE timestamp <= ? ORDER BY timestamp DESC LIMIT ?;", notAfter.UTC().Unix(), count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counter := 0
	for rows.Next() {

		dataRef := &proto.ChunkRef{Type: proto.ChunkType_SNAPSHOT}
		infoRef := &proto.ChunkRef{Type: proto.ChunkType_SNAPSHOT_INFO}

		info := &proto.SnapshotInfo{
			Data: dataRef,
		}

		err = rows.Scan(&info.Timestamp, &infoRef.Sum, &dataRef.Sum)
		if err != nil {
			return nil, err
		}

		infoList[counter] = SnapshotPointer{
			Info: info,
			Ref:  infoRef,
		}

		counter++
	}

	return infoList[:counter], rows.Err()
}
*/
