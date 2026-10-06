package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/backup/storekey"
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

	// Lease is what BeginSession promises the client: the session
	// store's own when it has one.
	Lease time.Duration
}

// NewStore returns a Store over index. sessions is nil for a store without
// sessions.
func NewStore(index backup.Index, sessions backup.SessionStore) *Store {
	s := &Store{
		Index:    index,
		Sessions: sessions,
		Lease:    30 * time.Minute,
	}

	if leased, ok := sessions.(backup.Leased); ok && leased.SessionLease() > 0 {
		s.Lease = leased.SessionLease()
	}

	return s
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

	// a policy rules every set of the store once reindexed, so only the
	// store itself writes one
	if object.Type() == proto.ObjectType_POLICY {
		return nil, fmt.Errorf("%w: policies are written by the store", ErrInvalidRequest)
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

func notFound(ref *proto.Ref) error {
	return fmt.Errorf("object %x: %w", ref.GetHash(), backup.ErrNotFound)
}

// readable refuses a malformed ref, and one no set of the store
// references, before anything is read.
func (s *Store) readable(ctx context.Context, ref *proto.Ref) error {
	if !ref.Valid() {
		return fmt.Errorf("%w: malformed ref", ErrInvalidRequest)
	}

	scope, ok := s.Index.(backup.RefScope)
	if !ok {
		return nil
	}

	referenced, err := scope.References(ctx, ref)
	if err != nil {
		return err
	}

	if !referenced {
		return notFound(ref)
	}

	return nil
}

// offered passes on the object types the store serves and hides the rest.
func offered(ref *proto.Ref, object *proto.Object, err error) (*proto.Object, error) {
	if errors.Is(err, backup.ErrNotFound) {
		return nil, notFound(ref)
	}

	if err != nil {
		return nil, err
	}

	switch object.Type() {
	case proto.ObjectType_COMMIT, proto.ObjectType_TREE, proto.ObjectType_FILE:
		return object, nil
	}

	return nil, notFound(ref)
}

// Get fetches a commit, tree or file object the caller references.
// Anything else, and anything absent, is backup.ErrNotFound.
func (s *Store) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	if err := s.readable(ctx, ref); err != nil {
		return nil, err
	}

	object, err := s.Index.Get(ctx, ref)

	return offered(ref, object, err)
}

// Read answers like Get, except that a store able to address its bytes
// says where they are instead of serving them. Exactly one of the object
// and the location is set.
func (s *Store) Read(ctx context.Context, ref *proto.Ref) (*proto.Object, *proto.Location, error) {
	if err := s.readable(ctx, ref); err != nil {
		return nil, nil, err
	}

	locator, ok := s.Index.(backup.Locator)
	if !ok {
		object, err := s.Index.Get(ctx, ref)
		object, err = offered(ref, object, err)

		return object, nil, err
	}

	object, location, err := locator.Read(ctx, ref)
	if err == nil && location != nil {
		return nil, location, nil
	}

	object, err = offered(ref, object, err)

	return object, nil, err
}

const (
	// treeBatch is how many trees Tree locates at once.
	treeBatch = 1024
	// treeWorkers bounds the tree reads one Tree call has in flight.
	treeWorkers = 16
)

// Tree walks the tree at ref breadth-first, its splits, and the trees of
// directories below it down to maxDepth levels, handing fn runs of those
// the index can locate and the rest as objects.
func (s *Store) Tree(ctx context.Context, ref *proto.Ref, maxDepth uint32, fn func(*proto.GetTreeResponse) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type pending struct {
		ref   *proto.Ref
		depth uint32
	}

	root, err := s.Get(ctx, ref)
	if err != nil {
		return err
	}

	locator, _ := s.Index.(backup.RecordLocator)

	var (
		batch  []*proto.GetTreeResponse
		walked int
	)

	flush := func() error {
		located := make(map[int]bool)

		if locator != nil {
			refs := make([]*proto.Ref, len(batch))
			for i, resp := range batch {
				refs[i] = resp.Ref
			}

			runs, err := locator.LocateRecords(ctx, refs)
			if err != nil {
				return fmt.Errorf("locating trees below %x: %w", ref.GetHash(), err)
			}

			for _, run := range runs {
				for _, record := range run.Records {
					located[int(record.Index)] = true
					record.Index += uint32(walked)
				}
			}

			if len(runs) > 0 {
				if err := fn(&proto.GetTreeResponse{Runs: runs}); err != nil {
					return err
				}
			}
		}

		for i, resp := range batch {
			if located[i] {
				continue
			}

			if err := fn(resp); err != nil {
				return err
			}
		}

		walked += len(batch)
		batch = batch[:0]

		return nil
	}

	type fetched struct {
		pending
		obj *proto.Object
		err error
	}

	fetch := func(next pending) chan fetched {
		done := make(chan fetched, 1)

		go func() {
			obj, err := s.Index.Get(ctx, next.ref)
			done <- fetched{pending: next, obj: obj, err: err}
		}()

		return done
	}

	// the reads run ahead of the walk, but their results are taken in queue
	// order so the walk, and the record indexes, are those of a serial one
	rootDone := make(chan fetched, 1)
	rootDone <- fetched{pending: pending{ref: ref}, obj: root}

	var queue []pending
	inFlight := []chan fetched{rootDone}

	for len(inFlight) > 0 || len(queue) > 0 {
		for len(queue) > 0 && len(inFlight) < treeWorkers {
			inFlight = append(inFlight, fetch(queue[0]))
			queue = queue[1:]
		}

		next := <-inFlight[0]
		inFlight = inFlight[1:]

		if errors.Is(next.err, backup.ErrNotFound) {
			return fmt.Errorf("tree %x: %w", next.ref.GetHash(), backup.ErrNotFound)
		}

		if next.err != nil {
			return next.err
		}

		obj := next.obj
		tree := obj.GetTree()
		if tree == nil {
			return fmt.Errorf("%w: object %x is not a tree", ErrInvalidRequest, next.ref.GetHash())
		}

		batch = append(batch, &proto.GetTreeResponse{Ref: next.ref, Object: obj})
		if len(batch) == treeBatch {
			if err := flush(); err != nil {
				return err
			}
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

	return flush()
}

const (
	// readFileWorkers bounds the part fetches one ReadFile call has in
	// flight.
	readFileWorkers = 16
	// readFileBatch is how many parts ReadFile locates at once.
	readFileBatch = 1024
)

// ReadFile hands fn the file's parts except the indexes in skip: runs
// of them the index can locate, and the stored objects of the rest in
// order. An inline part is never handed over.
func (s *Store) ReadFile(ctx context.Context, ref *proto.Ref, skip []uint32, fn func(*proto.ReadFileResponse) error) error {
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

	var wanted []int
	for i, part := range parts {
		if !skipped[uint32(i)] && part.Ref != nil {
			wanted = append(wanted, i)
		}
	}

	locator, _ := s.Index.(backup.RecordLocator)

	type result struct {
		resp *proto.ReadFileResponse
		err  error
	}

	pending := make(chan chan result, readFileWorkers)

	push := func(done chan result) bool {
		select {
		case pending <- done:
			return true
		case <-ctx.Done():
			return false
		}
	}

	ready := func(resp *proto.ReadFileResponse, err error) chan result {
		done := make(chan result, 1)
		done <- result{resp: resp, err: err}

		return done
	}

	go func() {
		defer close(pending)

		for start := 0; start < len(wanted); start += readFileBatch {
			batch := wanted[start:min(start+readFileBatch, len(wanted))]
			located := make(map[int]bool)

			if locator != nil {
				refs := make([]*proto.Ref, len(batch))
				for j, i := range batch {
					refs[j] = parts[i].Ref
				}

				runs, err := locator.LocateRecords(ctx, refs)
				if err != nil {
					push(ready(nil, fmt.Errorf("locating parts of file %x: %w", ref.GetHash(), err)))
					return
				}

				for _, run := range runs {
					for _, record := range run.Records {
						i := batch[record.Index]
						record.Index = uint32(i)
						located[i] = true
					}
				}

				if len(runs) > 0 && !push(ready(&proto.ReadFileResponse{Runs: runs}, nil)) {
					return
				}
			}

			for _, i := range batch {
				if located[i] {
					continue
				}

				done := make(chan result, 1)
				if !push(done) {
					return
				}

				go func(ref *proto.Ref) {
					o, err := s.Index.Get(ctx, ref)
					done <- result{resp: &proto.ReadFileResponse{Index: uint32(i), Object: o}, err: err}
				}(parts[i].Ref)
			}
		}
	}()

	for done := range pending {
		res := <-done
		if res.err != nil {
			if res.resp != nil {
				return fmt.Errorf("part %d of file %x: %w", res.resp.Index, ref.GetHash(), res.err)
			}

			return res.err
		}

		err = fn(res.resp)
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

// Escrow returns where the store keeps its escrowed key.
func (s *Store) Escrow() (backup.KeyEscrow, error) {
	escrow, ok := s.Index.(backup.KeyEscrow)
	if !ok {
		return nil, fmt.Errorf("%w: index %T keeps no escrowed key", backup.ErrNotImplemented, s.Index)
	}

	return escrow, nil
}

// ReadDir lists what a set held directly under dir at notAfter, sorted by
// name; backup.ErrNotImplemented for an index that cannot answer.
func (s *Store) ReadDir(ctx context.Context, set string, dir string, notAfter time.Time) ([]*proto.TreeNode, error) {
	lister, ok := s.Index.(backup.DirLister)
	if !ok {
		return nil, fmt.Errorf("%w: index %T lists no directories", backup.ErrNotImplemented, s.Index)
	}

	return lister.ReadDir(ctx, set, dir, notAfter)
}

// presenceScope is the store policy's scope; a store without a policy
// has the default policy's.
func (s *Store) presenceScope(ctx context.Context) (backup.PresenceScope, error) {
	policy := storekey.DefaultPolicy()

	if source, ok := s.Index.(backup.PolicySource); ok {
		stored, err := source.StorePolicy(ctx)
		if err != nil {
			return backup.PresenceOff, err
		}

		if stored != nil {
			policy = *stored
		}
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

	if restore != nil {
		if err := s.readable(ctx, restore); err != nil {
			return nil, err
		}
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
