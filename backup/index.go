package backup

import (
	"context"
	"time"

	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"
)

// An Index stamps a commit or pin it receives with the receipt time, and a
// commit with its set id, before storing it; the caller reads the ref off
// the stamped object afterwards. An object that already carries a receipt
// time is a replay and is stored as it is.
type Index interface {
	Open() error
	Close() error

	ObjectStore
	FileInfo(ctx context.Context, set string, name string, notAfter time.Time, count int) ([]*proto.TreeNode, error)
	CommitInfo(ctx context.Context, set string, notAfter time.Time, count int) ([]*proto.Commit, error)
	// LatestCommit returns the ref of the set's newest commit, or ErrNotFound.
	LatestCommit(ctx context.Context, set string) (*proto.Ref, error)
	ReIndex(ctx context.Context) error
}

// HeaderWalker visits object headers without loading bodies. Tombstones
// have no body and are only reachable this way.
type HeaderWalker interface {
	WalkHeaders(ctx context.Context, t proto.ObjectType, fn func(*proto.ObjectHeader) error) error
}

// RefScope is implemented by indexes that know which commit, tree and
// file refs the store's sets reach.
type RefScope interface {
	// References reports whether a set of the store references ref.
	References(ctx context.Context, ref *proto.Ref) (bool, error)
}

// CommitGrant is the answer to an allowed BeginCommit.
type CommitGrant struct {
	// SetID is the set's server-assigned id, which the commit body must carry.
	SetID uint64
	// Policy is the store's write policy for this run; nil when the server
	// holds none, which leaves the agent's key file in charge.
	Policy *storekey.Policy
}

// CommitGate is asked before a run whether the caller may commit to a set.
type CommitGate interface {
	BeginCommit(ctx context.Context, set string) (*CommitGrant, error)
}

// PartReader streams the stored objects of a file's parts, in order,
// skipping the given part indexes; the objects are as uploaded, sealed
// when the store is encrypted.
type PartReader interface {
	ReadParts(ctx context.Context, file *proto.Ref, skip []int, fn func(index int, obj *proto.Object) error) error
}

// Retention is implemented by indexes that keep the lifecycle of docs/09:
// a commit is live, then retired for a hold window, then tombstoned.
type Retention interface {
	// DeleteCommit retires a commit into its trash window; the newest
	// live commit of a set is refused with ErrNewestCommit.
	DeleteCommit(ctx context.Context, ref *proto.Ref) error
	// UndeleteCommit moves a retired commit back to live; a tombstoned
	// one is refused with ErrTombstoned.
	UndeleteCommit(ctx context.Context, ref *proto.Ref) error
	// DeleteSet closes a set and retires every commit; erase uses a zero
	// window.
	DeleteSet(ctx context.Context, set string, erase bool) error
	// UndeleteSet reopens a closed set and revives its commits.
	UndeleteSet(ctx context.Context, set string) error
	// Unpin tombstones a pin.
	Unpin(ctx context.Context, pin *proto.Ref) error
	// Pins lists the live pins.
	Pins(ctx context.Context) ([]*proto.PinInfo, error)
}

// Retirer runs the retirement job: every retired commit past its window
// gets a tombstone and loses its index rows.
type Retirer interface {
	Retire(ctx context.Context, now time.Time) (int, error)
}
