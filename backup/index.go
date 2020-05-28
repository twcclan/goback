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
	FileInfo(ctx context.Context, set string, name string, notAfter time.Time, count int) ([]*proto.TreeNode, error)
	CommitInfo(ctx context.Context, set string, notAfter time.Time, count int) ([]*proto.Commit, error)
	ReIndex(ctx context.Context) error
}
