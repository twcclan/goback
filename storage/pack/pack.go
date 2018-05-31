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

	// all of these are guarded by mtx
	mtx      sync.RWMutex
	archives map[string]*archive

	compactorMtx sync.Mutex
}

var _ backup.ObjectStore = (*PackStorage)(nil)

func (ps *PackStorage) Put(object *proto.Object) error {
	return ps.put(object)
}

func (ps *PackStorage) put(object *proto.Object) error {
	err := ps.withWritableArchive(func(a *archive) error {
		return a.Put(object)
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

func (ps *PackStorage) putRaw(hdr *proto.ObjectHeader, bytes []byte) error {
	return ps.withWritableArchive(func(a *archive) error {
		return a.putRaw(hdr, bytes)
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

func (ps *PackStorage) Has(ref *proto.Ref) (has bool) {
	_, rec := ps.indexLocation(ref)

	return rec != nil
}

func (ps *PackStorage) Get(ref *proto.Ref) (*proto.Object, error) {
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

		return archive.getRaw(ref, rec)
	}

	return nil, nil
}

func (ps *PackStorage) Delete(ref *proto.Ref) error {
	panic("not implemented")
}

func (ps *PackStorage) Walk(load bool, t proto.ObjectType, fn backup.ObjectReceiver) error {
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

// unloadArchive removes the provided archive from this storage (e.g. after it is not needed anymore).
// needs an exclusive lock
func (ps *PackStorage) unloadArchive(a *archive) error {
	return nil
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

func (ps *PackStorage) doCompaction() error {
	ps.compactorMtx.Lock()
	defer ps.compactorMtx.Unlock()

	var (
		candidates []*archive
		obsolete   []*archive
		total      uint64
	)

	ps.mtx.RLock()
	for _, candidate := range ps.archives {
		candidate.mtx.RLock()
		if candidate.size < ps.maxSize && candidate.readOnly {
			// this may be a candidate for compaction
			candidates = append(candidates, candidate)
			total += candidate.size
		}
		candidate.mtx.RUnlock()
	}
	ps.mtx.RUnlock()

	//var currentArchive archive
	getArchiveForCompaction := func() (*archive, error) {
		/*var err error
		if currentArchive == nil {
			currentArchive, err = newArchive(ps.storage)
		}*/
		return nil, nil
	}

	// use some heuristic to decide whether we should do a compaction
	if len(candidates) > 10 && total > ps.maxSize/4 {
		log.Printf("Compacting %d archives with %s total size", len(candidates), humanize.Bytes(total))

		for _, archive := range candidates {
			err := archive.foreach(loadAll, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
				// TODO: do some checks here like:
				// * duplicates
				// * deletions

				ar, err := getArchiveForCompaction()
				if err != nil {
					return err
				}

				return ar.putRaw(hdr, bytes)
			})

			if err != nil {
				return err
			}

			// we don't really care about the error here, we're about to delete it anyways
			err = archive.Close()
			if err != nil {
				log.Printf("Warning: failed to close archive after compaction: %v", err)
			}
			obsolete = append(obsolete, archive)
		}
	}

	err := ps.Flush()
	if err != nil {
		return err
	}

	// after having safely closed the active file(s) we can delete the left-overs
	for _, archive := range obsolete {
		err := ps.storage.Delete(archive.indexName())
		if err != nil {
			log.Printf("Failed deleting index %s after compaction", archive.name)
		}

		err = ps.storage.Delete(archive.archiveName())
		if err != nil {
			log.Printf("Failed deleting archive %s after compaction", archive.name)
		}
	}

	return nil
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

			grp.Go(func() error {
				return ps.finalizeArchive(archive)
			})
		}
	})

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
