package backup

import (
	"database/sql"
	"path"

	"github.com/twcclan/goback/proto"

	_ "github.com/mattn/go-sqlite3"
)

type Index interface {
	Open() error
	Close() error

	ReIndex(ChunkStore) error
	Index(*proto.Chunk) error

	HasChunk(*proto.ChunkRef) (bool, error)

	Stat(string) (*proto.ChunkRef, *proto.FileInfo, error)
}

func NewSqliteIndex(base string) *SqliteIndex {
	idx := &SqliteIndex{base: base}
	return idx
}

var _ Index = (*SqliteIndex)(nil)

//go:generate go-bindata -pkg backup sql/
type SqliteIndex struct {
	base string
	db   *sql.DB
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

		_, err = tx.Exec("INSERT INTO fileinfo(name, mod, chunk) VALUES(?, ?, ?);", info.Name, info.Timestamp, chunk.Ref.Sum)
		if err != nil {
			return
		}

	case proto.ChunkType_SNAPSHOT:
		snapshot := new(proto.Snapshot)
		proto.ReadMetaChunk(chunk, snapshot)

		_, err = tx.Exec("INSERT INTO snapshots(time, chunk) VALUES(?, ?);", snapshot.Timestamp, chunk.Ref.Sum)
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
		proto.ChunkType_FILE_DATA,
		proto.ChunkType_FILE_INFO,
		proto.ChunkType_SNAPSHOT,
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
	var unused int

	err := s.db.QueryRow("SELECT 1 FROM chunks WHERE sum = ? LIMIT 1;", ref.Sum).Scan(&unused)

	if err == sql.ErrNoRows {
		return false, nil
	}

	return err == nil, err
}

func (s *SqliteIndex) Stat(name string) (*proto.ChunkRef, *proto.FileInfo, error) {
	info := new(proto.FileInfo)
	ref := &proto.ChunkRef{Type: proto.ChunkType_FILE_INFO}
	err := s.db.QueryRow("SELECT name, mod, chunk FROM fileinfo WHERE name = ? ORDER BY mod DESC LIMIT 1;", name).Scan(&info.Name, &info.Timestamp, &ref.Sum)

	if err == sql.ErrNoRows {
		return nil, nil, nil
	}

	return ref, info, err
}
