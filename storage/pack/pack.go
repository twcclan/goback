package pack

// TODO: simplify the locking

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

const (
	indexOpenerThreads = 10
	ArchiveSuffix      = ".goback"
	IndexExt           = ".idx"
	// CommittedExt marks an archive of a session that a commit made
	// committed. It is written before the index is told, so the storage
	// alone says which session archives are committed.
	CommittedExt = ".committed"
	varIntMaxSize      = 10
)

var (
	ErrFileNotFound     = errors.New("requested file was not found")
	// ErrFileExists is CreateNew's answer for a name that is taken.
	ErrFileExists       = errors.New("the file exists")
	ErrInvalidExtension = errors.New("the provided extension is invalid")
)

// NewPackStorage builds a store from options; WithArchiveStorage and
// WithArchiveIndex are required.
func NewPackStorage(options ...PackOption) (*PackStorage, error) {
	opts := &packOptions{
		maxParallel: 1,
		maxSize:     1024 * 1024 * 1024,
		compaction: CompactionConfig{
			MinimumCandidates: 1000,
		},
	}

	for _, opt := range options {
		opt(opts)
	}

	if opts.logger == nil {
		opts.logger = slog.Default()
	}

	if opts.storage == nil {
		return nil, errors.New("No archive storage provided")
	}

	if opts.index == nil {
		return nil, errors.New("No archive index provided")
	}

	atRest := opts.atRestKey

	if opts.claimOpen <= 0 {
		opts.claimOpen = DefaultClaimOpen
	}

	if opts.claimGrace <= 0 {
		opts.claimGrace = DefaultClaimGrace
	}

	claims, _ := opts.index.(ClaimIndex)

	return &PackStorage{
		claims:           claims,
		claimOpen:        opts.claimOpen,
		claimGrace:       opts.claimGrace,
		archives:         make([]*archive, 0),
		retired:          make(map[string]bool),
		sessions:         make(map[string]*writeSession),
		archiveSemaphore: semaphore.NewWeighted(int64(opts.maxParallel)),
		storage:          opts.storage,
		compaction:       opts.compaction,
		maxSize:          opts.maxSize,
		closeBeforeRead:  opts.closeBeforeRead,
		cache:            opts.cache,
		index:            opts.index,
		idleFinalize:     opts.idleFinalize,
		sessionLease:     opts.sessionLease,
		atRest:           atRest,
		observer:         opts.observer,
		logger:           opts.logger,
	}, nil
}

// PackStorage stores objects in archives. Writes made under a session
// (see BeginSession) go to that session's own archives and are visible only
// to the session until it stores a commit; writes without a session go to
// root archives and are visible at once.
type PackStorage struct {
	archiveSemaphore *semaphore.Weighted
	escrowMtx        sync.Mutex
	storage          ArchiveStorage
	compaction       CompactionConfig
	maxSize          uint64
	closeBeforeRead  bool
	cache            backup.ObjectStore
	index            ArchiveIndex
	idleFinalize     time.Duration
	sessionLease     time.Duration

	// claims is the index again when it lets several processes write one
	// session, nil when this process is the only writer
	claims     ClaimIndex
	claimOpen  time.Duration
	claimGrace time.Duration

	observer ArchiveObserver

	// all of these are guarded by mtx
	mtx      sync.RWMutex
	archives []*archive
	// retired names archives a rewrite replaced; they are never opened
	// again, whatever a stale index location says
	retired map[string]bool

	sessionsMtx sync.Mutex
	sessions    map[string]*writeSession

	compactorMtx sync.Mutex
	atRest       *AtRestKey
	logger       *slog.Logger
}

type pendingObject struct {
	archive *archive
	record  *IndexRecord
}

var _ backup.ObjectStore = (*PackStorage)(nil)
var _ backup.Counter = (*PackStorage)(nil)
var _ backup.SessionStore = (*PackStorage)(nil)
var _ backup.Leased = (*PackStorage)(nil)

// SessionLease implements backup.Leased: zero when sessions never expire.
func (ps *PackStorage) SessionLease() time.Duration { return ps.sessionLease }

// Has reports whether the store holds a copy of the object that the caller
// may rely on. The newest record of the object decides: a tombstone newer
// than every copy makes it absent, unless an un-tombstone is newer still.
func (ps *PackStorage) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	scope := ScopeOf(ctx)

	tomb := proto.TombstoneRef(ref)
	untomb := proto.TombstoneRef(tomb)

	found, err := ps.index.LocateCopies([]*proto.Ref{ref, tomb, untomb}, scope)
	if err != nil {
		return false, err
	}

	bound, err := ps.tombstoneBound(found[string(tomb.Hash)], found[string(untomb.Hash)])
	if err != nil {
		return false, err
	}

	for _, loc := range found[string(ref.Hash)] {
		a, err := ps.archiveByName(loc.Archive)
		if err != nil && !errors.Is(err, errArchiveRetired) {
			return false, err
		}

		if a == nil {
			continue
		}

		if bound == nil || a.newerThan(loc.Record, *bound) {
			return true, nil
		}
	}

	if ws := ps.lookupWriteSession(scope.Session); ws != nil {
		ws.pendingMtx.RLock()
		_, pending := ws.pending[string(ref.Hash)]
		ws.pendingMtx.RUnlock()

		if pending {
			return true, nil
		}
	}

	// another process may hold it in an archive it has not finalized yet
	if ps.claims != nil && scope.Session != "" {
		return ps.claims.Holds(ref, scope.Session)
	}

	return false, nil
}

// tombstoneBound returns the version of the newest tombstone among tombs,
// or nil when there is none or an un-tombstone among untombs is newer.
func (ps *PackStorage) tombstoneBound(tombs, untombs []IndexLocation) (*Version, error) {
	tomb, err := ps.newest(tombs)
	if tomb == nil || err != nil {
		return nil, err
	}

	untomb, err := ps.newest(untombs)
	if err != nil {
		return nil, err
	}

	if untomb != nil && tomb.Before(*untomb) {
		return nil, nil
	}

	return tomb, nil
}

// newest returns the version of the newest of locs, nil for none.
func (ps *PackStorage) newest(locs []IndexLocation) (*Version, error) {
	var newest *Version

	for _, loc := range locs {
		a, err := ps.archiveByName(loc.Archive)
		if err != nil && !errors.Is(err, errArchiveRetired) {
			return nil, err
		}

		if a == nil {
			continue
		}

		if v := a.version(loc.Record); newest == nil || newest.Before(v) {
			newest = &v
		}
	}

	return newest, nil
}

// committedCopy finds a committed copy of ref the scope may read, passing
// over excluded archives, archives a rewrite retired and, when liveOnly,
// copies the last collection found unreachable. No copy is a nil archive.
func (ps *PackStorage) committedCopy(scope Scope, ref *proto.Ref, liveOnly bool, exclude ...string) (*archive, *IndexRecord, error) {
	for {
		loc, err := ps.index.LocateObject(ref, scope, exclude...)
		if errors.Is(err, ErrRecordNotFound) {
			return nil, nil, nil
		}

		if err != nil {
			return nil, nil, err
		}

		a, err := ps.archiveByName(loc.Archive)
		if err != nil && !errors.Is(err, errArchiveRetired) {
			return nil, nil, err
		}

		if a == nil || (liveOnly && a.candidate(ref.Hash)) {
			exclude = append(exclude, loc.Archive)
			continue
		}

		return a, &loc.Record, nil
	}
}

func (ps *PackStorage) cacheable(obj *proto.Object) bool {
	if obj == nil {
		return false
	}

	switch obj.Type() {
	case proto.ObjectType_COMMIT, proto.ObjectType_TREE, proto.ObjectType_FILE:
		return true
	}

	return false
}

func (ps *PackStorage) putWriteCache(ctx context.Context, obj *proto.Object, err error) error {
	if ps.cache != nil && err == nil && ps.cacheable(obj) {
		_ = ps.cache.Put(ctx, obj)
	}

	return err
}

func (ps *PackStorage) putReadCache(ctx context.Context) func(*proto.Object, error) (*proto.Object, error) {
	return func(obj *proto.Object, err error) (*proto.Object, error) {
		if ps.cache != nil && err == nil && ps.cacheable(obj) {
			_ = ps.cache.Put(ctx, obj)
		}

		return obj, err
	}
}

// forgetCached drops from the cache the objects of a deleted archive that
// no other archive still holds.
func (ps *PackStorage) forgetCached(ctx context.Context, idx IndexFile) {
	if ps.cache == nil {
		return
	}

	for _, rec := range idx {
		switch proto.ObjectType(rec.Type) {
		case proto.ObjectType_COMMIT, proto.ObjectType_TREE, proto.ObjectType_FILE:
		default:
			continue
		}

		ref := &proto.Ref{Hash: rec.Sum[:]}
		if _, err := ps.index.LocateObject(ref, Scope{}); !errors.Is(err, ErrRecordNotFound) {
			continue
		}

		if err := ps.cache.Delete(ctx, ref); err != nil {
			ps.logger.Warn("dropping a deleted object from the metadata cache failed", "ref", fmt.Sprintf("%x", rec.Sum), "err", err)
		}
	}
}

func (ps *PackStorage) getCache(ctx context.Context, ref *proto.Ref) *proto.Object {
	if ps.cache != nil {
		obj, err := ps.cache.Get(ctx, ref)
		if err == nil {
			return obj
		}
	}

	return nil
}

// Count implements backup.Counter.
func (ps *PackStorage) Count() (uint64, uint64, error) {
	return ps.index.CountObjects()
}

// Put implements backup.ObjectStore.
func (ps *PackStorage) Put(ctx context.Context, object *proto.Object) error {
	ctx, span := tracer.Start(ctx, "PackStorage.Put")
	defer span.End()

	return ps.putWriteCache(ctx, object, ps.put(ctx, object))
}

func (ps *PackStorage) put(ctx context.Context, object *proto.Object) error {
	hdr, stored, err := proto.HeaderFor(object)
	if err != nil {
		return err
	}

	ws, err := ps.writeSessionFor(ctx)
	if err != nil {
		return err
	}

	var (
		claimed *archive
		indexed chan error
	)

	err = ps.withWritableArchive(ctx, ws, func(a *archive) error {
		err := a.putRaw(ctx, hdr, stored)
		if err != nil {
			return err
		}

		ws.addPending(a, object.Ref())

		// queued under the session lock, so a finalize that follows
		// finds the row queued and writes it first
		if a.rows != nil {
			if record := a.indexLocation(object.Ref()); record != nil {
				claimed, indexed = a, a.rows.enqueue(*record)
			}
		}

		return nil
	})

	if err != nil {
		return err
	}

	// another process can only rely on the object once its row is in
	if claimed != nil {
		err := claimed.rows.wait(indexed)
		if lapsed(err) {
			// a commit elsewhere gave the archive up while this process
			// was still writing it
			_ = ps.finalizeArchive(claimed)

			return fmt.Errorf("%w: %w", backup.ErrSessionLost, err)
		}

		if err != nil {
			return err
		}
	}

	ps.touchSession(ws)

	if object.Type() == proto.ObjectType_COMMIT {
		err := ps.commit(ctx, ws, object.Ref())
		if err != nil {
			ps.refuse(ctx, ws, object.Ref(), err)
		}

		return err
	}

	return nil
}

// refuse tombstones a commit that failed after its record was written, so
// the record stays dead even where a rebuild without the index would find
// it in an archive. The tombstone goes beside the record, in the session's
// own archive, so the two are kept or dropped together.
func (ps *PackStorage) refuse(ctx context.Context, ws *writeSession, ref *proto.Ref, cause error) {
	err := ps.withWritableArchive(ctx, ws, func(a *archive) error {
		return a.putTombstone(ctx, ref, false)
	})
	if err != nil {
		ps.logger.Warn("tombstoning a refused commit failed", "commit", fmt.Sprintf("%x", ref.GetHash()), "cause", cause, "err", err)
	}
}

// commit makes everything the session wrote durable and visible: its open
// archive is finalized and its archives flip to committed. A session with
// an archive that failed to finalize cannot commit: the objects it
// acknowledged are in no index. A commit ends the session.
func (ps *PackStorage) commit(ctx context.Context, ws *writeSession, ref *proto.Ref) error {
	if err := ws.failed(); err != nil {
		return fmt.Errorf("session %s lost an archive: %w", ws.id, err)
	}

	// an archive of ours that lapsed is lost now, which settle answers for
	err := ps.flushSession(ws)
	if err != nil && !lapsed(err) {
		return err
	}

	if ws.session == nil {
		return nil
	}

	if ps.claims != nil {
		if err := ps.settle(ws.id); err != nil {
			return err
		}
	}

	if err := ps.resurrect(ctx, ws, ref); err != nil {
		return err
	}

	outcome, err := ps.markEnded(ws.id, sessionCommitted)
	if err != nil {
		return err
	}

	if outcome != sessionCommitted {
		return fmt.Errorf("session %s ended before it committed: %w", ws.id, backup.ErrNoSession)
	}

	err = ps.markCommitted(ws.id)
	if err != nil {
		return err
	}

	err = ps.index.CommitSession(ws.id)
	if err != nil {
		return err
	}

	ps.mtx.RLock()
	for _, a := range ps.archives {
		a.mtx.Lock()
		if a.session == ws.id {
			a.state = ArchiveCommitted
		}
		a.mtx.Unlock()
	}
	ps.mtx.RUnlock()

	return ps.endSession(ws.id)
}

// releasePending drops the pending entries of an archive once the archive
// index can answer for them.
func (ps *PackStorage) releasePending(ws *writeSession, a *archive, index IndexFile) {
	ws.pendingMtx.Lock()
	for i := range index {
		key := string(index[i].Sum[:])
		if p, ok := ws.pending[key]; ok && p.archive == a {
			delete(ws.pending, key)
		}
	}
	ws.pendingMtx.Unlock()

	a.releaseWriteIndex()
}

// errArchiveRetired names an archive a rewrite replaced.
var errArchiveRetired = errors.New("archive was retired by a rewrite")

// archiveByName returns the loaded archive, opening it read-only from the
// storage if another writer finalized it.
func (ps *PackStorage) archiveByName(name string) (*archive, error) {
	ps.mtx.RLock()
	if ps.retired[name] {
		ps.mtx.RUnlock()
		return nil, errArchiveRetired
	}

	for _, archive := range ps.archives {
		if archive.name == name {
			ps.mtx.RUnlock()
			return archive, nil
		}
	}
	ps.mtx.RUnlock()

	return ps.openArchive(name)
}

// retireArchive unloads an archive a rewrite replaced and keeps it from
// being opened again while its index rows and files go.
func (ps *PackStorage) retireArchive(a *archive) {
	ps.mtx.Lock()
	ps.retired[a.name] = true
	ps.mtx.Unlock()

	ps.unloadArchive(a)
}

// indexLocation returns the archive and record for the provided ref, or a nil
// record if the ref is not visible to the caller.
func (ps *PackStorage) indexLocation(ctx context.Context, ref *proto.Ref) (*archive, *IndexRecord, error) {
	scope := ScopeOf(ctx)

	a, rec, err := ps.committedCopy(scope, ref, false)
	if err != nil || a != nil {
		return a, rec, err
	}

	ws := ps.lookupWriteSession(scope.Session)
	if ws == nil {
		return nil, nil, nil
	}

	ws.pendingMtx.RLock()
	p, ok := ws.pending[string(ref.Hash)]
	ws.pendingMtx.RUnlock()

	if ok {
		return p.archive, p.record, nil
	}

	return nil, nil, nil
}

// Get implements backup.ObjectStore.
func (ps *PackStorage) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	ctx, span := tracer.Start(ctx, "PackStorage.Get")
	defer span.End()

	ps.touchSessionOf(ctx)

	obj, err := ps.get(ctx, ref)
	if err != nil && !errors.Is(err, backup.ErrNotFound) {
		// the copy may have moved while a rewrite retired its archive
		obj, err = ps.get(ctx, ref)
	}

	return ps.putReadCache(ctx)(obj, err)
}

func (ps *PackStorage) get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	archive, rec, err := ps.indexLocation(ctx, ref)
	if err != nil {
		return nil, err
	}

	if rec == nil {
		return nil, backup.ErrNotFound
	}

	// the cache answers only for what the index serves this caller, so
	// it never outlives a deletion or reaches past a scope
	if cached := ps.getCache(ctx, ref); cached != nil {
		return cached, nil
	}

	archive.mtx.RLock()
	needClose := !archive.readOnly && ps.closeBeforeRead
	archive.mtx.RUnlock()

	if needClose {
		trace.SpanFromContext(ctx).SetAttributes(attribute.Bool("close-before-read", true))
		ps.logger.Info("closing archive before reading an object", "archive", archive.name, "ref", fmt.Sprintf("%x", ref.Hash))
		err := ps.finalizeArchive(archive)

		if err != nil {
			return nil, err
		}
	}

	return archive.getRaw(ctx, ref, rec)
}

// Delete implements backup.ObjectStore.
func (ps *PackStorage) Delete(ctx context.Context, ref *proto.Ref) error {
	return ps.tombstone(ctx, ref, false)
}

// Erase implements backup.Eraser.
func (ps *PackStorage) Erase(ctx context.Context, ref *proto.Ref) error {
	return ps.tombstone(ctx, ref, true)
}

func (ps *PackStorage) tombstone(ctx context.Context, ref *proto.Ref, erase bool) error {
	ws, err := ps.writeSessionFor(ctx)
	if err != nil {
		return err
	}

	return ps.withWritableArchive(ctx, ws, func(a *archive) error {
		return a.putTombstone(ctx, ref, erase)
	})
}

// WalkHeaders implements backup.HeaderWalker over the committed archives.
func (ps *PackStorage) WalkHeaders(ctx context.Context, t proto.ObjectType, fn func(*proto.ObjectHeader) error) error {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	for _, archive := range ps.archives {
		archive.mtx.RLock()
		pending := archive.state == ArchivePending
		archive.mtx.RUnlock()

		if pending {
			continue
		}

		visit := func(hdr *proto.ObjectHeader, _ []byte, _, _ uint32) error {
			if err := ctx.Err(); err != nil {
				return err
			}

			if t == proto.ObjectType_INVALID || hdr.Type == t {
				return fn(hdr)
			}

			return nil
		}

		if t != proto.ObjectType_INVALID {
			typed, err := archive.foreachOfType(t, false, visit)
			if err != nil {
				return err
			}

			if typed {
				continue
			}
		}

		err := archive.foreach(loadNone, visit)
		if err != nil {
			return err
		}
	}

	return nil
}

// Walk implements backup.ObjectStore.
func (ps *PackStorage) Walk(ctx context.Context, load bool, t proto.ObjectType, fn backup.ObjectReceiver) error {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	var pred loadPredicate

	switch {
	case !load:
		pred = loadNone
	case load && t == proto.ObjectType_INVALID:
		pred = loadAll
	case load && t != proto.ObjectType_INVALID:
		pred = loadType(t)
	}

	for _, archive := range ps.archives {
		archive.mtx.RLock()
		pending := archive.state == ArchivePending
		archive.mtx.RUnlock()

		if pending {
			continue
		}

		visit := func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
			if t == proto.ObjectType_INVALID || hdr.Type == t {
				var obj *proto.Object
				var err error

				if load {
					obj, err = proto.ObjectFromStored(hdr, bytes)
					if err != nil {
						return errors.Wrapf(err, "object %x in archive %s", hdr.Ref.Hash, archive.name)
					}
				}

				return fn(obj)
			}

			return nil
		}

		if t != proto.ObjectType_INVALID {
			typed, err := archive.foreachOfType(t, load, visit)
			if err != nil {
				return err
			}

			if typed {
				continue
			}
		}

		ps.logger.Debug("reading archive", "archive", archive.name)

		err := archive.foreach(pred, visit)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ps *PackStorage) unloadArchive(a *archive) {
	ps.mtx.Lock()
	filtered := make([]*archive, 0, len(ps.archives))
	for _, arch := range ps.archives {
		if arch.name != a.name {
			filtered = append(filtered, arch)
		}
	}
	ps.archives = filtered
	ps.mtx.Unlock()
}

// finalizeArchive closes the writer of a session's open archive and
// registers the archive with the index.
func (ps *PackStorage) finalizeArchive(a *archive) error {
	ws := a.owner
	if ws == nil {
		return nil
	}

	ws.mtx.Lock()
	defer ws.mtx.Unlock()

	if ws.archive != a {
		return nil
	}

	return ps.finalizeLocked(ws)
}

// finalizeLocked finalizes the session's open archive; the caller holds the
// session lock. The writer slot is released as soon as the writer is closed,
// even if storing or indexing its index fails. An archive the index refuses
// because its session is gone is deleted.
func (ps *PackStorage) finalizeLocked(ws *writeSession) error {
	a := ws.archive
	if a == nil {
		return nil
	}

	ws.archive = nil

	if a.rows != nil {
		a.closing.Stop()

		if err := a.rows.drain(); err != nil && !lapsed(err) {
			ws.fail(err)
		}
	}

	index, err := a.CloseWriter()
	ps.archiveSemaphore.Release(1)

	if index == nil {
		ws.fail(err)
		return err
	}

	if err == nil {
		var created time.Time

		created, err = a.indexCreated()
		a.setCreated(created)
	}

	if a.rows != nil {
		return ps.finalizeClaimed(ws, a, index, err)
	}

	info := ws.archiveInfo(a.name)
	info.Created = a.created

	indexErr := ps.index.IndexArchive(info, index)
	if errors.Is(indexErr, backup.ErrNoSession) {
		ps.dropArchive(a)
		return indexErr
	}

	if indexErr == nil {
		ps.releasePending(ws, a, index)
	}

	if err != nil {
		ws.fail(err)
		return err
	}

	ws.fail(indexErr)

	return indexErr
}

// finalizeClaimed turns a claimed archive pending, its objects being
// indexed already. One whose claim lapsed is turned lost and deleted: a
// commit may have stopped waiting for it, so its objects count as gone.
func (ps *PackStorage) finalizeClaimed(ws *writeSession, a *archive, index IndexFile, closeErr error) error {
	if closeErr != nil {
		ws.fail(closeErr)
		ps.abandon(ws, a)

		return closeErr
	}

	err := ps.claims.FinalizeArchive(a.name, ps.claimLimit(), a.created)
	if lapsed(err) {
		ps.logger.Warn("finalizing an archive after its claim lapsed; its objects are lost", "archive", a.name)
		ps.abandon(ws, a)

		return fmt.Errorf("archive %s: %w", a.name, err)
	}

	if err != nil {
		ws.fail(err)

		return err
	}

	ps.releasePending(ws, a, index)

	return nil
}

// abandon turns a claimed archive lost and deletes it.
func (ps *PackStorage) abandon(ws *writeSession, a *archive) {
	if _, err := ps.claims.Abandon(a.name, 0); err != nil {
		ps.logger.Warn("abandoning archive failed", "archive", a.name, "err", err)
	}

	ws.pendingMtx.Lock()
	for key, p := range ws.pending {
		if p.archive == a {
			delete(ws.pending, key)
		}
	}
	ws.pendingMtx.Unlock()

	ps.dropArchive(a)
}

// discardArchive closes a session's open archive without indexing it and
// deletes it; the caller holds the session lock.
func (ps *PackStorage) discardArchive(ws *writeSession) {
	a := ws.archive
	if a == nil {
		return
	}

	ws.archive = nil

	_, _ = a.CloseWriter()
	ps.archiveSemaphore.Release(1)
	ps.dropArchive(a)
}

// dropArchive unloads an archive and deletes its files.
func (ps *PackStorage) dropArchive(a *archive) {
	ps.unloadArchive(a)

	if err := a.Close(); err != nil {
		ps.logger.Warn("closing archive failed", "archive", a.name, "err", err)
	}

	ps.deleteArchiveFiles(a.name)
}

// hasIndexFile reports whether the archive's writer got as far as storing
// its index, which only a finalized archive has.
func (ps *PackStorage) hasIndexFile(name string) bool {
	file, err := ps.storage.Open(name + IndexExt)
	if err != nil {
		return false
	}

	_ = file.Close()

	return true
}

// archiveStored tells the observer an archive is in the storage.
func (ps *PackStorage) archiveStored(a *archive, bytes int64) {
	if ps.observer != nil {
		ps.observer.ArchiveStored(a.name, bytes, a.session)
	}
}

// markCommitted writes the committed marker of every archive the session
// is about to commit.
func (ps *PackStorage) markCommitted(session string) error {
	pending, err := ps.index.PendingArchives(session)
	if err != nil {
		return err
	}

	for _, name := range pending {
		f, err := ps.storage.Create(name + CommittedExt)
		if err != nil {
			return fmt.Errorf("marking archive %s committed: %w", name, err)
		}

		_, err = f.Write([]byte(session))
		if err == nil {
			err = f.Close()
		} else {
			_ = f.Close()
		}

		if err != nil {
			return fmt.Errorf("marking archive %s committed: %w", name, err)
		}
	}

	return nil
}

// markedCommitted reports whether a commit marked the archive committed.
func (ps *PackStorage) markedCommitted(name string) bool {
	file, err := ps.storage.Open(name + CommittedExt)
	if err != nil {
		return false
	}

	_ = file.Close()

	return true
}

func (ps *PackStorage) deleteArchiveFiles(name string) {
	err := ps.storage.Delete(name + IndexExt)
	if err != nil && !notExist(err) {
		ps.logger.Warn("deleting index failed", "archive", name, "err", err)
	}

	err = ps.storage.Delete(name + ArchiveSuffix)
	if err != nil && !notExist(err) {
		ps.logger.Warn("deleting archive failed", "archive", name, "err", err)
	}

	if err == nil && ps.observer != nil {
		ps.observer.ArchiveDeleted(name)
	}

	err = ps.storage.Delete(name + GCExt)
	if err != nil && !notExist(err) {
		ps.logger.Warn("deleting gc result failed", "archive", name, "err", err)
	}

	err = ps.storage.Delete(name + CommittedExt)
	if err != nil && !notExist(err) {
		ps.logger.Warn("deleting committed marker failed", "archive", name, "err", err)
	}
}

// openArchive loads a finalized archive. One the index does not know is
// indexed as committed, unless a session wrote it and never finalized it
// (no index file beside it): that one is deleted.
func (ps *PackStorage) openArchive(name string) (*archive, error) {
	info, known, err := ps.index.LookupArchive(name)
	if err != nil {
		return nil, err
	}

	if !known && ParsePlacement(name).Session != "" && !ps.hasIndexFile(name) && !ps.markedCommitted(name) {
		ps.logger.Info("deleting archive of an unfinished session", "archive", name)
		ps.deleteArchiveFiles(name)

		return nil, nil
	}

	ps.mtx.RLock()
	retired := ps.retired[name]
	ps.mtx.RUnlock()

	if retired {
		return nil, errArchiveRetired
	}

	a, err := openArchive(ps.storage, name, ps.atRest, ps.logger)
	if err != nil {
		return nil, err
	}

	a.gc, err = readGCFile(ps.storage, name)
	if err != nil {
		ps.logger.Warn("ignoring unreadable gc result", "archive", name, "err", err)
	}

	if !known || info.Created.IsZero() {
		created, err := a.indexCreated()
		if err != nil {
			return nil, fmt.Errorf("reading when the index of %s was created: %w", name, err)
		}

		idx, err := a.getIndex()
		if err != nil {
			return nil, err
		}

		if !known {
			ps.logger.Info("indexing archive", "archive", name)
			info = ArchiveInfo{Name: name}
		}

		info.Created = created

		// an archive the index knows without its creation time gets it
		err = ps.index.IndexArchive(info, idx)
		if err != nil {
			return nil, err
		}
	}

	a.state = info.State
	a.session = info.Session
	a.created = info.Created

	ps.mtx.Lock()
	defer ps.mtx.Unlock()

	for _, loaded := range ps.archives {
		if loaded.name == name {
			_ = a.Close()
			return loaded, nil
		}
	}

	ps.archives = append(ps.archives, a)

	return a, nil
}

// withWritableArchive runs writer against the session's open archive,
// opening one when the session has none or its archive is full. Writes of
// one session are serialized.
func (ps *PackStorage) withWritableArchive(ctx context.Context, ws *writeSession, writer func(*archive) error) error {
	ws.mtx.Lock()
	defer ws.mtx.Unlock()

	if ws.archive != nil {
		ws.archive.mtx.RLock()
		full := ws.archive.size >= ps.maxSize
		ws.archive.mtx.RUnlock()

		// the timer finalizes an archive that has been open for as long
		// as it may be, unless the process had no CPU to run it
		expired := ws.archive.rows != nil && time.Since(ws.archive.opened) >= ps.claimOpen

		if full || expired {
			ps.logger.Debug("finalizing archive", "archive", ws.archive.name, "full", full)
			err := ps.finalizeLocked(ws)
			if err != nil && !lapsed(err) {
				return err
			}
		}
	}

	if ws.archive == nil {
		err := ps.archiveSemaphore.Acquire(ctx, 1)
		if err != nil {
			return err
		}

		a, err := newArchive(ps.storage, ws.placement.Dir(), ps.atRest, ps.logger)
		if err != nil {
			ps.archiveSemaphore.Release(1)
			return err
		}

		a.owner = ws
		a.session = ws.id
		a.state = ws.state()
		a.stored = ps.archiveStored

		if ps.claims != nil && ws.session != nil {
			if err := ps.claim(a); err != nil {
				_, _ = a.CloseWriter()
				ps.archiveSemaphore.Release(1)
				ps.deleteArchiveFiles(a.name)

				return err
			}
		}

		ps.mtx.Lock()
		ps.archives = append(ps.archives, a)
		ps.mtx.Unlock()

		ws.archive = a
	}

	ws.lastWrite = time.Now()

	return writer(ws.archive)
}

// Close implements backup.ObjectStore.
func (ps *PackStorage) Close() error {
	err := ps.Flush()
	if err != nil {
		return err
	}

	ps.mtx.Lock()
	defer ps.mtx.Unlock()

	for i := range ps.archives {
		err := ps.archives[i].Close()
		if err != nil {
			return err
		}
	}

	if ps.cache != nil {
		if cls, ok := ps.cache.(io.Closer); ok {
			cls.Close()
		}
	}

	return nil
}

// Open implements backup.ObjectStore.
func (ps *PackStorage) Open() error {
	names, err := ps.archiveNames()
	if err != nil {
		return errors.Wrap(err, "failed listing archive names")
	}

	sem := semaphore.NewWeighted(indexOpenerThreads)
	group, ctx := errgroup.WithContext(context.Background())

	for _, name := range names {
		if err = sem.Acquire(ctx, 1); err != nil {
			break
		}

		group.Go(func() error {
			defer sem.Release(1)

			_, err := ps.openArchive(name)

			return err
		})
	}

	err = group.Wait()
	if err != nil {
		return err
	}

	return ps.reconcileSessions()
}

// refreshArchives catches the loaded archives up with the storage and the
// index: it opens the archives other processes stored since, takes up the
// commits of archives it loaded while they were pending, and unloads the
// committed archives whose files are gone.
func (ps *PackStorage) refreshArchives() error {
	names, err := ps.archiveNames()
	if err != nil {
		return errors.Wrap(err, "failed listing archive names")
	}

	listed := make(map[string]bool, len(names))
	for _, name := range names {
		listed[name] = true
	}

	ps.mtx.RLock()
	loaded := make(map[string]*archive, len(ps.archives))
	for _, a := range ps.archives {
		loaded[a.name] = a
	}
	ps.mtx.RUnlock()

	sem := semaphore.NewWeighted(indexOpenerThreads)
	group, ctx := errgroup.WithContext(context.Background())

	for name := range listed {
		if loaded[name] != nil {
			continue
		}

		if err := sem.Acquire(ctx, 1); err != nil {
			break
		}

		group.Go(func() error {
			defer sem.Release(1)

			// an archive another process is still writing is that
			// process's to finish or clean up
			info, known, err := ps.index.LookupArchive(name)
			if err != nil {
				return err
			}

			if (known && info.State == ArchiveOpen) || (!known && !ps.hasIndexFile(name)) {
				return nil
			}

			_, err = ps.openArchive(name)
			if errors.Is(err, errArchiveRetired) {
				return nil
			}

			return err
		})
	}

	if err := group.Wait(); err != nil {
		return err
	}

	for name, a := range loaded {
		a.mtx.RLock()
		settled := a.readOnly && a.owner == nil
		state := a.state
		a.mtx.RUnlock()

		if !settled {
			continue
		}

		if state == ArchiveCommitted {
			if !listed[name] {
				ps.unloadArchive(a)
				_ = a.Close()
			}

			continue
		}

		info, known, err := ps.index.LookupArchive(name)
		if err != nil {
			return err
		}

		if known && info.State == ArchiveCommitted {
			a.mtx.Lock()
			a.state, a.session = ArchiveCommitted, ""
			a.mtx.Unlock()
		}
	}

	return nil
}

// File is one archive, index or gc file held by an ArchiveStorage.
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer

	Stat() (fs.FileInfo, error)
}

// ArchiveStorage holds a store's files under slash-separated names; Open of
// a missing name is ErrFileNotFound.
type ArchiveStorage interface {
	Create(name string) (File, error)
	// CreateNew stores data under name unless a file has it already, which
	// it answers with ErrFileExists; of several callers racing for a name,
	// exactly one succeeds.
	CreateNew(name string, data []byte) error
	Open(name string) (File, error)
	List(extension string) ([]string, error)
	Delete(name string) error
	DeleteAll() error
}

// IndexLocation is where an ArchiveIndex found an object.
type IndexLocation struct {
	Archive string
	Record  IndexRecord
}

var (
	// ErrRecordNotFound is LocateObject's answer when no visible archive holds the object.
	ErrRecordNotFound = errors.New("couldn't find index record")
)

// ArchiveIndex maps refs to the archives holding them, records each
// archive's visibility, and keeps the live sessions.
type ArchiveIndex interface {
	SessionIndex

	LocateObject(ref *proto.Ref, scope Scope, exclude ...string) (IndexLocation, error)
	// LocateCopies returns every copy the archives visible to scope hold
	// of each ref, keyed by its hash; a ref none holds is absent.
	LocateCopies(refs []*proto.Ref, scope Scope) (map[string][]IndexLocation, error)
	LookupArchive(archive string) (ArchiveInfo, bool, error)
	// IndexArchive registers an archive; a pending one needs a live session.
	// Of an archive it already knows, it only records a creation time the
	// index lacks.
	IndexArchive(archive ArchiveInfo, index IndexFile) error
	DeleteArchive(archive string, index IndexFile) error
	Close() error
	CountObjects() (uint64, uint64, error)
}
