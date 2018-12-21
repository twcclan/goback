package backup

import (
	"context"
	"time"

	"github.com/twcclan/goback/proto"
)

type Index interface {
	Open() error
	Close() error

	ObjectStore
	FileInfo(ctx context.Context, name string, notAfter time.Time, count int) ([]proto.TreeNode, error)
	CommitInfo(ctx context.Context, notAfter time.Time, count int) ([]proto.Commit, error)
	ReIndex(ctx context.Context) error
}
