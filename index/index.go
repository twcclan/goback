// Package index holds what a store index reports about its sets and the
// store policy, as the admin surface shows them.
package index

import (
	"time"

	"github.com/twcclan/goback/backup/storekey"
)

// Set states.
const (
	SetActive  = "active"
	SetClosing = "closing"
	SetDeleted = "deleted"
)

// SetInfo is a set with the size of its newest live commit.
type SetInfo struct {
	ID            int64
	Name, AgentID string
	State         string
	LogicalSize   int64
}

// StorePolicy is the store's write policy with its acknowledgement state.
type StorePolicy struct {
	Policy            storekey.Policy
	KeyAcknowledgedAt *time.Time
}
