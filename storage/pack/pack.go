package pack

// TODO: locking is a big mess right now, find a better way of handling concurrency

import (
	"context"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	humanize "github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	"github.com/willf/bloom"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

const (
	IndexOpenerThreads = 10
	ArchiveSuffix      = ".goback"
	ArchivePattern     = "*" + ArchiveSuffix
	IndexExt           = ".idx"
	varIntMaxSize      = 10
	bloomFilterM       = 150000
	bloomFilterK       = 7
)

func NewPackStorage(options ...PackOption) (*PackStorage, error) {
	opts := &packOptions{
		compaction:  true,
		maxParallel: 1,
		maxSize:     1024 * 1024 * 1024,
	}

	for _, opt := range options {
		opt(opts)
	}

	if opts.storage == nil {
		return nil, errors.New("No archive storage provided")
	}

	return &PackStorage{
		archives:         make(map[string]*archive),
		writable:         make(chan *archive, opts.maxParallel),
		archiveSemaphore: semaphore.NewWeighted(int64(opts.maxParallel)),
		storage:          opts.storage,
		compaction:       opts.compaction,
		maxSize:          opts.maxSize,
		closeBeforeRead:  opts.closeBeforeRead,
	}, nil
}

type PackStorage struct {
	writable         chan *archive
	archiveSemaphore *semaphore.Weighted
	storage          ArchiveStorage
	compaction       bool
	maxSize          uint64
	closeBeforeRead  bool
	cache            backup.ObjectStore

	// all of these are guarded by mtx
	mtx      sync.RWMutex
	archives map[string]*archive

	compactorMtx sync.Mutex
}

var _ backup.ObjectStore = (*PackStorage)(nil)

func (ps *PackStorage) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	_, loc := ps.indexLocation(ref)

	return loc != nil, nil
}

func (ps *PackStorage) cachable(obj *proto.Object) bool {
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
	if ps.cache != nil && err == nil && ps.cachable(obj) {
		ps.cache.Put(ctx, obj)
	}

	return err
}

func (ps *PackStorage) putReadCache(ctx context.Context) func(*proto.Object, error) (*proto.Object, error) {
	return func(obj *proto.Object, err error) (*proto.Object, error) {
		if ps.cache != nil && err == nil && ps.cachable(obj) {
			ps.cache.Put(ctx, obj)
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

func (ps *PackStorage) Put(ctx context.Context, object *proto.Object) error {
	return ps.putWriteCache(ctx, object, ps.put(ctx, object))
}

func (ps *PackStorage) put(ctx context.Context, object *proto.Object) error {
	err := ps.withWritableArchive(func(a *archive) error {
		return a.Put(ctx, object)
	})

	if err != nil {
		return err
	}

	// if this was a commit run a flush operation, to make sure all the data is safely stored
	// and also because we expect to be reading a lot of objects for indexing purposes
	if object.Type() == proto.ObjectType_COMMIT {
		log.Printf("Flushing after commit")
		return ps.Flush()
	}

	return nil
}

func (ps *PackStorage) putRaw(ctx context.Context, hdr *proto.ObjectHeader, bytes []byte) error {
	return ps.withWritableArchive(func(a *archive) error {
		return a.putRaw(ctx, hdr, bytes)
	})
}

// indexLocation returns the indexRecord for the provided ref or nil if it's
// not in this store.
func (ps *PackStorage) indexLocation(ref *proto.Ref) (*archive, *indexRecord) {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	locations := bloom.Locations(ref.Sha1, bloomFilterK)

	for _, archive := range ps.archives {
		if rec := archive.indexLocation(ref, locations); rec != nil {
			return archive, rec
		}
	}

	return nil, nil
}

// indexLocationExcept checks if a given ref exists in an archive that is not in
// the provided map of exclusions
func (ps *PackStorage) indexLocationExcept(ref *proto.Ref, exclude map[string]bool) (*archive, *indexRecord) {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	locations := bloom.Locations(ref.Sha1, bloomFilterK)

	for name, archive := range ps.archives {
		if rec := archive.indexLocation(ref, locations); rec != nil {
			if exclude == nil || !exclude[name] {
				return archive, rec
			}
		}
	}

	return nil, nil
}

func (ps *PackStorage) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	cached := ps.getCache(ctx, ref)
	if cached != nil {
		return cached, nil
	}

	archive, rec := ps.indexLocation(ref)
	if rec != nil {
		archive.mtx.RLock()
		needClose := !archive.readOnly && ps.closeBeforeRead
		archive.mtx.RUnlock()

		if needClose {
			log.Printf("Need to close archive %s before reading object %x", archive.name, ref.Sha1)
			err := ps.finalizeArchive(archive)

			if err != nil {
				return nil, err
			}
		}

		return ps.putReadCache(ctx)(archive.getRaw(ctx, ref, rec))
	}

	return nil, nil
}

func (ps *PackStorage) Delete(ctx context.Context, ref *proto.Ref) error {
	panic("not implemented")
}

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
		log.Printf("Reading archive: %s", archive.name)
		err := archive.foreach(pred, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
			if t == proto.ObjectType_INVALID || hdr.Type == t {
				var obj *proto.Object
				var err error

				if load {
					obj, err = proto.NewObjectFromCompressedBytes(bytes)
					if err != nil {
						return err
					}
				}

				return fn(hdr, obj)
			}

			return nil
		})

		if err != nil {
			return err
		}
	}

	return nil
}

// unloadArchive removes the provided archive from this storage (e.g. after it is not needed anymore)
func (ps *PackStorage) unloadArchive(a *archive) {
	ps.mtx.Lock()
	delete(ps.archives, a.name)
	ps.mtx.Unlock()
}

func (ps *PackStorage) finalizeArchive(a *archive) error {
	err := a.CloseWriter()
	if err == errAlreadyClosed {
		return nil
	}

	ps.archiveSemaphore.Release(1)
	return err
}

func (ps *PackStorage) newArchive() error {
	a, err := newArchive(ps.storage)
	if err != nil {
		return err
	}

	ps.mtx.Lock()
	ps.archives[a.name] = a
	ps.putWritableArchive(a)
	ps.mtx.Unlock()

	return nil
}

func (ps *PackStorage) openArchive(name string) error {
	a, err := openArchive(ps.storage, name)
	if err != nil {
		return err
	}

	ps.mtx.Lock()
	ps.archives[a.name] = a
	ps.mtx.Unlock()

	return nil
}

func (ps *PackStorage) withWritableArchive(writer func(*archive) error) error {
	for {
		select {
		// try to grab an idle open archive
		case a := <-ps.writable:
			// lock this archive for writing
			a.mtx.RLock()
			readOnly := a.readOnly
			size := a.size
			a.mtx.RUnlock()

			// skip archives that have already been finalized
			if readOnly {
				continue
			}

			// close this archive if it's full and retry
			if size >= ps.maxSize {
				log.Printf("Closing archive because it's full: %s", a.name)
				err := ps.finalizeArchive(a)
				if err != nil {
					return err
				}

				continue
			}

			defer ps.putWritableArchive(a)

			return writer(a)
		case <-time.After(10 * time.Millisecond):
			if ps.archiveSemaphore.TryAcquire(1) {
				err := ps.newArchive()
				if err != nil {
					return err
				}
			}
		}
	}
}

func (ps *PackStorage) putWritableArchive(ar *archive) {
	ps.writable <- ar
}

func (ps *PackStorage) calculateWaste(a *archive) float64 {
	objects := make(map[string]bool)
	var total, waste uint64

	for _, record := range a.readIndex {
		total += uint64(record.Length)
		objRefString := string(record.Sum[:])

		if objects[objRefString] {
			waste += uint64(record.Length)
		} else {
			objects[objRefString] = true
		}
	}

	log.Print(len(objects), len(a.readIndex))

	return float64(waste) / float64(total)
}

func (ps *PackStorage) doCompaction() error {
	ps.compactorMtx.Lock()
	defer ps.compactorMtx.Unlock()

	ctx := context.Background()

	var (
		candidates []*archive
		obsolete   []*archive
		total      uint64
	)

	ps.mtx.RLock()
	for _, candidate := range ps.archives {
		candidate.mtx.RLock()

		// we only care about finalized archives
		if candidate.readOnly && (candidate.size < ps.maxSize || ps.calculateWaste(candidate) > 0.1) {
			// this may be a candidate for compaction
			candidates = append(candidates, candidate)
			total += candidate.size
		}
		candidate.mtx.RUnlock()
	}
	ps.mtx.RUnlock()

	closeArchive := func(a *archive) error {
		err := a.CloseWriter()
		if err != nil && err != errAlreadyClosed {
			return err
		}

		ps.mtx.Lock()
		ps.archives[a.name] = a
		ps.mtx.Unlock()

		return nil
	}

	var openArchive *archive
	getArchive := func() (*archive, error) {
		var err error

		// if the currently open archive is full, finalize it
		if openArchive != nil && openArchive.size >= ps.maxSize {
			err = closeArchive(openArchive)
			if err != nil {
				return nil, err
			}

			openArchive = nil
		}

		if openArchive == nil {
			openArchive, err = newArchive(ps.storage)
		}

		return openArchive, err
	}

	// use some heuristic to decide whether we should do a compaction
	if len(candidates) > 10 {
		log.Printf("Compacting %d archives with %s total size", len(candidates), humanize.Bytes(total))

		var droppedObjects, droppedSize uint64

		// a map with all the archives that we are looking to compact
		exceptions := make(map[string]bool)
		for _, archive := range candidates {
			exceptions[archive.name] = true
		}

		// a map of all the objects we've already written to a new archive
		written := make(map[string]bool)

		for _, archive := range candidates {
			err := archive.foreach(loadAll, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
				// TODO: do some more checks here like:
				// * deletions
				// * garbage collection

				objRefString := string(hdr.Ref.Sha1)

				// if we have already written this object during this compaction run we drop it
				if written[objRefString] {
					droppedObjects++
					droppedSize += uint64(length)
					return nil
				}

				// if we can find a location for this ref in any other archive we just drop it
				if _, rec := ps.indexLocationExcept(hdr.Ref, exceptions); rec != nil {
					droppedObjects++
					droppedSize += uint64(length)
					return nil
				}

				ar, err := getArchive()
				if err != nil {
					return err
				}

				written[objRefString] = true

				return ar.putRaw(ctx, hdr, bytes)
			})

			if err != nil {
				return err
			}
			obsolete = append(obsolete, archive)
		}

		log.Printf("Dropped %d objecst during compaction, saved %s", droppedObjects, humanize.Bytes(droppedSize))
	}

	if openArchive != nil {
		err := closeArchive(openArchive)
		if err != nil {
			return err
		}
	}

	// after having safely closed the active file(s) we can delete the left-overs
	for _, archive := range obsolete {
		ps.unloadArchive(archive)

		if e := archive.Close(); e != nil {
			log.Printf("Failed closing obsolete archive after compaction: %v", e)
		}

		err := ps.storage.Delete(archive.indexName())
		if err != nil {
			log.Printf("Failed deleting index %s after compaction: %v", archive.name, err)
		}

		err = ps.storage.Delete(archive.archiveName())
		if err != nil {
			log.Printf("Failed deleting archive %s after compaction: %v", archive.name, err)
		}
	}

	return nil
}

func (ps *PackStorage) backgroundCompaction() {
	err := ps.doCompaction()

	if err != nil {
		log.Println("Failed running compaction", err)
	} else {
		log.Print("Compaction successful")
	}
}

func (ps *PackStorage) withExclusiveLock(do func()) {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()

	do()
}

func (ps *PackStorage) withReadLock(do func()) {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	do()
}

// Flush will concurrently finalize all currently open archives
func (ps *PackStorage) Flush() error {
	grp, _ := errgroup.WithContext(context.Background())

	ps.withReadLock(func() {
		for _, archive := range ps.archives {

			a := archive
			grp.Go(func() error {
				return ps.finalizeArchive(a)
			})
		}
	})

	defer func() {
		go ps.backgroundCompaction()
	}()

	return grp.Wait()
}

func (ps *PackStorage) Close() error {
	// if ps.compaction {
	// 	return ps.doCompaction()
	// }

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

func (ps *PackStorage) Open() error {
	matches, err := ps.storage.List()
	if err != nil {
		return errors.Wrap(err, "failed listing archive names")
	}

	sem := semaphore.NewWeighted(IndexOpenerThreads)
	group, ctx := errgroup.WithContext(context.Background())

	for _, match := range matches {
		if err = sem.Acquire(ctx, 1); err != nil {
			break
		}

		name := match

		group.Go(func() error {
			defer sem.Release(1)

			return ps.openArchive(name)
		})
	}

	return group.Wait()
}

//go:generate mockery -name File -inpkg -testonly -outpkg pack
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer

	Stat() (os.FileInfo, error)
}

//go:generate mockery -name ArchiveStorage -inpkg -testonly -outpkg pack
type ArchiveStorage interface {
	Create(name string) (File, error)
	Open(name string) (File, error)
	List() ([]string, error)
	Delete(name string) error
}
