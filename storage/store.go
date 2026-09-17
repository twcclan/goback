package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

// ErrInvalidRequest is a call the store cannot act on as given: a malformed
// ref, an object that fails validation, a ref of the wrong object type.
var ErrInvalidRequest = errors.New("invalid request")

// Store is one store's operations over an index and its sessions: the work
// behind every RPC of proto.Store, with no transport in it. The caller's
// principal and session travel in the context.
type Store struct {
	Index    backup.Index
	Sessions backup.SessionStore

	// Lease is what BeginSession promises the client; the store enforces
	// its own.
	Lease time.Duration

	// PresenceScope says whose commits feed the filters Presence serves
	// when the index has no policy.
	PresenceScope backup.PresenceScope
}

// NewStore returns a Store over index. sessions is nil for a store without
// sessions.
func NewStore(index backup.Index, sessions backup.SessionStore) *Store {
	return &Store{
		Index:         index,
		Sessions:      sessions,
		Lease:         30 * time.Minute,
		PresenceScope: backup.PresenceStore,
	}
}

// BeginCommit asks the index whether the caller may commit to set. An index
// without a gate allows every commit and assigns the set id later; a
// refusal is backup.ErrCommitDenied with the reason.
func (s *Store) BeginCommit(ctx context.Context, set string) (*backup.CommitGrant, error) {
	gate, ok := s.Index.(backup.CommitGate)
	if !ok {
		return &backup.CommitGrant{}, nil
	}

	return gate.BeginCommit(ctx, set)
}

// Upload is one Put: the object, the ref the client computed for it (nil
// for a commit or pin, which the store stamps itself) and the refs a file
// assumes are stored already.
type Upload struct {
	Object  *proto.Object
	Ref     *proto.Ref
	Assumed []*proto.Ref
}

// Receipt answers an Upload: the stored ref, the stamped object for a
// commit or pin, or the assumed refs that are missing, in which case
// nothing was stored.
type Receipt struct {
	Ref     *proto.Ref
	Object  *proto.Object
	Missing []*proto.Ref
}

// Put stores up.Object after recomputing its ref from the received bytes;
// a client ref that does not match is proto.ErrRefMismatch. Commits and
// pins are stamped with the receipt time, and a commit with its set id, so
// their ref is computed here and returned with the object.
func (s *Store) Put(ctx context.Context, up Upload) (*Receipt, error) {
	object := up.Object

	err := object.Validate()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	stamped := object.GetCommit() != nil || object.GetPin() != nil

	if !stamped && up.Ref != nil && !object.Ref().Equal(up.Ref) {
		return nil, fmt.Errorf("%w: client sent %x, server computed %x", proto.ErrRefMismatch, up.Ref.Hash, object.Ref().Hash)
	}

	if stamped {
		p, err := auth.Require(ctx)
		if err != nil {
			return nil, err
		}

		if commit := object.GetCommit(); commit != nil {
			err = p.AuthorizeCommit(commit)
			if err != nil {
				return nil, err
			}
		}

		// whatever the client wrote there, the server decides
		clearStamp(object)
	}

	if s.Sessions != nil {
		if _, ok := backup.SessionFromContext(ctx); !ok {
			return nil, fmt.Errorf("%w: writes need a session", backup.ErrNoSession)
		}
	}

	if len(up.Assumed) > 0 {
		if object.GetFile() == nil {
			return nil, fmt.Errorf("%w: assumed refs are only accepted for file objects", ErrInvalidRequest)
		}

		missing, err := backup.Missing(ctx, s.Index, up.Assumed)
		if err != nil {
			return nil, err
		}

		if len(missing) > 0 {
			return &Receipt{Ref: object.Ref(), Missing: missing}, nil
		}
	}

	err = s.Index.Put(ctx, object)
	if err != nil {
		return nil, err
	}

	receipt := &Receipt{Ref: object.Ref()}
	if stamped {
		receipt.Object = object
	}

	return receipt, nil
}

// clearStamp removes a client-supplied receipt time and set id so the
// index assigns its own.
func clearStamp(object *proto.Object) {
	if commit := object.GetCommit(); commit != nil {
		commit.SetId = 0
		commit.ReceivedAtNs = 0
	}

	if pin := object.GetPin(); pin != nil {
		pin.ReceivedAtNs = 0
	}
}

// Get fetches a commit, tree or file object the caller references.
// Anything else, and anything absent, is backup.ErrNotFound.
func (s *Store) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	if !ref.Valid() {
		return nil, fmt.Errorf("%w: malformed ref", ErrInvalidRequest)
	}

	notFound := fmt.Errorf("object %x: %w", ref.GetHash(), backup.ErrNotFound)

	if scope, ok := s.Index.(backup.RefScope); ok {
		referenced, err := scope.References(ctx, ref)
		if err != nil {
			return nil, err
		}

		if !referenced {
			return nil, notFound
		}
	}

	obj, err := s.Index.Get(ctx, ref)
	if errors.Is(err, backup.ErrNotFound) {
		return nil, notFound
	}

	if err != nil {
		return nil, err
	}

	switch obj.Type() {
	case proto.ObjectType_COMMIT, proto.ObjectType_TREE, proto.ObjectType_FILE:
		return obj, nil
	}

	return nil, notFound
}

// Tree walks the tree at ref breadth-first, its splits, and the trees of
// directories below it down to maxDepth levels, handing each to fn.
func (s *Store) Tree(ctx context.Context, ref *proto.Ref, maxDepth uint32, fn func(*proto.Ref, *proto.Object) error) error {
	type pending struct {
		ref   *proto.Ref
		depth uint32
	}

	root, err := s.Get(ctx, ref)
	if err != nil {
		return err
	}

	queue := []pending{{ref: ref}}
	loaded := map[string]*proto.Object{string(ref.GetHash()): root}

	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]

		obj := loaded[string(next.ref.GetHash())]
		if obj == nil {
			obj, err = s.Index.Get(ctx, next.ref)
			if errors.Is(err, backup.ErrNotFound) {
				return fmt.Errorf("tree %x: %w", next.ref.GetHash(), backup.ErrNotFound)
			}

			if err != nil {
				return err
			}
		}

		tree := obj.GetTree()
		if tree == nil {
			return fmt.Errorf("%w: object %x is not a tree", ErrInvalidRequest, next.ref.GetHash())
		}

		err = fn(next.ref, obj)
		if err != nil {
			return err
		}

		for _, split := range tree.Splits {
			queue = append(queue, pending{ref: split, depth: next.depth})
		}

		if next.depth >= maxDepth {
			continue
		}

		for _, node := range tree.Nodes {
			if node.GetStat().IsDir() {
				queue = append(queue, pending{ref: node.Ref, depth: next.depth + 1})
			}
		}
	}

	return nil
}

// readFileWorkers bounds the part fetches one ReadFile call has in flight.
const readFileWorkers = 16

// ReadFile hands the stored objects of the file's parts to fn in order,
// skipping the part indexes in skip.
func (s *Store) ReadFile(ctx context.Context, ref *proto.Ref, skip []uint32, fn func(int, *proto.Object) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	obj, err := s.Get(ctx, ref)
	if err != nil {
		return err
	}

	if obj.GetFile() == nil {
		return fmt.Errorf("%w: object %x is not a file", ErrInvalidRequest, ref.GetHash())
	}

	parts, err := backup.FileParts(ctx, s.Index, obj.GetFile())
	if err != nil {
		return err
	}

	skipped := make(map[uint32]bool, len(skip))
	for _, i := range skip {
		skipped[i] = true
	}

	type result struct {
		obj *proto.Object
		err error
	}

	type fetch struct {
		index int
		done  chan result
	}

	pending := make(chan fetch, readFileWorkers)

	go func() {
		defer close(pending)

		for i, part := range parts {
			if skipped[uint32(i)] || part.Ref == nil {
				continue
			}

			f := fetch{index: i, done: make(chan result, 1)}
			select {
			case pending <- f:
			case <-ctx.Done():
				return
			}

			go func(ref *proto.Ref) {
				o, err := s.Index.Get(ctx, ref)
				f.done <- result{obj: o, err: err}
			}(part.Ref)
		}
	}()

	for f := range pending {
		res := <-f.done
		if res.err != nil {
			return fmt.Errorf("part %d of file %x: %w", f.index, ref.GetHash(), res.err)
		}

		err = fn(f.index, res.obj)
		if err != nil {
			return err
		}
	}

	return ctx.Err()
}

// Retention is the index's retention state, backup.ErrNotImplemented for
// an index that keeps none.
func (s *Store) Retention() (backup.Retention, error) {
	ret, ok := s.Index.(backup.Retention)
	if !ok {
		return nil, fmt.Errorf("%w: index %T keeps no retention state", backup.ErrNotImplemented, s.Index)
	}

	return ret, nil
}

// presenceScope is the caller's store policy's scope, or the store's
// default for a store without a policy.
func (s *Store) presenceScope(ctx context.Context) (backup.PresenceScope, error) {
	source, ok := s.Index.(backup.PolicySource)
	if !ok {
		return s.PresenceScope, nil
	}

	policy, err := source.StorePolicy(ctx)
	if err != nil {
		return backup.PresenceOff, err
	}

	if policy == nil {
		return s.PresenceScope, nil
	}

	return backup.ParsePresenceScope(policy.PresenceScope)
}

// Presence returns the head filters of the caller's scope for set: nil
// when the index keeps none or the scope is off.
func (s *Store) Presence(ctx context.Context, set string) ([]*proto.PresenceFilter, error) {
	index, ok := s.Index.(backup.PresenceIndex)
	if !ok {
		return nil, nil
	}

	scope, err := s.presenceScope(ctx)
	if err != nil {
		return nil, err
	}

	if scope == backup.PresenceOff {
		return nil, nil
	}

	return index.Presence(ctx, scope, set)
}

// SessionLookup resolves a session id presented by a caller.
type SessionLookup interface {
	LookupSession(ctx context.Context, id string) (*backup.Session, error)
}

// BeginSession opens a session on set for the caller; restore names the
// commit a restore session reads, nil for a backup.
func (s *Store) BeginSession(ctx context.Context, set string, restore *proto.Ref) (*backup.Session, error) {
	if s.Sessions == nil {
		return nil, fmt.Errorf("%w: this store has no sessions", backup.ErrNotImplemented)
	}

	p, err := auth.Require(ctx)
	if err != nil {
		return nil, err
	}

	session := &backup.Session{
		AgentID: p.AgentID,
		Set:     set,
		Restore: restore,
	}

	_, err = s.Sessions.BeginSession(ctx, session)
	if err != nil {
		return nil, err
	}

	return session, nil
}

// EndSession drops what the caller's session id has not committed.
func (s *Store) EndSession(ctx context.Context, id string) error {
	session, err := s.Session(ctx, id)
	if err != nil {
		return err
	}

	return s.Sessions.EndSession(backup.WithSession(ctx, session))
}

// Session returns the live session behind id if it belongs to the caller;
// an unknown id is backup.ErrNotFound, another caller's auth.ErrForbidden.
func (s *Store) Session(ctx context.Context, id string) (*backup.Session, error) {
	if s.Sessions == nil {
		return nil, fmt.Errorf("%w: this store has no sessions", backup.ErrNotImplemented)
	}

	lookup, ok := s.Sessions.(SessionLookup)
	if !ok {
		return nil, fmt.Errorf("%w: this store cannot resolve sessions", backup.ErrNotImplemented)
	}

	p, err := auth.Require(ctx)
	if err != nil {
		return nil, err
	}

	session, err := lookup.LookupSession(ctx, id)
	if errors.Is(err, backup.ErrNoSession) {
		return nil, fmt.Errorf("session %s: %w", id, backup.ErrNotFound)
	}

	if err != nil {
		return nil, err
	}

	if session.AgentID != p.AgentID {
		return nil, fmt.Errorf("%w: session %s belongs to another caller", auth.ErrForbidden, id)
	}

	return session, nil
}
