package backup

import (
	"database/sql"
	"path"
	"sync"
	"time"

	"github.com/twcclan/goback/proto"

	// load sqlite3 driver
	_ "github.com/mattn/go-sqlite3"
)

type Index interface {
	Open() error
	Close() error

	// chunk related methods
	ReIndex(ChunkStore) error
	Index(*proto.Chunk) error
	HasChunk(*proto.ChunkRef) (bool, error)

	SnapshotInfo(notAfter time.Time, count int) ([]SnapshotPointer, error)

	FileInfo(name string, notAfter time.Time, count int) ([]FilePointer, error)
}

type SnapshotPointer struct {
	Info *proto.SnapshotInfo
	Ref  *proto.ChunkRef
}

type FilePointer struct {
	Info *proto.FileInfo
	Ref  *proto.ChunkRef
}

func NewSqliteIndex(base string) *SqliteIndex {
	idx := &SqliteIndex{base: base, txMtx: new(sync.Mutex)}
	return idx
}

var _ Index = (*SqliteIndex)(nil)

//go:generate go-bindata -pkg backup sql/
type SqliteIndex struct {
	base  string
	db    *sql.DB
	txMtx *sync.Mutex
}

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

func (s *SqliteIndex) index(chunk *proto.Chunk, tx *sql.Tx) (err error) {
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

func (s *SqliteIndex) Index(chunk *proto.Chunk) (err error) {
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

func (s *SqliteIndex) HasChunk(ref *proto.ChunkRef) (bool, error) {
	s.txMtx.Lock()
	defer s.txMtx.Unlock()

	var unused int

	err := s.db.QueryRow("SELECT 1 FROM chunks WHERE sum = ? LIMIT 1;", ref.Sum).Scan(&unused)

	if err == sql.ErrNoRows {
		return false, nil
	}

	return err == nil, err
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

func (s *SqliteIndex) FileInfo(name string, notAfter time.Time, count int) ([]FilePointer, error) {
	infoList := make([]FilePointer, count)

	rows, err := s.db.Query("SELECT name, timestamp, size, mode, data, chunk FROM fileInfo WHERE name = ? AND timestamp <= ? ORDER BY timestamp DESC LIMIT ?;", name, notAfter.UTC().Unix(), count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counter := 0
	for rows.Next() {

		dataRef := &proto.ChunkRef{Type: proto.ChunkType_FILE}
		infoRef := &proto.ChunkRef{Type: proto.ChunkType_FILE_INFO}

		info := &proto.FileInfo{
			Data: dataRef,
		}

		err = rows.Scan(&info.Name, &info.Timestamp, &info.Size, &info.Mode, &dataRef.Sum, &infoRef.Sum)
		if err != nil {
			return nil, err
		}

		infoList[counter] = FilePointer{
			Info: info,
			Ref:  infoRef,
		}

		counter++
	}

	return infoList[:counter], rows.Err()
}
