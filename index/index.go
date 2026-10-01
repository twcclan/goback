// Package index holds what a store index reports about its sets and the
// store policy, as the admin surface shows them.
package index

import (
	"time"

	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/backup/storekey"
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

// SetInfo is a set with the size of its newest live commit and, as of the
// last garbage collection, what its objects take up in the store and the
// size of the distinct content they carry before compression.
type SetInfo struct {
	ID               int64
	Name             string
	State            string
	LogicalSize      int64
	PhysicalSize     int64
	DeduplicatedSize int64
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

// Windows are the store's hold and trash windows in days: how long a
// commit retired by policy, or deleted by hand, waits before its
// tombstone.
type Windows struct {
	HoldDays  int
	TrashDays int
}
