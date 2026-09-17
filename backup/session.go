package backup

import (
	"context"
	"errors"
	"time"

	"github.com/twcclan/goback/proto"
)

// Session is one backup or restore run of an agent against a set. Objects
// written under a session are visible only to that session until a commit.
type Session struct {
	ID      string
	AgentID string
	Set     string
	// Restore names the object a restore session reads; retirement and
	// garbage collection keep it and everything it names while the session
	// lives.
	Restore  *proto.Ref
	Started  time.Time
	LastSeen time.Time
}

// RestoreLeaser reports the refs that live restore sessions are reading.
type RestoreLeaser interface {
	RestoreLeases(ctx context.Context) ([]*proto.Ref, error)
}

// ErrNoSession is returned when a session id names no live session.
var ErrNoSession = errors.New("no such session")

type sessionKey struct{}

// WithSession attaches the session to the context.
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionFromContext returns the request's session, if any.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(*Session)
	return s, ok && s != nil
}

// SessionStore opens and closes backup sessions. BeginSession fills in the
// session's id and returns a context carrying it; every Put and Get made
// with that context belongs to the session. EndSession drops whatever the
// session has not committed.
type SessionStore interface {
	BeginSession(ctx context.Context, s *Session) (context.Context, error)
	EndSession(ctx context.Context) error
}

// Leased is a session store that ends sessions after a lease without a
// write.
type Leased interface {
	SessionLease() time.Duration
}
