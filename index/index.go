// Package index holds what a store index reports about its sets and the
// store policy, as the admin surface shows them.
package index

import (
	"time"

	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"
)

// Set states.
const (
	// SetActive accepts commits.
	SetActive = "active"
	// SetClosing was deleted and keeps its commits through the trash window.
	SetClosing = "closing"
	// SetDeleted has no live commits left.
	SetDeleted = "deleted"
)

// SetInfo is a set with the size of its newest live commit, the sizes of
// all its live commits added up, and, as of the last garbage collection, what its objects take up in the store and the
// size of the distinct content they carry before compression.
type SetInfo struct {
	ID               int64
	Name             string
	State            string
	LogicalSize      int64
	KeptLogicalSize  int64
	PhysicalSize     int64
	DeduplicatedSize int64
	// UniqueSize is the size of every distinct file version an untombstoned
	// commit of the set holds, each counted once: what storing each version
	// of each file once would take. Deleted commits still restorable count.
	UniqueSize int64
	// AloneSize is what the set would take up were it the only set, and
	// ExclusiveSize what of that no other set holds, as of the last
	// garbage collection.
	AloneSize, ExclusiveSize int64
}

// SetQuery picks and orders a page of sets. The zero SetQuery lists
// every set by name.
type SetQuery struct {
	// States keeps the sets in one of these states; none keeps all.
	States []string
	// Match keeps the sets whose name holds it, ignoring case, and those
	// Named lists; an empty Match keeps every set.
	Match string
	Named []string
	// BySize lists only the sets a garbage collection measured, largest
	// PhysicalSize first and by name within a size, instead of by name.
	BySize bool
	// After and AfterSize are the Name and PhysicalSize of the last set of
	// the previous page; AfterSize counts only with BySize. An empty After
	// starts at the first set.
	After     string
	AfterSize int64
	// Limit is the most sets a page holds; zero holds every one.
	Limit int
}

// StorePolicy is the store's write policy with its acknowledgement state.
type StorePolicy struct {
	Policy            storekey.Policy
	KeyAcknowledgedAt *time.Time
}

// SetRetention is a set's retention as an operator sees it: the policy
// set on it, nil when it inherits the store's, the policy in force, and
// whether retirement waits for a policy after a rebuild.
type SetRetention struct {
	Policy    *retention.Policy
	Effective retention.Policy
	Paused    bool
}

// Windows are the store's trash window in days: how long a commit deleted
// by hand waits before its tombstone.
type Windows struct {
	TrashDays int
}

// Version is one version of a path with From, when the store received
// the commit that first held it. No two versions of a path share a From.
type Version struct {
	Node *proto.TreeNode
	From time.Time
}

// CommitDetail is a commit together with what the index knows about it
// beyond the stored object: how big the set was when it was taken, and
// which retention rule is keeping it.
type CommitDetail struct {
	Commit *proto.Commit
	// Ref is the commit object's own ref, which names it to a caller that
	// wants to pin or read it.
	Ref *proto.Ref
	// LogicalSize is what the set's files held at this commit, and Files
	// how many there were; nil when nothing measured it, which is every
	// commit written before the index started recording it.
	LogicalSize, Files *int64
	// RetainedBy names the retention rules keeping this commit, comma
	// separated: "last", "within", "pinned", "hourly", "daily", "weekly",
	// "monthly". It is empty for a commit retention has not evaluated yet
	// or has retired.
	RetainedBy string
	// Incomplete reports a commit indexed around objects the store no
	// longer holds, which cannot be restored whole.
	Incomplete bool
}
