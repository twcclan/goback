package pack

// TODO: locking is a big mess right now, find a better way of handling concurrency

import (
	"context"
	"io"
	"io/fs"
	"log"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	"go.opencensus.io/trace"
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
	ErrFileNotFound = errors.New("requested file was not found")
)

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

	if opts.storage == nil {
		return nil, errors.New("No archive storage provided")
	}

	if opts.index == nil {
		return nil, errors.New("No archive index provided")
	}

	return &PackStorage{
		archives:         make([]*archive, 0),
		writable:         make(chan *archive, opts.maxParallel),
		archiveSemaphore: semaphore.NewWeighted(int64(opts.maxParallel)),
		storage:          opts.storage,
		compaction:       opts.compaction,
		maxSize:          opts.maxSize,
		closeBeforeRead:  opts.closeBeforeRead,
		cache:            opts.cache,
		index:            opts.index,
	}, nil
}

type PackStorage struct {
	writable         chan *archive
	archiveSemaphore *semaphore.Weighted
	storage          ArchiveStorage
	compaction       CompactionConfig
	maxSize          uint64
	closeBeforeRead  bool
	cache            backup.ObjectStore
	index            ArchiveIndex

	// all of these are guarded by mtx
	mtx      sync.RWMutex
	archives []*archive

	compactorMtx     sync.Mutex
	compactorTicker  *time.Ticker
	compactorClose   chan struct{}
	compactorRunning bool
}

var _ backup.ObjectStore = (*PackStorage)(nil)
var _ backup.Counter = (*PackStorage)(nil)

func (ps *PackStorage) Has(ctx context.Context, ref *proto.Ref) (bool, error) {
	// can't guarantee anything while compaction is in progress.
	// potential duplicates will be removed during later compaction runs
	if ps.compactorRunning {
		return false, nil
	}

	_, loc := ps.indexLocation(ctx, ref)

	return loc != nil, nil
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

func (ps *PackStorage) Count() (uint64, uint64, error) {
	return ps.index.CountObjects()
}

func (ps *PackStorage) Put(ctx context.Context, object *proto.Object) error {
	ctx, span := trace.StartSpan(ctx, "PackStorage.Put")
	defer span.End()

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
func (ps *PackStorage) indexLocation(ctx context.Context, ref *proto.Ref) (*archive, *IndexRecord) {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	loc, err := ps.index.LocateObject(ref)
	if err != nil {
		return nil, nil
	}

	for _, archive := range ps.archives {
		if archive.name == loc.Archive {
			return archive, &loc.Record
		}

		//if rec := archive.indexLocation(ref); rec != nil {
		//return archive, rec
		//}
	}

	// go through currently opened archives
	for _, archive := range ps.archives {
		if rec := archive.indexLocation(ref); rec != nil {
			return archive, rec
		}
	}

	return nil, nil
}

// indexLocationExcept checks if a given ref exists in an archive that is not in
// the provided map of exclusions
func (ps *PackStorage) indexLocationExcept(ref *proto.Ref, exclude ...*archive) (*archive, *IndexRecord) {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	var exclusions []string
	for _, archive := range exclude {
		exclusions = append(exclusions, archive.name)
	}

	loc, err := ps.index.LocateObject(ref, exclusions...)
	if err != nil {
		return nil, nil
	}

	var hit *archive
	for _, arc := range ps.archives {
		if arc.name == loc.Archive {
			return hit, &loc.Record
		}
	}

	return nil, nil
}

func (ps *PackStorage) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	ctx, span := trace.StartSpan(ctx, "PackStorage.Get")
	defer span.End()

	cached := ps.getCache(ctx, ref)
	if cached != nil {
		return cached, nil
	}

	archive, rec := ps.indexLocation(ctx, ref)
	if rec != nil {
		archive.mtx.RLock()
		needClose := !archive.readOnly && ps.closeBeforeRead
		archive.mtx.RUnlock()

		if needClose {
			span.AddAttributes(trace.BoolAttribute("close-before-read", true))
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
	return ps.withWritableArchive(func(a *archive) error {
		return a.putTombstone(ctx, ref)
	})
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

// unloadArchive removes the provided archive from this storage (e.g. after it is not needed anymore)
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

func (ps *PackStorage) finalizeArchive(a *archive) error {
	index, err := a.CloseWriter()
	if err == errAlreadyClosed {
		return nil
	}

	err = ps.index.IndexArchive(a.name, index)
	if err != nil {
		return err
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
	ps.archives = append(ps.archives, a)
	ps.putWritableArchive(a)
	ps.mtx.Unlock()

	return nil
}

func (ps *PackStorage) openArchive(name string) error {
	a, err := openArchive(ps.storage, name)
	if err != nil {
		return err
	}

	hasArchive, err := ps.index.HasArchive(name)
	if err != nil {
		return err
	}

	if !hasArchive {
		idx, err := a.getIndex()
		if err != nil {
			return err
		}

		log.Printf("indexing archive %s", name)
		err = ps.index.IndexArchive(name, idx)
		if err != nil {
			return err
		}
	}

	ps.mtx.Lock()
	ps.archives = append(ps.archives, a)
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

//func (ps *PackStorage) doMark() error {
//	// TODO:
//	//  * restrict reachability tests to selected archives
//
//	start := time.Now()
//	defer func() {
//		log.Printf("Finished gc mark phase after %s", time.Since(start))
//		stats.Record(context.Background(), GCMarkTime.M(float64(time.Since(start))/float64(time.Millisecond)))
//	}()
//
//	var archives []*archive
//	ps.withReadLock(func() {
//		// get a list of all finalized archives
//		for _, archive := range ps.archives {
//			archive.mtx.Lock()
//			if archive.readOnly {
//				archive.gcBits.ClearAll()
//				archives = append(archives, archive)
//			}
//			archive.mtx.Unlock()
//		}
//	})
//
//	refs := make(chan *proto.Ref, runtime.NumCPU()*2)
//
//	// run parallel mark
//	group, ctx := errgroup.WithContext(context.Background())
//	for i := 0; i < runtime.NumCPU()/2; i++ {
//		group.Go(func() error {
//			for ref := range refs {
//				startCommit := time.Now()
//				err := ps.markRecursively(ref)
//				stats.Record(context.Background(), GCCommitMarkLatency.M(float64(time.Since(startCommit))/float64(time.Millisecond)))
//
//				if err != nil {
//					log.Printf("Couldn't run garbage collector mark step on ref %x: %s", ref.Sha1, err)
//					return err
//				}
//			}
//
//			return nil
//		})
//	}
//
//	for _, arch := range archives {
//		for i := range arch.readIndex {
//			if proto.ObjectType(arch.readIndex[i].Type) == proto.ObjectType_COMMIT {
//				select {
//				case refs <- &proto.Ref{Sha1: arch.readIndex[i].Sum[:]}:
//				case <-ctx.Done():
//					close(refs)
//					return ctx.Err()
//				}
//			}
//		}
//	}
//
//	close(refs)
//
//	return group.Wait()
//}

//func (ps *PackStorage) markRecursively(ref *proto.Ref) error {
//	arch, loc := ps.indexLocation(context.Background(), ref)
//
//	// skip already marked objects
//	idx, _ := arch.readIndex.lookup(ref)
//
//	arch.mtx.RLock()
//	skip := !arch.readOnly || arch.gcBits.Test(idx)
//	arch.mtx.RUnlock()
//
//	if skip {
//		return nil
//	}
//
//	stats.Record(context.Background(), GCObjectsScanned.M(1))
//	arch.mtx.Lock()
//	arch.gcBits.Set(idx)
//	arch.mtx.Unlock()
//
//	// no need to recurse into blobs
//	if proto.ObjectType(loc.Type) == proto.ObjectType_BLOB {
//		return nil
//	}
//
//	obj, err := ps.Get(context.Background(), ref)
//	if err != nil {
//		return err
//	}
//
//	switch obj.Type() {
//	// mark the tree of the commit
//	case proto.ObjectType_COMMIT:
//		return ps.markRecursively(obj.GetCommit().Tree)
//	// for trees we need to mark each individual node
//	case proto.ObjectType_TREE:
//		for _, node := range obj.GetTree().Nodes {
//			err = ps.markRecursively(node.Ref)
//			if err != nil {
//				return err
//			}
//
//		}
//	// and for a file we mark each part
//	case proto.ObjectType_FILE:
//		for _, part := range obj.GetFile().Parts {
//			err = ps.markRecursively(part.Ref)
//			if err != nil {
//				return err
//			}
//		}
//
//		for _, split := range obj.GetFile().GetSplits() {
//			err = ps.markRecursively(split)
//			if err != nil {
//				return err
//			}
//		}
//	}
//
//	return nil
//}
//
//func (ps *PackStorage) isObjectReachable(ref *proto.Ref) bool {
//	// find archive that is holding the ref
//	a, _ := ps.indexLocation(context.Background(), ref)
//	if a == nil {
//		return false
//	}
//
//	a.mtx.RLock()
//	defer a.mtx.RUnlock()
//
//	// assume not finalized objects are reachable
//	if !a.readOnly {
//		return true
//	}
//
//	idx, _ := a.readIndex.lookup(ref)
//
//	return a.gcBits.Test(idx)
//}

func (ps *PackStorage) doCompaction() error {
	ps.compactorMtx.Lock()
	defer ps.compactorMtx.Unlock()

	ps.compactorRunning = true
	defer func() {
		ps.compactorRunning = false
	}()

	ctx := context.Background()

	//if ps.compaction.GarbageCollection {
	//	err := ps.doMark()
	//	if err != nil {
	//		return errors.Wrap(err, "failed gc mark phase")
	//	}
	//
	//	var total, reachable int
	//
	//	ps.withReadLock(func() {
	//		for _, a := range ps.archives {
	//			a.mtx.RLock()
	//
	//			if a.gcBits != nil {
	//				total += len(a.readIndex)
	//				reachable += int(a.gcBits.Count())
	//			}
	//
	//			a.mtx.RUnlock()
	//		}
	//	})
	//
	//	log.Printf("object reachability %d/%d (%f %%)", reachable, total, float64(reachable)/float64(total)*100)
	//}

	var (
		candidates []*archive
		obsolete   []*archive
		total      uint64
		//totalObjects int64
	)

	ps.mtx.RLock()
	for _, candidate := range ps.archives {
		candidate.mtx.RLock()

		if candidate.readOnly {
			//totalObjects += int64(len(candidate.readIndex))
		}

		// we only care about finalized archives
		if candidate.readOnly && candidate.size < ps.maxSize {
			// this may be a candidate for compaction
			candidates = append(candidates, candidate)
			total += candidate.size
		}
		candidate.mtx.RUnlock()
	}
	ps.mtx.RUnlock()

	//stats.Record(ctx, TotalLiveObjects.M(totalObjects))

	closeArchive := func(a *archive) error {
		_, err := a.CloseWriter()
		if err != nil && err != errAlreadyClosed {
			return err
		}

		ps.mtx.Lock()
		ps.archives = append(ps.archives, a)
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
	if len(candidates) > ps.compaction.MinimumCandidates || total >= ps.maxSize {
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
				if _, rec := ps.indexLocationExcept(hdr.Ref, candidates...); rec != nil {
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

		idx, err := archive.getIndex()
		if err != nil {
			log.Printf("Failed getting archive index for deletion: %s", err)
			continue
		}

		err = ps.index.DeleteArchive(archive.name, idx)
		if err != nil {
			log.Printf("Failed removing local index %s after compaction: %s", archive.name, err)
		}

		err = ps.storage.Delete(archive.indexName())
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
	log.Println("Running compaction")
	err := ps.doCompaction()

	if err != nil {
		log.Println("Failed running compaction", err)
	} else {
		log.Print("Compaction successful")
	}
}

func (ps *PackStorage) periodicCompaction() {
	for {
		select {
		case <-ps.compactorTicker.C:
			ps.backgroundCompaction()
		case <-ps.compactorClose:
			break
		}
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

	if ps.compaction.AfterFlush {
		defer func() {
			go ps.backgroundCompaction()
		}()
	}

	return grp.Wait()
}

func (ps *PackStorage) Close() error {
	if ps.compaction.OnClose {
		// this wil block, but only log potential errors
		ps.backgroundCompaction()
	}

	if ps.compactorTicker != nil {
		ps.compactorTicker.Stop()
		close(ps.compactorClose)
		ps.compactorTicker = nil
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

	if ps.compaction.Periodically > time.Duration(0) {
		ps.compactorTicker = time.NewTicker(ps.compaction.Periodically)
		ps.compactorClose = make(chan struct{})

		go ps.periodicCompaction()
	}

	if ps.compaction.OnOpen {
		defer func() {
			go ps.backgroundCompaction()
		}()
	}

	return group.Wait()
}

//go:generate mockery --name File --inpackage --testonly --outpkg pack
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer

	Stat() (fs.FileInfo, error)
}

//go:generate mockery --name ArchiveStorage --inpackage --testonly --outpkg pack
type ArchiveStorage interface {
	Create(name string) (File, error)
	Open(name string) (File, error)
	List() ([]string, error)
	Delete(name string) error
}

type IndexLocation struct {
	Archive string
	Record  IndexRecord
}

var (
	ErrRecordNotFound = errors.New("couldn't find index record")
)

type ArchiveIndex interface {
	LocateObject(ref *proto.Ref, exclude ...string) (IndexLocation, error)
	HasArchive(archive string) (bool, error)
	IndexArchive(archive string, index IndexFile) error
	DeleteArchive(archive string, index IndexFile) error
	Close() error
	CountObjects() (uint64, uint64, error)
}
