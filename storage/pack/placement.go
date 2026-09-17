package pack

import (
	"context"
	"strings"
	"time"

	"github.com/twcclan/goback/backup"
)

// Placement is the decoded prefix of an archive name: the session that
// wrote it and has not committed (<session>/<id>), or none for a committed
// archive at the root.
type Placement struct {
	Session string
}

// ParsePlacement decodes the prefix of an archive name.
func ParsePlacement(name string) Placement {
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return Placement{}
	}

	return Placement{Session: parts[0]}
}

// Dir is the prefix under which archives of this placement are written.
func (p Placement) Dir() string {
	return p.Session
}

// sessionPlacement is where a session's archives are written.
func sessionPlacement(s *backup.Session) Placement {
	return Placement{Session: s.ID}
}

// ArchiveState is the visibility of an archive: committed archives are
// served to everyone, pending ones only to the session that wrote them.
type ArchiveState uint8

const (
	ArchiveCommitted ArchiveState = iota
	ArchivePending
)

// ArchiveInfo is what the archive index records about an archive.
type ArchiveInfo struct {
	Name    string
	Session string
	State   ArchiveState
}

// Scope filters LocateObject: the committed archives plus the pending
// archives of Session.
type Scope struct {
	Session string
}

// Visible reports whether an archive is served to this scope.
func (s Scope) Visible(a ArchiveInfo) bool {
	if a.State == ArchivePending {
		return s.Session != "" && a.Session == s.Session
	}

	return true
}

// ScopeOf is the scope of a context: the session it carries, else the
// committed archives alone.
func ScopeOf(ctx context.Context) Scope {
	if s, ok := backup.SessionFromContext(ctx); ok {
		return Scope{Session: s.ID}
	}

	return Scope{}
}

// SessionIndex keeps the live sessions of a store, so any node can tell a
// session id from a stale one and a janitor can expire the dead.
type SessionIndex interface {
	BeginSession(s *backup.Session) error
	TouchSession(id string, at time.Time) error
	GetSession(id string) (*backup.Session, error)
	ListSessions() ([]*backup.Session, error)
	// EndSession drops the session and its pending archives, returning
	// the names of the archives dropped.
	EndSession(id string) ([]string, error)
	// CommitSession flips the session's pending archives to committed.
	CommitSession(id string) error
}
