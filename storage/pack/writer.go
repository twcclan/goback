package pack

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

type ArchiveIndexer interface {
	IndexArchive(name string, index IndexFile) error
}

func NewWriter(storage ArchiveStorage, index ArchiveIndexer, maxParallel uint, maxSize uint64) *Writer {
	grp, ctx := errgroup.WithContext(context.Background())

	writer := &Writer{
		index:        index,
		storage:      storage,
		maxSize:      maxSize,
		objects:      make(chan *archiveWrite),
		ctx:          ctx,
		grp:          grp,
		idleTimeout:  5 * time.Second,
		totalTimeout: 5 * time.Minute,
		maxParallel:  maxParallel,
		semaphore:    semaphore.NewWeighted(int64(maxParallel)),
	}

	return writer
}

type archiveWrite struct {
	obj *proto.Object
	err chan error
	ctx context.Context

	raw   bool
	hdr   *proto.ObjectHeader
	bytes []byte
}

type Writer struct {
	storage ArchiveStorage
	index   ArchiveIndexer

	// objects is the channel that writers read from to write data
	objects chan *archiveWrite

	// grp is an errgroup to keep track of the archiveWriter writers
	grp *errgroup.Group

	// ctx will be cancelled as soon as the first archiveWriter writer errors
	ctx context.Context

	// idleTimeout is the duration that a writer will wait for a write before finalizing an archiveWriter
	idleTimeout time.Duration

	// the archives will be closed, if they are still active after totalTimeout
	totalTimeout time.Duration

	// semaphore is used to limit the number of archiveWriter writers
	semaphore *semaphore.Weighted

	// err stores the first error returned by any of the writers
	err atomic.Value

	// maxSize is the maximum size in bytes that we want archives to have
	maxSize uint64

	// maxParallel is the maximum number of archives that are opened in parallel
	maxParallel uint
}

func (w *Writer) newArchiver() error {
	if err := w.err.Load(); err != nil {
		return err.(error)
	}

	if w.semaphore.TryAcquire(1) {
		w.grp.Go(func() error {
			id, err := uuid.NewRandom()
			if err != nil {
				return err
			}

			name := id.String()

			file, err := w.storage.Create(archiveFileName(name))
			if err != nil {
				return err
			}

			archive, err := writeArchive(file, name)
			if err != nil {
				w.err.Store(err)
				return err
			}

			err = w.archiver(w.ctx, archive)
			if err != nil {
				w.err.Store(err)
				return err
			}

			return err
		})
	}

	return nil
}

func (w *Writer) archiver(ctx context.Context, a *archiveWriter) error {
	defer w.semaphore.Release(1)

	exit := func() error {
		err := a.Close()
		if err != nil {
			return err
		}

		return w.index.IndexArchive(a.name, a.getIndex())
	}

	timeout := time.After(w.totalTimeout)

	for {
		select {
		case <-ctx.Done():
			return exit()

		case write := <-w.objects:
			// if the channel was closed, we just exit here
			if write == nil {
				return exit()
			}

			if write.raw {
				write.err <- a.putRaw(write.ctx, write.hdr, write.bytes)
			} else {
				// write the object and notify the caller
				write.err <- a.Put(write.ctx, write.obj)
			}

			// do some house-keeping before we accept the next write
			if a.size >= w.maxSize {
				return exit()
			}

		case <-timeout:
			// if we reached the total timeout => exit
			return exit()
		case <-time.After(w.idleTimeout):
			// if we haven't received a write for some time => exit
			return exit()
		}
	}
}

func (w *Writer) write(write *archiveWrite) error {
	// if we cannot send the write to an idle archiveWriter in 10ms, try to create a new archiver
	for {
		select {
		case w.objects <- write:
			return <-write.err
		case <-time.After(10 * time.Millisecond):
			// if there was an error in one of the archives we stop here
			if err := w.err.Load(); err != nil {
				return err.(error)
			}

			// otherwise, attempt to create a new archiveWriter
			err := w.newArchiver()
			if err != nil {
				return err
			}
		}
	}
}

func (w *Writer) putRaw(ctx context.Context, hdr *proto.ObjectHeader, bytes []byte) error {
	return w.write(&archiveWrite{
		err:   make(chan error, 1),
		ctx:   ctx,
		raw:   true,
		hdr:   hdr,
		bytes: bytes,
	})
}

func (w *Writer) Put(ctx context.Context, object *proto.Object) error {
	return w.write(&archiveWrite{
		obj: object,
		err: make(chan error, 1),
		ctx: ctx,
	})
}

func (w *Writer) Close() error {
	if w.objects == nil {
		return nil
	}

	// close the objects channel to make all the archiveWriter writer exit
	close(w.objects)
	w.objects = nil

	return w.grp.Wait()
}
