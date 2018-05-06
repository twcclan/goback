package pack

import (
	"context"
	"io"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	humanize "github.com/dustin/go-humanize"
	"github.com/pkg/errors"
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

func NewPackStorage(storage ArchiveStorage) *PackStorage {
	return &PackStorage{
		archives:         make([]archive, 0),
		activeArchives:   make(map[string]archive),
		idle:             make(chan *archive),
		archiveSemaphore: semaphore.NewWeighted(int64(storage.MaxParallel())),
		storage:          storage,
		index:            make(index, 0),
	}
}

type PackStorage struct {
	archives         []archive
	activeArchives   map[string]archive
	idle             chan *archive
	base             string
	mtx              sync.RWMutex
	archiveSemaphore *semaphore.Weighted
	storage          ArchiveStorage
	index            index
}

var _ backup.ObjectStore = (*PackStorage)(nil)

func (ps *PackStorage) Put(object *proto.Object) error {
	// don't store objects we already know about
	// TODO: add bloom filter
	if ps.Has(object.Ref()) {
		return nil
	}

	return ps.put(object)
}

func (ps *PackStorage) put(object *proto.Object) error {
	a, err := ps.getWritableArchive()
	if err != nil {
		return errors.Wrap(err, "Couldn't get archive for writing")
	}

	return a.Put(object)
}

func (ps *PackStorage) putRaw(hdr *proto.ObjectHeader, bytes []byte) error {
	a, err := ps.getWritableArchive()
	if err != nil {
		return errors.Wrap(err, "Couldn't get archive for writing")
	}

	return a.putRaw(hdr, bytes)
}

// indexLocation returns the indexRecord for the provided ref or nil if it's
// not in this store.
// You need to hold a read lock before calling this
func (ps *PackStorage) indexLocation(ref *proto.Ref) *indexRecord {
	loc := ps.index.lookup(ref)
	if loc != nil {
		return loc
	}

	for i := range ps.archives {
		a := &ps.archives[i]
		a.mtx.RLock()

		if !ps.archives[i].readOnly {
			loc, ok := a.writeIndex[string(ref.Sha1)]
			a.mtx.RUnlock()

			if ok {
				return loc
			}

		} else {
			a.mtx.RUnlock()
		}
	}

	return nil
}

func (ps *PackStorage) Has(ref *proto.Ref) (has bool) {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	return ps.indexLocation(ref) != nil
}

func (ps *PackStorage) Get(ref *proto.Ref) (*proto.Object, error) {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	rec := ps.indexLocation(ref)
	if rec != nil {
		a := &ps.archives[rec.Pack]

		if a.closeBeforeRead {
			// need to release lock here so we can safely close the archive
			ps.mtx.RUnlock()
			err := ps.closeArchive(a)
			if err != nil {
				return nil, err
			}
		}

		return a.getRaw(ref, rec)
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

	for i := range ps.archives {
		log.Printf("Reading archive: %s", ps.archives[i].name)
		err := ps.archives[i].foreach(pred, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
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

// hasExcept checks if a given ref exists in an archive that is not in
// the provided map of exclusions
func (ps *PackStorage) hasExcept(ref *proto.Ref, exclude map[string]bool) bool {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	loc := ps.indexLocation(ref)
	if loc != nil {
		return !exclude[ps.archives[loc.Pack].name]
	}

	return false
}

func (ps *PackStorage) closeArchive(a *archive) error {
	return nil
}

func (ps *PackStorage) Close() error {
	var (
		current    []*archive
		candidates []*archive
		obsolete   []*archive
		total      uint64
	)

	ps.mtx.Lock()
	for i := range ps.archives {
		current = append(current, &ps.archives[i])
	}
	ps.mtx.Unlock()

	if _, ok := ps.storage.(NeedCompaction); ok {
		// check if we could do a compaction here
		for _, archive := range current {
			if archive.size < ps.storage.MaxSize() && archive.readOnly {
				// this may be a candidate for compaction
				candidates = append(candidates, archive)
				total += archive.size
			}
		}

		// use some heuristic to decide whether we should do a compaction
		if len(candidates) > 10 && total > ps.storage.MaxSize()/4 {
			log.Printf("Compacting %d archives with %s total size", len(candidates), humanize.Bytes(total))

			exclusions := make(map[string]bool)
			// build the list of archives to exclude when checking for duplicates.
			// we do not want to consider archives that may potentially be removed
			for _, archive := range candidates {
				exclusions[archive.name] = true
			}

			for _, archive := range candidates {
				err := archive.foreach(loadAll, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
					// if it still exists in another file, it means that it is a duplicate
					if ps.hasExcept(hdr.Ref, exclusions) {
						return nil
					}

					return ps.putRaw(hdr, bytes)
				})

				if err != nil {
					return err
				}

				// we don't care about the error here, we're about to delete it anyways
				archive.Close()
				obsolete = append(obsolete, archive)
			}
		}
	}

	ps.mtx.Lock()
	for i := range ps.archives {
		if !ps.archives[i].readOnly {
			err := ps.archives[i].CloseWriter()
			if err != nil {
				ps.mtx.Unlock()
				return errors.Wrap(err, "Failed closing archive")
			}
		}
	}
	ps.mtx.Unlock()

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

			a, aErr := openArchive(ps.storage, name)
			if aErr != nil {
				return aErr
			}

			ps.mtx.Lock()
			a.number = uint16(len(ps.archives))
			ps.archives = append(ps.archives, a)
			ps.index = append(ps.index, a.readIndex...)
			ps.markIdle(&ps.archives[a.number])
			ps.mtx.Unlock()

			return nil
		})
	}

	err = group.Wait()
	if err != nil {
		return err
	}

	ps.mtx.Lock()
	sort.Sort(ps.index)
	ps.mtx.Unlock()
	return nil
}

func (ps *PackStorage) markIdle(a *archive) {
	go func() {
		ps.idle <- a
	}()
}

func (ps *PackStorage) addArchive() error {
	a, err := newArchive(ps.storage)
	if err != nil {
		return err
	}

	ps.mtx.Lock()

	a.number = uint16(len(ps.archives))
	ps.archives = append(ps.archives, a)
	ps.markIdle(&ps.archives[a.number])

	ps.mtx.Unlock()

	return nil
}

func (ps *PackStorage) getWritableArchive() (*archive, error) {
	for {
		select {
		// try to grab an idle open archive
		case a := <-ps.idle:
			// close this archive if it's full and retry
			if a.size >= ps.storage.MaxSize() {
				log.Printf("Closing archive because it's full: %s", a.name)
				err := a.CloseWriter()
				if err != nil {
					return nil, err
				}

				ps.mtx.Lock()
				ps.index = append(ps.index, a.readIndex...)
				a.readIndex = nil
				ps.mtx.Unlock()

				ps.archiveSemaphore.Release(1)

				continue
			}

			return a, nil
		case <-time.After(10 * time.Millisecond):
			if ps.archiveSemaphore.TryAcquire(1) {
				ps.addArchive()
			}
		}
	}
}

//go:generate mockery -name File -inpkg -testonly -outpkg pack
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer

	Stat() (os.FileInfo, error)
}

type CloseBeforeRead interface {
	CloseBeforeRead()
}

type NeedCompaction interface {
	NeedCompaction()
}

//go:generate mockery -name ArchiveStorage -inpkg -testonly -outpkg pack
type ArchiveStorage interface {
	Create(name string) (File, error)
	Open(name string) (File, error)
	List() ([]string, error)
	Delete(name string) error
	MaxSize() uint64
	MaxParallel() uint64
}
