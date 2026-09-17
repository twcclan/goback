package pack

import (
	"context"
	"strings"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

const placementPublic = "public"

// PlacementKind says which prefix an archive lives under.
type PlacementKind int

const (
	// PlacementRoot is a committed archive at the root of the storage.
	PlacementRoot PlacementKind = iota
	// PlacementSession is an archive a session wrote and has not committed: <session>/<id>.
	PlacementSession
	// PlacementPublic holds convergent blobs: public/<id>.
	PlacementPublic
)

// Placement is the decoded prefix of an archive name.
type Placement struct {
	Kind    PlacementKind
	Session string
}

// ParsePlacement decodes the prefix of an archive name.
func ParsePlacement(name string) Placement {
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return Placement{Kind: PlacementRoot}
	}

	if parts[0] == placementPublic {
		return Placement{Kind: PlacementPublic}
	}

	return Placement{Kind: PlacementSession, Session: parts[0]}
}

// Group is the prefix compaction rewrites into: the root for a session's
// archives, otherwise the placement itself.
func (p Placement) Group() Placement {
	if p.Kind == PlacementSession {
		return Placement{Kind: PlacementRoot}
	}

	return Placement{Kind: p.Kind}
}

// Dir is the prefix under which archives of this placement are written.
func (p Placement) Dir() string {
	switch p.Kind {
	case PlacementSession:
		return p.Session
	case PlacementPublic:
		return placementPublic
	}

	return ""
}

// sessionPlacement is where a session's archives are written.
func sessionPlacement(s *backup.Session) Placement {
	return Placement{Kind: PlacementSession, Session: s.ID}
}

// destination picks the group an object is rewritten into: a convergent
// blob goes to the public prefix, everything else stays in its group.
func destination(group Placement, hdr *proto.ObjectHeader) Placement {
	if hdr.GetType() == proto.ObjectType_BLOB && hdr.GetEncryption() == proto.Encryption_CONVERGENT {
		return Placement{Kind: PlacementPublic}
	}

	return group
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
