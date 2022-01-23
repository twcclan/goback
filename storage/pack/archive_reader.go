package pack

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/twcclan/goback/proto"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"golang.org/x/sync/errgroup"
)

type readFile interface {
	io.ReadSeeker
	io.Closer
}

func readArchive(file File) (*archiveReader, error) {
	a := &archiveReader{
		readFile: file,
	}

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	// store the size of the archiveWriter here for later
	a.size = uint64(info.Size())

	return a, nil
}

func streamArchive(file File, load loadPredicate, callback func(hdr *proto.ObjectHeader, bytes []byte, offset uint32, length uint32) error) error {
	// use streaming if the underlying storage supports it
	if writerTo, ok := file.(io.WriterTo); ok {
		pReader, pWriter := io.Pipe()
		var grp errgroup.Group

		grp.Go(func() error {
			defer pWriter.Close()

			_, wErr := writerTo.WriteTo(pWriter)

			return wErr
		})

		grp.Go(func() error {
			return foreachReader(pReader, load, callback)
		})

		wErr := grp.Wait()

		if wErr != nil {
			return fmt.Errorf("couldn't stream file: %w", wErr)
		}

		return nil
	}

	return foreachReader(file, load, callback)
}

type archiveReader struct {
	readFile readFile
	size     uint64

	mtx sync.RWMutex
}

func (a *archiveReader) getRaw(ctx context.Context, ref *proto.Ref, loc *IndexRecord) (*proto.Object, error) {

	ctx, span := trace.StartSpan(ctx, "archiveWriter.getRaw")
	defer span.End()

	buf := make([]byte, loc.Length)

	start := time.Now()
	if readerAt, ok := a.readFile.(io.ReaderAt); ok {
		span.AddAttributes(trace.BoolAttribute("lock-free", true))

		_, err := readerAt.ReadAt(buf, int64(loc.Offset))
		if err != nil {
			return nil, fmt.Errorf("failed filling buffer: %w", err)
		}
	} else {
		span.AddAttributes(trace.BoolAttribute("lock-free", false))

		// need to get an exclusive lock if we can't use ReadAt
		a.mtx.Lock()

		_, err := a.readFile.Seek(int64(loc.Offset), io.SeekStart)
		if err != nil {
			a.mtx.Unlock()
			return nil, fmt.Errorf("failed seeking in file: %w", err)
		}

		_, err = io.ReadFull(a.readFile, buf)
		if err != nil {
			a.mtx.Unlock()
			return nil, fmt.Errorf("failed filling buffer: %w", err)
		}

		a.mtx.Unlock()
	}

	readLatency := float64(time.Since(start)) / float64(time.Millisecond)

	// read size of object header
	hdrSize, consumed := proto.DecodeVarint(buf)

	// read object header
	hdr, err := proto.NewObjectHeaderFromBytes(buf[consumed : consumed+int(hdrSize)])
	if err != nil {
		return nil, fmt.Errorf("failed parsing object header: %w", err)
	}

	if !bytes.Equal(hdr.Ref.Sha1, ref.Sha1) {
		return nil, errors.New("object doesn't match Ref, index probably corrupted")
	}

	if ctx, err := tag.New(ctx,
		tag.Insert(KeyObjectType, hdr.Type.String()),
	); err == nil {
		stats.Record(ctx,
			GetObjectSize.M(int64(hdr.Size)),
			ArchiveReadLatency.M(readLatency),
			ArchiveReadSize.M(int64(loc.Length)),
		)
	}

	return proto.NewObjectFromCompressedBytes(buf[consumed+int(hdrSize):])
}

func (a *archiveReader) Close() error {
	return a.readFile.Close()
}

type loadPredicate func(*proto.ObjectHeader) bool

func loadAll(hdr *proto.ObjectHeader) bool  { return true }
func loadNone(hdr *proto.ObjectHeader) bool { return false }
func loadType(t proto.ObjectType) loadPredicate {
	return func(hdr *proto.ObjectHeader) bool {
		return hdr.Type == t
	}
}

func foreachReader(reader io.Reader, load loadPredicate, callback func(hdr *proto.ObjectHeader, bytes []byte, offset uint32, length uint32) error) error {
	bufReader := bufio.NewReaderSize(reader, 1024*16)
	offset := uint32(0)

	for {
		// read size of object header
		hdrSizeBytes, err := bufReader.Peek(varIntMaxSize)
		if len(hdrSizeBytes) != varIntMaxSize {
			if err == io.EOF {
				// we're done reading this archiveWriter
				break
			}
			return err
		}

		hdrSize, consumed := proto.DecodeVarint(hdrSizeBytes)
		_, err = bufReader.Discard(consumed)
		if err != nil {
			return err
		}

		// read object header
		hdrBytes := make([]byte, hdrSize)
		n, err := io.ReadFull(bufReader, hdrBytes)
		if n != int(hdrSize) {
			return fmt.Errorf("failed reading object header: %w", io.ErrUnexpectedEOF)
		}

		if err != nil && err != io.EOF {
			return fmt.Errorf("failed reading object header: %w", err)
		}

		hdr, err := proto.NewObjectHeaderFromBytes(hdrBytes)
		if err != nil {
			return fmt.Errorf("failed parsing object header: %w", err)
		}

		var objOffset = offset
		var objectBytes []byte
		if load(hdr) {
			objectBytes = make([]byte, hdr.Size)
			n, err = io.ReadFull(bufReader, objectBytes)
			if n != int(hdr.Size) {
				return fmt.Errorf("failed reading object: %w", io.ErrUnexpectedEOF)
			}

			if err != nil && err != io.EOF {
				return fmt.Errorf("failed reading object data: %w", err)
			}
		} else {
			// skip the object data
			_, err = bufReader.Discard(int(hdr.Size))
			if err != nil {
				return fmt.Errorf("could not skip ahead: %w", err)
			}
		}

		offset += uint32(consumed) + uint32(hdr.Size) + uint32(hdrSize)
		length := uint32(consumed) + uint32(hdr.Size) + uint32(hdrSize)

		err = callback(hdr, objectBytes, objOffset, length)
		if err != nil {
			return err
		}
	}

	return nil
}
