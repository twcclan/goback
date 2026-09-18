package pack

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var errAlreadyClosed = errors.New("Writer is already closed")

const (
	archiveFormatVersion uint16 = 2
	archiveHashSHA256    uint16 = 1
	archiveHeaderSize           = 16
)

var archiveMagic = []byte("GOBACKPACK")

// archiveHeader is the fixed 16-byte prefix of every archive: magic, format
// version, hash algorithm, two reserved bytes.
func archiveHeader() []byte {
	hdr := make([]byte, archiveHeaderSize)
	copy(hdr, archiveMagic)
	binary.BigEndian.PutUint16(hdr[10:], archiveFormatVersion)
	binary.BigEndian.PutUint16(hdr[12:], archiveHashSHA256)

	return hdr
}

func checkArchiveHeader(hdr []byte) error {
	if len(hdr) < archiveHeaderSize || !bytes.Equal(hdr[:len(archiveMagic)], archiveMagic) {
		return errors.New("not a goback archive")
	}

	if v := binary.BigEndian.Uint16(hdr[10:]); v != archiveFormatVersion {
		return errors.Errorf("archive format version %d, want %d", v, archiveFormatVersion)
	}

	if h := binary.BigEndian.Uint16(hdr[12:]); h != archiveHashSHA256 {
		return errors.Errorf("archive hash algorithm %d, want %d", h, archiveHashSHA256)
	}

	return nil
}

type readFile interface {
	io.ReadSeeker
	io.Closer
}

type writeFile interface {
	io.WriteCloser
}

type archive struct {
	writeFile  writeFile
	readFile   readFile
	readOnly   bool
	size       uint64
	writeIndex map[string]*IndexRecord
	gc         *gcFile
	mtx        sync.RWMutex
	last       *proto.Ref
	storage    ArchiveStorage
	name       string
	atRest     *AtRestKeys
	logger     *slog.Logger

	// owner is the session writing this archive, nil once finalized or
	// when opened from storage
	owner   *writeSession
	session string
	state   ArchiveState
}

// newArchive opens a writable archive named by a fresh uuid under dir,
// sealing its payloads under atRest when that is set.
func newArchive(storage ArchiveStorage, dir string, atRest *AtRestKeys, logger *slog.Logger) (*archive, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}

	a := &archive{
		storage:  storage,
		name:     path.Join(dir, id.String()),
		readOnly: false,
		atRest:   atRest,
		logger:   logger,
	}

	return a, a.open()
}

func openArchive(storage ArchiveStorage, name string, atRest *AtRestKeys, logger *slog.Logger) (*archive, error) {
	a := &archive{
		storage:  storage,
		name:     name,
		readOnly: true,
		atRest:   atRest,
		logger:   logger,
	}

	return a, a.open()
}

func (a *archive) recoverIndex(err error) (IndexFile, error) {
	a.logger.Warn("recovering the index from the archive", "archive", a.name, "err", err)

	recoveredIndex := make(IndexFile, 0)

	err = a.foreach(loadNone, func(o *proto.ObjectHeader, _ []byte, offset, length uint32) error {
		record := IndexRecord{
			Offset: offset,
			Length: length,
			Type:   uint32(o.Type),
		}
		copy(record.Sum[:], o.Ref.Hash)

		recoveredIndex = append(recoveredIndex, record)

		return nil
	})

	if err != nil {
		return nil, errors.Wrap(err, "Couldn't read archive to recover index")
	}

	a.logger.Info("recovered index", "archive", a.name, "records", len(recoveredIndex))

	return recoveredIndex, a.storeReadIndex(recoveredIndex)
}

func (a *archive) getIndex() (IndexFile, error) {
	idxFile, err := a.storage.Open(a.indexName())
	if err != nil {
		// TODO: recovery may take very long; make it configurable
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

		_, err = a.writeFile.Write(archiveHeader())
		if err != nil {
			return errors.Wrap(err, "Failed writing archive header")
		}

		a.size = archiveHeaderSize
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

		a.size = uint64(info.Size())

		hdr := make([]byte, archiveHeaderSize)
		_, err = io.ReadFull(readFile, hdr)
		if err == nil {
			err = checkArchiveHeader(hdr)
		}

		if err != nil {
			readFile.Close()
			return errors.Wrapf(err, "archive %s", a.name)
		}
	}

	return nil
}

// indexLocation answers from the write index, which stays alive after the
// writer is closed until releaseWriteIndex is called.
func (a *archive) indexLocation(ref *proto.Ref) *IndexRecord {
	a.mtx.RLock()
	defer a.mtx.RUnlock()

	return a.writeIndex[string(ref.Hash)]
}

func (a *archive) gcResult() *gcFile {
	a.mtx.RLock()
	defer a.mtx.RUnlock()

	return a.gc
}

func (a *archive) setGCResult(g *gcFile) {
	a.mtx.Lock()
	a.gc = g
	a.mtx.Unlock()
}

// candidate reports whether the last completed generation found the object
// unreachable, so a presence check must not rely on this copy.
func (a *archive) candidate(hash []byte) bool {
	a.mtx.RLock()
	defer a.mtx.RUnlock()

	return a.gc != nil && a.gc.candidate(hash)
}

func (a *archive) releaseWriteIndex() {
	a.mtx.Lock()
	a.writeIndex = nil
	a.mtx.Unlock()
}

func (a *archive) archiveName() string {
	return a.name + ArchiveSuffix
}

func (a *archive) indexName() string {
	return a.name + IndexExt
}

// readRecord returns the raw bytes of one index record.
func (a *archive) readRecord(loc *IndexRecord) ([]byte, bool, error) {
	return a.readSpan(int64(loc.Offset), int64(loc.Length))
}

// scanIndex streams the archive's index. An index file that cannot be
// read is recovered into memory first, as any read of it would.
func (a *archive) scanIndex() (indexScanner, error) {
	file, err := a.storage.Open(a.indexName())
	if err == nil {
		scanner, err := newFileScanner(file)
		if err == nil {
			return scanner, nil
		}

		_ = file.Close()
	}

	idx, err := a.getIndex()
	if err != nil {
		return nil, err
	}

	return &sliceScanner{idx: idx}, nil
}

// readOnlyNow reports whether the archive is closed to writes, and so
// whether its bytes can be read without finalizing it first.
func (a *archive) readOnlyNow() bool {
	a.mtx.RLock()
	defer a.mtx.RUnlock()

	return a.readOnly
}

// readSpan reads length bytes at offset, which may cover several records.
// The bool reports whether the read was lock-free.
func (a *archive) readSpan(offset, length int64) ([]byte, bool, error) {
	buf := make([]byte, length)

	if readerAt, ok := a.readFile.(io.ReaderAt); ok {
		_, err := readerAt.ReadAt(buf, offset)
		if err != nil {
			return nil, true, errors.Wrap(err, "Failed filling buffer")
		}

		return buf, true, nil
	}

	// Seek and Read share the file position, so the fallback needs the
	// exclusive lock
	a.mtx.Lock()
	defer a.mtx.Unlock()

	_, err := a.readFile.Seek(offset, io.SeekStart)
	if err != nil {
		return nil, false, errors.Wrap(err, "Failed seeking in file")
	}

	_, err = io.ReadFull(a.readFile, buf)
	if err != nil {
		return nil, false, errors.Wrap(err, "Failed filling buffer")
	}

	return buf, false, nil
}

// readHeader decodes the object header of one index record.
func (a *archive) readHeader(loc *IndexRecord) (*proto.ObjectHeader, error) {
	buf, _, err := a.readRecord(loc)
	if err != nil {
		return nil, err
	}

	hdrSize, consumed := proto.DecodeVarint(buf)

	return proto.NewObjectHeaderFromBytes(buf[consumed : consumed+int(hdrSize)])
}

func (a *archive) getRaw(ctx context.Context, ref *proto.Ref, loc *IndexRecord) (*proto.Object, error) {
	ctx, span := tracer.Start(ctx, "archive.getRaw")
	defer span.End()

	start := time.Now()
	buf, lockFree, err := a.readRecord(loc)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.Bool("lock-free", lockFree))

	return a.objectFromRecord(ctx, ref, loc, buf, float64(time.Since(start))/float64(time.Millisecond))
}

// objectFromRecord decodes a record already read from the archive, which
// is how a caller that fetched several records at once opens each one.
func (a *archive) objectFromRecord(ctx context.Context, ref *proto.Ref, loc *IndexRecord, buf []byte, readLatency float64) (*proto.Object, error) {
	hdrSize, consumed := proto.DecodeVarint(buf)

	hdr, err := proto.NewObjectHeaderFromBytes(buf[consumed : consumed+int(hdrSize)])
	if err != nil {
		return nil, errors.Wrap(err, "Failed parsing object header")
	}

	if !bytes.Equal(hdr.Ref.Hash, ref.Hash) {
		return nil, errors.New("Object doesn't match Ref, index probably corrupted")
	}

	if hdr.Type == proto.ObjectType_TOMBSTONE {
		return nil, errors.Errorf("ref %x is a tombstone and has no object", ref.Hash)
	}

	attrs := metric.WithAttributes(keyObjectType.String(hdr.Type.String()))
	getObjectSize.Record(ctx, int64(hdr.Size), attrs)
	archiveReadLatency.Record(ctx, readLatency, attrs)
	archiveReadSize.Record(ctx, int64(loc.Length), attrs)

	stored, err := openAtRest(a.atRest, hdr, buf[consumed+int(hdrSize):])
	if err != nil {
		return nil, errors.Wrapf(err, "reading object %x from archive %s", ref.Hash, a.name)
	}

	obj, err := proto.ObjectFromStored(hdr, stored)
	if err != nil {
		return nil, errors.Wrapf(err, "reading object %x from archive %s", ref.Hash, a.name)
	}

	return obj, nil
}

func (a *archive) putTombstone(ctx context.Context, ref *proto.Ref, erase bool) error {
	hdr := &proto.ObjectHeader{
		Ref:          proto.TombstoneRef(ref),
		TombstoneFor: ref,
		Type:         proto.ObjectType_TOMBSTONE,
		Erase:        erase,
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

	if hdr.Type == proto.ObjectType_INVALID {
		return errors.New("object header has no type")
	}

	if !ref.Valid() {
		return errors.Errorf("object header ref has %d bytes, want %d", len(ref.GetHash()), proto.HashSize)
	}

	hdr.Predecessor = a.last
	hdr.AtRestKeyId = nil

	if a.atRest != nil && len(bytes) > 0 {
		sealed, err := a.atRest.seal(hdr, bytes)
		if err != nil {
			return err
		}

		bytes, hdr.AtRestKeyId = sealed, a.atRest.ID()
	}

	hdr.Size = uint64(len(bytes))

	// an existing timestamp says when the object first entered the store;
	// rewrites carry it over
	if hdr.Timestamp == nil {
		hdr.Timestamp = timestamppb.Now()
	}

	hdrBytes := proto.Bytes(hdr)
	hdrBytesSize := uint64(len(hdrBytes))

	// record layout: varint header size, header, payload
	hdrBytes = append(proto.EncodeVarint(nil, hdrBytesSize), hdrBytes...)
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

	copy(record.Sum[:], ref.Hash)

	a.writeIndex[string(ref.Hash)] = record

	a.size += uint64(record.Length)
	a.last = ref

	attrs := metric.WithAttributes(keyObjectType.String(hdr.Type.String()))
	putObjectSize.Record(ctx, int64(hdr.Size), attrs)
	archiveWriteLatency.Record(ctx, writeLatency, attrs)
	archiveWriteSize.Record(ctx, int64(len(data)), attrs)

	return nil
}

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

	fileHeader := make([]byte, archiveHeaderSize)
	_, err := io.ReadFull(bufReader, fileHeader)
	if err != nil {
		return errors.Wrap(err, "reading archive header")
	}

	err = checkArchiveHeader(fileHeader)
	if err != nil {
		return err
	}

	offset := uint32(archiveHeaderSize)

	for {
		hdrSizeBytes, err := bufReader.Peek(varIntMaxSize)
		if len(hdrSizeBytes) != varIntMaxSize {
			if err == io.EOF {
				break
			}
			return err
		}

		hdrSize, consumed := proto.DecodeVarint(hdrSizeBytes)
		_, err = bufReader.Discard(consumed)
		if err != nil {
			return err
		}

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

			objectBytes, err = openAtRest(a.atRest, hdr, objectBytes)
			if err != nil {
				return err
			}
		} else {
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

	if writerTo, ok := file.(io.WriterTo); ok {
		pReader, pWriter := io.Pipe()
		var grp errgroup.Group

		grp.Go(func() error {
			_, wErr := writerTo.WriteTo(pWriter)
			pWriter.CloseWithError(wErr)

			return wErr
		})

		grp.Go(func() error {
			// closing the read side unblocks the writer if the walk stops early
			rErr := a.foreachReader(pReader, load, callback)
			pReader.CloseWithError(rErr)

			return rErr
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

// Close finalizes a still-open writer, which reopens the reader, and then
// closes the reader.
func (a *archive) Close() error {
	_, err := a.CloseWriter()
	if err != nil && err != errAlreadyClosed {
		return err
	}

	return a.CloseReader()
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

	a.readOnly = true

	// the read handle may have been opened against the upload in flight;
	// reads must now come from what the storage holds
	_ = a.readFile.Close()
	a.readFile, err = a.storage.Open(a.archiveName())
	if err != nil {
		return nil, errors.Wrap(err, "Failed reopening archive for reading")
	}

	return a.storeIndex()
}
