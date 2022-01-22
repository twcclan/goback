package pack

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/twcclan/goback/proto"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

type ArchiveIndexer interface {
	IndexArchive(name string, index IndexFile) error
}

func NewWriter(storage ArchiveStorage, index ArchiveIndexer, maxParallel uint, maxSize uint64) (*Writer, error) {
	grp, ctx := errgroup.WithContext(context.Background())

	writer := &Writer{
		index:     index,
		storage:   storage,
		maxSize:   maxSize,
		objects:   make(chan *archiveWrite),
		ctx:       ctx,
		grp:       grp,
		timeout:   5 * time.Second,
		semaphore: semaphore.NewWeighted(int64(maxParallel)),
	}

	err := writer.newArchiver()
	if err != nil {
		return nil, err
	}

	return writer, nil
}

var (
	ErrWriterClosed = errors.New("the writer is already closed")
)

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

	// grp is an errgroup to keep track of the archive writers
	grp *errgroup.Group

	// ctx will be cancelled as soon as the first archive writer errors
	ctx context.Context

	// timeout is the duration that a writer will wait for a write before finalizing an archive
	timeout time.Duration

	// semaphore is used to limit the number of archive writers
	semaphore *semaphore.Weighted

	// err stores the first error returned by any of the writers
	err atomic.Value

	// maxSize is the maximum size in bytes that we want archives to have
	maxSize uint64
}

func (w *Writer) newArchiver() error {
	if err := w.err.Load(); err != nil {
		return err.(error)
	}

	if w.semaphore.TryAcquire(1) {
		w.grp.Go(func() error {
			archive, err := newArchive(w.storage)
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

func (w *Writer) archiver(ctx context.Context, a *archive) error {
	defer w.semaphore.Release(1)

	exit := func() error {
		idx, err := a.CloseWriter()
		if err != nil {
			return err
		}

		return w.index.IndexArchive(a.name, idx)
	}

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

		case <-time.After(w.timeout):
			// if we haven't received a write for some time => exit
			return exit()
		}
	}
}

func (w *Writer) write(write *archiveWrite) error {
	// if we cannot send the write to an idle archive in 10ms, try to create a new archiver
	for {
		select {
		case w.objects <- write:
			return <-write.err
		case <-time.After(10 * time.Millisecond):
			// if there was an error in one of the archives we stop here
			if err := w.err.Load(); err != nil {
				return err.(error)
			}

			// otherwise, attempt to create a new archive
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
	// close the objects channel to make all the archive writer exit
	close(w.objects)

	return w.grp.Wait()
}
