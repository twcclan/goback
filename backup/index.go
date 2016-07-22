package backup

import (
	"time"

	"github.com/twcclan/goback/proto"
)

type Index interface {
	Open() error
	Close() error

	ObjectStore
	FileInfo(name string, notAfter time.Time, count int) ([]proto.TreeNode, error)
	/*
		// chunk related methods
		ReIndex(ObjectStore) error
		Index(*proto.Object) error
		HasChunk(*proto.Ref) (bool, error)

		SnapshotInfo(notAfter time.Time, count int) ([]SnapshotPointer, error)

	*/
}

type SnapshotPointer struct {
	Info *proto.Commit
	Ref  *proto.Ref
}
