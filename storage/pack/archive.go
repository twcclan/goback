package pack

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"io"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/willf/bitset"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var errAlreadyClosed = errors.New("Writer is already closed")

type readFile interface {
	io.ReadSeeker
	io.Closer
}

type writeFile interface {
	io.WriteCloser
}

//go:generate go run github.com/vektra/mockery/v2 --name fileInfo --inpackage --testonly --outpkg pack
type fileInfo interface {
	os.FileInfo
}

//go:generate go run github.com/vektra/mockery/v2 --name readerAt --inpackage --testonly --outpkg pack
type readerAt interface {
	readFile
	io.ReaderAt
}

//go:generate go run github.com/vektra/mockery/v2 --name writerTo --inpackage --testonly --outpkg pack
type writerTo interface {
	File
	io.WriterTo
}

type archive struct {
	writeFile  writeFile
	readFile   readFile
	readOnly   bool
	size       uint64
	writeIndex map[string]*IndexRecord
	gcBits     *bitset.BitSet
	mtx        sync.RWMutex
	last       *proto.Ref
	storage    ArchiveStorage
	name       string
}

func newArchive(storage ArchiveStorage) (*archive, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}

	a := &archive{
		storage:  storage,
		name:     id.String(),
		readOnly: false,
	}

	return a, a.open()
}

func openArchive(storage ArchiveStorage, name string) (*archive, error) {
	a := &archive{
		storage:  storage,
		name:     name,
		readOnly: true,
	}

	return a, a.open()
}

func (a *archive) recoverIndex(err error) (IndexFile, error) {
	log.Printf("Attempting index recovery. couldn't open index: %v", err)

	recoveredIndex := make(IndexFile, 0)

	err = a.foreach(loadNone, func(o *proto.ObjectHeader, _ []byte, offset, length uint32) error {
		record := IndexRecord{
			Offset: offset,
			Length: length,
			Type:   uint32(o.Type),
		}
		copy(record.Sum[:], o.Ref.Sha1)

		recoveredIndex = append(recoveredIndex, record)

		return nil
	})

	if err != nil {
		return nil, errors.Wrap(err, "Couldn't read archive to recover index")
	}

	log.Printf("Recovered %d index records", len(recoveredIndex))

	return recoveredIndex, a.storeReadIndex(recoveredIndex)
}

func (a *archive) getIndex() (IndexFile, error) {
	idxFile, err := a.storage.Open(a.indexName())
	if err != nil {
		// attempt to recover index
		// TODO: make this configurable since it may potentially take very long

		return a.recoverIndex(err)
	}
	defer idxFile.Close()

	idxBuf := bytes.NewBuffer(nil)
	_, err = io.Copy(idxBuf, idxFile)
	if err != nil {
		idx, err := a.recoverIndex(err)

		return idx, errors.Wrap(err, "Couldn't read index file")
	}

	var index IndexFile

	_, err = (&index).ReadFrom(idxBuf)
	if err != nil {
		index, err = a.recoverIndex(err)

		return index, errors.Wrap(err, "Couldn't read index file")
	}

	return index, nil
}

func (a *archive) open() (err error) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	if !a.readOnly {
		a.writeFile, err = a.storage.Create(a.archiveName())
		if err != nil {
			return errors.Wrap(err, "Failed creating archive file")
		}

		a.writeIndex = make(map[string]*IndexRecord)
	}

	readFile, err := a.storage.Open(a.archiveName())
	if err != nil {
		return errors.Wrap(err, "Failed opening archive for reading")
	}
	a.readFile = readFile

	if a.readOnly {
		info, err := readFile.Stat()
		if err != nil {
			return err
		}

		// store the size of the archive here for later
		a.size = uint64(info.Size())

	}

	return nil
}

func (a *archive) indexLocation(ref *proto.Ref) *IndexRecord {
	a.mtx.RLock()
	defer a.mtx.RUnlock()

	if a.readOnly {
		return nil
	}

	return a.writeIndex[string(ref.Sha1)]
}

func (a *archive) archiveName() string {
	return a.name + ArchiveSuffix
}

func (a *archive) indexName() string {
	return a.name + IndexExt
}

func (a *archive) getRaw(ctx context.Context, ref *proto.Ref, loc *IndexRecord) (*proto.Object, error) {
	ctx, span := trace.StartSpan(ctx, "archive.getRaw")
	defer span.End()

	buf := make([]byte, loc.Length)

	start := time.Now()
	if readerAt, ok := a.readFile.(io.ReaderAt); ok {
		span.AddAttributes(trace.BoolAttribute("lock-free", true))

		_, err := readerAt.ReadAt(buf, int64(loc.Offset))
		if err != nil {
			return nil, errors.Wrap(err, "Failed filling buffer")
		}
	} else {
		span.AddAttributes(trace.BoolAttribute("lock-free", false))

		// need to get an exclusive lock if we can't use ReadAt
		a.mtx.Lock()

		_, err := a.readFile.Seek(int64(loc.Offset), io.SeekStart)
		if err != nil {
			a.mtx.Unlock()
			return nil, errors.Wrap(err, "Failed seeking in file")
		}

		_, err = io.ReadFull(a.readFile, buf)
		if err != nil {
			a.mtx.Unlock()
			return nil, errors.Wrap(err, "Failed filling buffer")
		}

		a.mtx.Unlock()
	}

	readLatency := float64(time.Since(start)) / float64(time.Millisecond)

	// read size of object header
	hdrSize, consumed := proto.DecodeVarint(buf)

	// read object header
	hdr, err := proto.NewObjectHeaderFromBytes(buf[consumed : consumed+int(hdrSize)])
	if err != nil {
		return nil, errors.Wrap(err, "Failed parsing object header")
	}

	if !bytes.Equal(hdr.Ref.Sha1, ref.Sha1) {
		return nil, errors.New("Object doesn't match Ref, index probably corrupted")
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

func (a *archive) Put(ctx context.Context, object *proto.Object) error {
	ctx, span := trace.StartSpan(ctx, "archive.Put")
	defer span.End()

	bytes := object.CompressedBytes()
	ref := object.Ref()

	hdr := &proto.ObjectHeader{
		Compression: proto.Compression_GZIP,
		Ref:         ref,
		Type:        object.Type(),
	}

	return a.putRaw(ctx, hdr, bytes)
}

func (a *archive) putTombstone(ctx context.Context, ref *proto.Ref) error {
	tombstoneSha := sha1.Sum(ref.Sha1)
	tombstoneRef := &proto.Ref{Sha1: tombstoneSha[:]}

	hdr := &proto.ObjectHeader{
		Ref:          tombstoneRef,
		TombstoneFor: ref,
		Type:         proto.ObjectType_TOMBSTONE,
	}

	return a.putRaw(ctx, hdr, nil)
}

func (a *archive) putRaw(ctx context.Context, hdr *proto.ObjectHeader, bytes []byte) error {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	if a.readOnly {
		panic("Cannot write to readonly archive")
	}

	ref := hdr.Ref
	// make sure not to have duplicates within a single file
	if _, ok := a.writeIndex[string(ref.Sha1)]; ok {
		return nil
	}

	// add type info where it isn't already present
	if hdr.Type == proto.ObjectType_INVALID {
		obj, err := proto.NewObjectFromCompressedBytes(bytes)
		if err != nil {
			return err
		}

		hdr.Type = obj.Type()
	}

	hdr.Predecessor = a.last
	hdr.Size = uint64(len(bytes))

	// keep the timestamp if it's already present. this is important,
	// because we rely on the information of when a certain object
	// first entered our system
	if hdr.Timestamp == nil {
		hdr.Timestamp = timestamppb.Now()
	}

	hdrBytes := proto.Bytes(hdr)
	hdrBytesSize := uint64(len(hdrBytes))

	// construct a buffer with our header preceeded by a varint describing its size
	hdrBytes = append(proto.EncodeVarint(nil, hdrBytesSize), hdrBytes...)

	// add our payload
	data := append(hdrBytes, bytes...)

	start := time.Now()
	_, err := a.writeFile.Write(data)
	if err != nil {
		return errors.Wrap(err, "Failed writing header")
	}

	writeLatency := float64(time.Since(start)) / float64(time.Millisecond)

	record := &IndexRecord{
		Offset: uint32(a.size),
		Length: uint32(len(hdrBytes) + len(bytes)),
		Type:   uint32(hdr.Type),
	}

	copy(record.Sum[:], ref.Sha1)

	a.writeIndex[string(ref.Sha1)] = record

	a.size += uint64(record.Length)
	a.last = ref

	if ctx, err := tag.New(ctx,
		tag.Insert(KeyObjectType, hdr.Type.String()),
	); err == nil {
		stats.Record(ctx,
			PutObjectSize.M(int64(hdr.Size)),
			ArchiveWriteLatency.M(writeLatency),
			ArchiveWriteSize.M(int64(len(data))),
		)
	}

	return nil
}

//func (a *archive) markObject(ref *proto.Ref) {
//	a.mtx.Lock()
//	defer a.mtx.Unlock()
//
//	n, record := a.readIndex.lookup(ref)
//	if record != nil {
//		a.gcBits.Set(n)
//	}
//}

type loadPredicate func(*proto.ObjectHeader) bool

func loadAll(hdr *proto.ObjectHeader) bool  { return true }
func loadNone(hdr *proto.ObjectHeader) bool { return false }
func loadType(t proto.ObjectType) loadPredicate {
	return func(hdr *proto.ObjectHeader) bool {
		return hdr.Type == t
	}
}

func (a *archive) foreachReader(reader io.Reader, load loadPredicate, callback func(hdr *proto.ObjectHeader, bytes []byte, offset uint32, length uint32) error) error {
	bufReader := bufio.NewReaderSize(reader, 1024*16)
	offset := uint32(0)

	for {
		// read size of object header
		hdrSizeBytes, err := bufReader.Peek(varIntMaxSize)
		if len(hdrSizeBytes) != varIntMaxSize {
			if err == io.EOF {
				// we're done reading this archive
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
			return errors.Wrap(io.ErrUnexpectedEOF, "Failed reading object header")
		}

		if err != nil && err != io.EOF {
			return errors.Wrap(err, "Failed reading object header")
		}

		hdr, err := proto.NewObjectHeaderFromBytes(hdrBytes)
		if err != nil {
			return errors.Wrap(err, "Failed parsing object header")
		}

		var objOffset = offset
		var objectBytes []byte
		if load(hdr) {
			objectBytes = make([]byte, hdr.Size)
			n, err = io.ReadFull(bufReader, objectBytes)
			if n != int(hdr.Size) {
				return errors.Wrap(io.ErrUnexpectedEOF, "Failed reading object")
			}

			if err != nil && err != io.EOF {
				return errors.Wrap(err, "Failed reading object data")
			}
		} else {
			// skip the object data
			bufReader.Discard(int(hdr.Size))
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

func (a *archive) foreach(load loadPredicate, callback func(hdr *proto.ObjectHeader, bytes []byte, offset uint32, length uint32) error) error {
	file, err := a.storage.Open(a.archiveName())
	if err != nil {
		return errors.Wrap(err, "Couldn't open file for streaming")
	}

	defer file.Close()

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
			return a.foreachReader(pReader, load, callback)
		})

		wErr := grp.Wait()

		if wErr != nil {
			return errors.Wrap(wErr, "Couldn't stream file")
		}

		return nil
	}

	return a.foreachReader(file, load, callback)
}

func (a *archive) storeReadIndex(idx IndexFile) error {
	sort.Sort(idx)

	idxFile, err := a.storage.Create(a.indexName())
	if err != nil {
		return errors.Wrap(err, "Failed creating index file")
	}

	_, err = idx.WriteTo(idxFile)
	if err != nil {
		return errors.Wrap(err, "Couldn't encode index file")
	}

	return errors.Wrap(idxFile.Close(), "Failed closing index file")
}

func (a *archive) storeIndex() (IndexFile, error) {
	idx := make(IndexFile, 0, len(a.writeIndex))

	for _, loc := range a.writeIndex {
		idx = append(idx, *loc)
	}

	return idx, a.storeReadIndex(idx)
}

func (a *archive) Close() error {
	err := a.CloseReader()
	if err != nil {
		return err
	}

	_, err = a.CloseWriter()

	// this should be a nop here
	if err == errAlreadyClosed {
		err = nil
	}

	return err
}

func (a *archive) CloseReader() error {
	return a.readFile.Close()
}

func (a *archive) CloseWriter() (IndexFile, error) {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	if a.readOnly {
		return nil, errAlreadyClosed
	}

	err := a.writeFile.Close()
	if err != nil {
		return nil, errors.Wrap(err, "Failed closing file")
	}

	// switch to read-only mode
	a.readOnly = true

	idx, err := a.storeIndex()

	// release write index
	a.writeIndex = nil

	return idx, err
}
