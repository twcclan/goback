package pack

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"io"
	"io/fs"
	"sync"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/pkg/errors"
)

const (
	ArchiveSuffix  = ".goback"
	ArchivePattern = "*" + ArchiveSuffix
	IndexExt       = ".idx"
	varIntMaxSize  = 10
)

var (
	ErrFileNotFound            = errors.New("requested file was not found")
	ErrInvalidExtension        = errors.New("the provided extension is invalid")
	ErrTransactionsUnsupported = errors.New("backing storage does not support transactions")
)

func New(options ...PackOption) (*Store, error) {
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
		return nil, errors.New("No archiveWriter storage provided")
	}

	if opts.index == nil {
		return nil, errors.New("No archiveWriter locator provided")
	}

	writer := NewWriter(opts.storage, opts.index, opts.maxParallel, opts.maxSize)

	return &Store{
		Reader: NewReader(opts.storage, opts.index),
		Writer: writer,
	}, nil
}

type Store struct {
	*Reader
	*Writer

	writerMtx sync.RWMutex

	//compactorMtx     sync.Mutex
	//compactorTicker  *time.Ticker
	//compactorClose   chan struct{}
	//compactorRunning bool
}

var _ backup.ObjectStore = (*Store)(nil)
var _ backup.Transactioner = (*Store)(nil)

func (s *Store) Flush() error {
	s.writerMtx.Lock()
	defer s.writerMtx.Unlock()

	writer := NewWriter(s.Writer.storage, s.Writer.index, s.Writer.maxParallel, s.Writer.maxSize)

	err := s.Writer.Close()
	if err != nil {
		return err
	}

	s.Writer = writer
	return nil
}

func (s *Store) Put(ctx context.Context, object *proto.Object) error {
	s.writerMtx.RLock()
	defer s.writerMtx.RUnlock()

	return s.Writer.Put(ctx, object)
}

func (s *Store) Begin(ctx context.Context) (backup.Transaction, error) {
	if p, ok := s.Writer.storage.(Parent); ok {
		txId, err := uuid.NewRandom()
		if err != nil {
			return nil, fmt.Errorf("couldn't create transaction id: %w", err)
		}

		nested, err := p.Child(txId.String())
		if err != nil {
			return nil, err
		}

		txWriter := NewWriter(nested, &nopIndexer{}, s.Writer.maxParallel, s.Writer.maxSize)

		return &packTx{Writer: txWriter, store: s}, nil
	}

	return nil, ErrTransactionsUnsupported
}

type nopIndexer struct{}

func (n *nopIndexer) IndexArchive(name string, index IndexFile) error { return nil }

type nopLocator struct{}

func (n *nopLocator) LocateObject(ref *proto.Ref, exclude ...string) (IndexLocation, error) {
	return IndexLocation{}, nil
}

var _ backup.Transaction = (*packTx)(nil)

type packTx struct {
	*Writer

	store *Store
}

func (p *packTx) Commit(ctx context.Context) error {
	// close the tx writer first
	err := p.Writer.Close()
	if err != nil {
		return err
	}

	// create a new writer for the main storage
	writer := NewWriter(p.store.Writer.storage, p.index, 1, p.maxSize)
	defer writer.Close()

	reader := NewReader(p.Writer.storage, &nopLocator{})
	err = reader.WriteTo(ctx, writer)
	if err != nil {
		return err
	}

	return writer.Close()
}

func (p *packTx) Rollback() error {
	err := p.Writer.Close()
	if err != nil {
		return err
	}

	return p.storage.DeleteAll()
}

//
//func (ps *Store) cacheable(obj *proto.Object) bool {
//	if obj == nil {
//		return false
//	}
//
//	switch obj.Type() {
//	case proto.ObjectType_COMMIT, proto.ObjectType_TREE, proto.ObjectType_FILE:
//		return true
//	}
//
//	return false
//}
//
//func (ps *Store) putWriteCache(ctx context.Context, obj *proto.Object, err error) error {
//	if ps.cache != nil && err == nil && ps.cacheable(obj) {
//		_ = ps.cache.Put(ctx, obj)
//	}
//
//	return err
//}
//
//func (ps *Store) putReadCache(ctx context.Context) func(*proto.Object, error) (*proto.Object, error) {
//	return func(obj *proto.Object, err error) (*proto.Object, error) {
//		if ps.cache != nil && err == nil && ps.cacheable(obj) {
//			_ = ps.cache.Put(ctx, obj)
//		}
//
//		return obj, err
//	}
//}
//
//func (ps *Store) getCache(ctx context.Context, ref *proto.Ref) *proto.Object {
//	if ps.cache != nil {
//		obj, err := ps.cache.Get(ctx, ref)
//		if err == nil {
//			return obj
//		}
//	}
//
//	return nil
//}

func (s *Store) Delete(ctx context.Context, ref *proto.Ref) error {
	return errors.New("not implemented")
}

//func (ps *Store) doMark() error {
//	// TODO:
//	//  * restrict reachability tests to selected archives
//
//	start := time.Now()
//	defer func() {
//		log.Printf("Finished gc mark phase after %s", time.Since(start))
//		stats.Record(context.Background(), GCMarkTime.M(float64(time.Since(start))/float64(time.Millisecond)))
//	}()
//
//	var archives []*archiveWriter
//	ps.withReadLock(func() {
//		// get a list of all finalized archives
//		for _, archiveWriter := range ps.archives {
//			archiveWriter.mtx.Lock()
//			if archiveWriter.readOnly {
//				archiveWriter.gcBits.ClearAll()
//				archives = append(archives, archiveWriter)
//			}
//			archiveWriter.mtx.Unlock()
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

//func (ps *Store) markRecursively(ref *proto.Ref) error {
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
//func (ps *Store) isObjectReachable(ref *proto.Ref) bool {
//	// find archiveWriter that is holding the ref
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

//func (ps *Store) doCompaction() error {
//	ps.compactorMtx.Lock()
//	defer ps.compactorMtx.Unlock()
//
//	ps.compactorRunning = true
//	defer func() {
//		ps.compactorRunning = false
//	}()
//
//	ctx := context.Background()
//
//	//if ps.compaction.GarbageCollection {
//	//	err := ps.doMark()
//	//	if err != nil {
//	//		return errors.Wrap(err, "failed gc mark phase")
//	//	}
//	//
//	//	var total, reachable int
//	//
//	//	ps.withReadLock(func() {
//	//		for _, a := range ps.archives {
//	//			a.mtx.RLock()
//	//
//	//			if a.gcBits != nil {
//	//				total += len(a.readIndex)
//	//				reachable += int(a.gcBits.Count())
//	//			}
//	//
//	//			a.mtx.RUnlock()
//	//		}
//	//	})
//	//
//	//	log.Printf("object reachability %d/%d (%f %%)", reachable, total, float64(reachable)/float64(total)*100)
//	//}
//
//	var (
//		candidates []*archiveWriter
//		obsolete   []*archiveWriter
//		total      uint64
//		//totalObjects int64
//	)
//
//	ps.mtx.RLock()
//	for _, candidate := range ps.archives {
//		candidate.mtx.RLock()
//
//		if candidate.readOnly {
//			//totalObjects += int64(len(candidate.readIndex))
//		}
//
//		// we only care about finalized archives
//		if candidate.readOnly && candidate.size < ps.maxSize {
//			// this may be a candidate for compaction
//			candidates = append(candidates, candidate)
//			total += candidate.size
//		}
//		candidate.mtx.RUnlock()
//	}
//	ps.mtx.RUnlock()
//
//	//stats.Record(ctx, TotalLiveObjects.M(totalObjects))
//
//	closeArchive := func(a *archiveWriter) error {
//		_, err := a.CloseWriter()
//		if err != nil && err != errAlreadyClosed {
//			return err
//		}
//
//		ps.mtx.Lock()
//		ps.archives = append(ps.archives, a)
//		ps.mtx.Unlock()
//
//		return nil
//	}
//
//	var readArchive *archiveWriter
//	getArchive := func() (*archiveWriter, error) {
//		var err error
//
//		// if the currently open archiveWriter is full, finalize it
//		if readArchive != nil && readArchive.size >= ps.maxSize {
//			err = closeArchive(readArchive)
//			if err != nil {
//				return nil, err
//			}
//
//			readArchive = nil
//		}
//
//		if readArchive == nil {
//			readArchive, err = writeArchive(ps.storage)
//		}
//
//		return readArchive, err
//	}
//
//	// use some heuristic to decide whether we should do a compaction
//	if len(candidates) > ps.compaction.MinimumCandidates || total >= ps.maxSize {
//		log.Printf("Compacting %d archives with %s total size", len(candidates), humanize.Bytes(total))
//
//		var droppedObjects, droppedSize uint64
//
//		// a map with all the archives that we are looking to compact
//		exceptions := make(map[string]bool)
//		for _, archiveWriter := range candidates {
//			exceptions[archiveWriter.name] = true
//		}
//
//		// a map of all the objects we've already written to a new archiveWriter
//		written := make(map[string]bool)
//
//		for _, archiveWriter := range candidates {
//			err := archiveWriter.foreach(loadAll, func(hdr *proto.ObjectHeader, bytes []byte, offset, length uint32) error {
//				// TODO: do some more checks here like:
//				// * deletions
//				// * garbage collection
//
//				objRefString := string(hdr.Ref.Sha1)
//
//				// if we have already written this object during this compaction run we drop it
//				if written[objRefString] {
//					droppedObjects++
//					droppedSize += uint64(length)
//					return nil
//				}
//
//				// if we can find a location for this ref in any other archiveWriter we just drop it
//				if _, rec := ps.indexLocationExcept(hdr.Ref, candidates...); rec != nil {
//					droppedObjects++
//					droppedSize += uint64(length)
//					return nil
//				}
//
//				ar, err := getArchive()
//				if err != nil {
//					return err
//				}
//
//				written[objRefString] = true
//
//				return ar.putRaw(ctx, hdr, bytes)
//			})
//
//			if err != nil {
//				return err
//			}
//			obsolete = append(obsolete, archiveWriter)
//		}
//
//		log.Printf("Dropped %d objecst during compaction, saved %s", droppedObjects, humanize.Bytes(droppedSize))
//	}
//
//	if readArchive != nil {
//		err := closeArchive(readArchive)
//		if err != nil {
//			return err
//		}
//	}
//
//	// after having safely closed the active file(s) we can delete the left-overs
//	for _, archiveWriter := range obsolete {
//		ps.unloadArchive(archiveWriter)
//
//		if e := archiveWriter.Close(); e != nil {
//			log.Printf("Failed closing obsolete archiveWriter after compaction: %v", e)
//		}
//
//		idx, err := archiveWriter.loadIndex()
//		if err != nil {
//			log.Printf("Failed getting archiveWriter index for deletion: %s", err)
//			continue
//		}
//
//		err = ps.index.DeleteArchive(archiveWriter.name, idx)
//		if err != nil {
//			log.Printf("Failed removing local index %s after compaction: %s", archiveWriter.name, err)
//		}
//
//		err = ps.storage.Delete(archiveWriter.indexName())
//		if err != nil {
//			log.Printf("Failed deleting index %s after compaction: %v", archiveWriter.name, err)
//		}
//
//		err = ps.storage.Delete(archiveWriter.archiveName())
//		if err != nil {
//			log.Printf("Failed deleting archiveWriter %s after compaction: %v", archiveWriter.name, err)
//		}
//	}
//
//	return nil
//}

//func (ps *Store) backgroundCompaction() {
//	log.Println("Running compaction")
//	err := ps.doCompaction()
//
//	if err != nil {
//		log.Println("Failed running compaction", err)
//	} else {
//		log.Print("Compaction successful")
//	}
//}
//
//func (ps *Store) periodicCompaction() {
//	for {
//		select {
//		case <-ps.compactorTicker.C:
//			ps.backgroundCompaction()
//		case <-ps.compactorClose:
//			break
//		}
//	}
//}

//go:generate go run github.com/vektra/mockery/v2 --name File --inpackage --testonly --outpkg pack
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer

	Stat() (fs.FileInfo, error)
}

//go:generate go run github.com/vektra/mockery/v2 --name ArchiveStorage --inpackage --testonly --outpkg pack
type ArchiveStorage interface {
	Create(name string) (File, error)
	Open(name string) (File, error)
	List(extension string) ([]string, error)
	Delete(name string) error
	DeleteAll() error
}

type Parent interface {
	Child(name string) (ArchiveStorage, error)
	Children() ([]string, error)
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

func archiveFileName(name string) string {
	return name + ArchiveSuffix
}
