package pack

// TODO: simplify the locking

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strings"
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
	IndexOpenerThreads = 10
	ArchiveSuffix      = ".goback"
	ArchivePattern     = "*" + ArchiveSuffix
	IndexExt           = ".idx"
	varIntMaxSize      = 10
)

var (
	ErrFileNotFound     = errors.New("requested file was not found")
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

	return &PackStorage{
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
		atRest:           opts.atRest,
		logger:           opts.logger,
	}, nil
}

// PackStorage stores objects in archives. Writes made under a session
// (see BeginSession) go to that session's own archives and are visible only
// to the session until it stores a commit; writes without a session go to
// root archives and are visible at once.
type PackStorage struct {
	archiveSemaphore *semaphore.Weighted
	storage          ArchiveStorage
	compaction       CompactionConfig
	maxSize          uint64
	closeBeforeRead  bool
	cache            backup.ObjectStore
	index            ArchiveIndex
	idleFinalize     time.Duration
	sessionLease     time.Duration

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

// Has reports whether the store holds a copy of the object that the last
// completed garbage collection found reachable. A copy it found unreachable
// does not count, so the caller uploads again instead of relying on it.
func (ps *PackStorage) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	scope := ScopeOf(ctx)

	a, _, err := ps.committedCopy(scope, ref, true)
	if err != nil {
		return false, err
	}

	if a != nil {
		return true, nil
	}

	ws := ps.lookupWriteSession(scope.Session)
	if ws == nil {
		return false, nil
	}

	ws.pendingMtx.RLock()
	_, pending := ws.pending[string(ref.Hash)]
	ws.pendingMtx.RUnlock()

	return pending, nil
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

	err = ps.withWritableArchive(ctx, ws, func(a *archive) error {
		err := a.putRaw(ctx, hdr, stored)
		if err != nil {
			return err
		}

		ws.addPending(a, object.Ref())

		return nil
	})

	if err != nil {
		return err
	}

	ps.touchSession(ws)

	if object.Type() == proto.ObjectType_COMMIT {
		return ps.commit(ws)
	}

	return nil
}

// commit makes everything the session wrote durable and visible: its open
// archive is finalized and its archives flip to committed. A session with
// an archive that failed to finalize cannot commit: the objects it
// acknowledged are in no index.
func (ps *PackStorage) commit(ws *writeSession) error {
	if err := ws.failed(); err != nil {
		return fmt.Errorf("session %s lost an archive: %w", ws.id, err)
	}

	err := ps.flushSession(ws)
	if err != nil {
		return err
	}

	if ws.session == nil {
		return nil
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

	return nil
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

// indexLocationExcept finds a committed copy of the ref outside the given
// archives.
func (ps *PackStorage) indexLocationExcept(ref *proto.Ref, exclude ...*archive) (*IndexLocation, error) {
	var exclusions []string
	for _, archive := range exclude {
		exclusions = append(exclusions, archive.name)
	}

	loc, err := ps.index.LocateObject(ref, Scope{}, exclusions...)
	if err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return nil, nil
		}

		return nil, err
	}

	return &loc, nil
}

// Get implements backup.ObjectStore.
func (ps *PackStorage) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	ctx, span := tracer.Start(ctx, "PackStorage.Get")
	defer span.End()

	ps.touchSessionOf(ctx)

	cached := ps.getCache(ctx, ref)
	if cached != nil {
		return cached, nil
	}

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

		err := archive.foreach(loadNone, func(hdr *proto.ObjectHeader, _ []byte, _, _ uint32) error {
			if err := ctx.Err(); err != nil {
				return err
			}

			if t == proto.ObjectType_INVALID || hdr.Type == t {
				return fn(hdr)
			}

			return nil
		})
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

		ps.logger.Debug("reading archive", "archive", archive.name)
		err := archive.foreach(pred, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
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
		})

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

	index, err := a.CloseWriter()
	ps.archiveSemaphore.Release(1)

	if index == nil {
		ws.fail(err)
		return err
	}

	indexErr := ps.index.IndexArchive(ws.archiveInfo(a.name), index)
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

func (ps *PackStorage) deleteArchiveFiles(name string) {
	err := ps.storage.Delete(name + IndexExt)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		ps.logger.Warn("deleting index failed", "archive", name, "err", err)
	}

	err = ps.storage.Delete(name + ArchiveSuffix)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		ps.logger.Warn("deleting archive failed", "archive", name, "err", err)
	}

	err = ps.storage.Delete(name + GCExt)
	if err != nil && !notExist(err) {
		ps.logger.Warn("deleting gc result failed", "archive", name, "err", err)
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

	if !known && ParsePlacement(name).Kind == PlacementSession && !ps.hasIndexFile(name) {
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

	if !known {
		idx, err := a.getIndex()
		if err != nil {
			return nil, err
		}

		ps.logger.Info("indexing archive", "archive", name)
		info = ArchiveInfo{Name: name}
		err = ps.index.IndexArchive(info, idx)
		if err != nil {
			return nil, err
		}
	}

	a.state = info.State
	a.session = info.Session

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

		if full {
			ps.logger.Debug("finalizing full archive", "archive", ws.archive.name)
			err := ps.finalizeLocked(ws)
			if err != nil {
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

		ps.mtx.Lock()
		ps.archives = append(ps.archives, a)
		ps.mtx.Unlock()

		ws.archive = a
	}

	ws.lastWrite = time.Now()

	return writer(ws.archive)
}

func (ps *PackStorage) withReadLock(do func()) {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	do()
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
	matches, err := ps.storage.List(ArchiveSuffix)
	if err != nil {
		return errors.Wrap(err, "failed listing archive names")
	}

	sem := semaphore.NewWeighted(IndexOpenerThreads)
	group, ctx := errgroup.WithContext(context.Background())

	for _, match := range matches {
		if err = sem.Acquire(ctx, 1); err != nil {
			break
		}

		name := strings.TrimSuffix(match, ArchiveSuffix)

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

	return nil
}

// File is one archive, index or gc file held by an ArchiveStorage.
//
//go:generate go run github.com/vektra/mockery/v2 --name File --inpackage --testonly --outpkg pack
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer

	Stat() (fs.FileInfo, error)
}

// ArchiveStorage holds a store's files under slash-separated names; Open of
// a missing name is ErrFileNotFound.
//
//go:generate go run github.com/vektra/mockery/v2 --name ArchiveStorage --inpackage --testonly --outpkg pack
type ArchiveStorage interface {
	Create(name string) (File, error)
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
	LookupArchive(archive string) (ArchiveInfo, bool, error)
	// IndexArchive registers an archive; a pending one needs a live session.
	IndexArchive(archive ArchiveInfo, index IndexFile) error
	DeleteArchive(archive string, index IndexFile) error
	Close() error
	CountObjects() (uint64, uint64, error)
}
