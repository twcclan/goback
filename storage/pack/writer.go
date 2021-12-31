package pack

import (
	"context"
	"errors"
	"log"
	"sync/atomic"
	"time"

	"github.com/twcclan/goback/proto"

	"golang.org/x/sync/semaphore"
)

func NewWriter(storage ArchiveStorage, index ArchiveIndex, maxParallel uint, maxSize uint64) *Writer {
	return &Writer{
		index:            index,
		storage:          storage,
		maxSize:          maxSize,
		writable:         make(chan *archive, maxParallel),
		archiveSemaphore: semaphore.NewWeighted(int64(maxParallel)),
	}
}

var (
	ErrWriterClosed = errors.New("the writer is already closed")
)

type Writer struct {
	index   ArchiveIndex
	storage ArchiveStorage

	// writable is a buffered channel containing archives that are open for writing
	writable         chan *archive
	archiveSemaphore *semaphore.Weighted

	// maxSize is the maximum size in bytes that we allow archives to have
	maxSize uint64

	// archives counts the number of open archives
	archives int32

	// closing is 1 if we are in the process of closing the writer
	closing int32
}

func (w *Writer) Put(ctx context.Context, object *proto.Object) error {
	if atomic.LoadInt32(&w.closing) == 1 {
		return ErrWriterClosed
	}

	return w.withWritableArchive(func(a *archive) error {
		return a.Put(ctx, object)
	})
}

func (w *Writer) withWritableArchive(writer func(*archive) error) error {
	for {
		select {
		// try to grab an idle open archive
		case a := <-w.writable:
			// a would be nil if the writer was closed while trying to find a writable archive
			if a == nil {
				return ErrWriterClosed
			}

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
			if size >= w.maxSize {
				log.Printf("Closing archive because it's full: %s", a.name)
				err := w.finalizeArchive(a)
				if err != nil {
					return err
				}

				continue
			}

			defer w.putWritableArchive(a)

			return writer(a)
		case <-time.After(10 * time.Millisecond):
			if w.archiveSemaphore.TryAcquire(1) {
				err := w.newArchive()
				if err != nil {
					// release if archive could not be created
					w.archiveSemaphore.Release(1)
					return err
				}
			}
		}
	}
}

func (w *Writer) putWritableArchive(ar *archive) {
	w.writable <- ar
}

func (w *Writer) finalizeArchive(a *archive) error {
	err := a.CloseReader()
	if err != nil {
		return err
	}

	index, err := a.CloseWriter()
	if err == errAlreadyClosed {
		return nil
	}

	err = w.index.IndexArchive(a.name, index)
	if err != nil {
		return err
	}

	atomic.AddInt32(&w.archives, -1)
	w.archiveSemaphore.Release(1)
	return err
}

func (w *Writer) newArchive() error {
	if atomic.LoadInt32(&w.closing) == 1 {
		return ErrWriterClosed
	}

	a, err := newArchive(w.storage)
	if err != nil {
		return err
	}

	atomic.AddInt32(&w.archives, 1)
	w.putWritableArchive(a)

	return nil
}

func (w *Writer) Close() error {
	atomic.StoreInt32(&w.closing, 1)

	for atomic.LoadInt32(&w.archives) > 0 {
		archive := <-w.writable

		err := w.finalizeArchive(archive)
		if err != nil {
			return err
		}
	}

	close(w.writable)

	return nil
}
