package pack

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"os"
	"sort"
	"sync"

	"github.com/twcclan/goback/proto"
	"golang.org/x/sync/errgroup"

	"github.com/golang/protobuf/ptypes"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type readFile interface {
	io.ReadSeeker
	io.Closer
}

type writeFile interface {
	io.WriteCloser
}

//go:generate mockery -name fileInfo -inpkg -testonly -outpkg pack
type fileInfo interface {
	os.FileInfo
}

//go:generate mockery -name readerAt -inpkg -testonly -outpkg pack
type readerAt interface {
	readFile
	io.ReaderAt
}

//go:generate mockery -name writerTo -inpkg -testonly -outpkg pack
type writerTo interface {
	File
	io.WriterTo
}

type archive struct {
	writeFile  writeFile
	readFile   readFile
	writer     *bufio.Writer
	reader     *bufio.Reader
	readOnly   bool
	size       uint64
	writeIndex map[string]*indexRecord
	readIndex  index
	mtx        *sync.RWMutex
	last       *proto.Ref
	storage    ArchiveStorage
	name       string
	number     uint16
}

func newArchive(storage ArchiveStorage) (archive, error) {
	id, err := uuid.NewV4()
	if err != nil {
		return archive{}, err
	}

	a := archive{
		storage:  storage,
		name:     id.String(),
		readOnly: false,
		mtx:      &sync.RWMutex{},
	}

	return a, a.open()
}

func openArchive(storage ArchiveStorage, name string) (archive, error) {
	a := archive{
		storage:  storage,
		name:     name,
		readOnly: true,
		mtx:      &sync.RWMutex{},
	}

	return a, a.open()
}

func (a *archive) recoverIndex(err error) error {
	log.Printf("Attempting index recovery. couldn't open index: %v", err)

	recoveredIndex := make(index, 0)

	err = a.foreach(loadNone, func(o *proto.ObjectHeader, _ []byte, offset, length uint32) error {
		record := indexRecord{
			Offset: offset,
			Length: length,
		}
		copy(record.Sum[:], o.Ref.Sha1)

		recoveredIndex = append(recoveredIndex, record)

		return nil
	})

	if err != nil {
		return errors.Wrap(err, "Couldn't read achive to recover index")
	}

	log.Printf("Recovered %d index records", len(recoveredIndex))

	recErr := a.storeReadIndex(recoveredIndex)
	if recErr != nil {
		// we tried hard, have to fail here
		return errors.Wrap(recErr, "failed index recovery")
	}

	return nil
}

func (a *archive) open() (err error) {
	if !a.readOnly {
		a.writeFile, err = a.storage.Create(a.archiveName())
		if err != nil {
			return errors.Wrap(err, "Failed creating archive file")
		}

		//a.writer = bufio.NewWriterSize(a.writeFile, 1024*1024)
		a.writeIndex = make(map[string]*indexRecord)
	}

	readFile, err := a.storage.Open(a.archiveName())
	if err != nil {
		return errors.Wrap(err, "Failed opening archive for reading")
	}
	a.readFile = readFile
	a.reader = bufio.NewReader(a.readFile)

	if a.readOnly {
		info, err := readFile.Stat()
		if err != nil {
			return err
		}

		// store the size of the archive here for later
		a.size = uint64(info.Size())

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
			return errors.Wrap(a.recoverIndex(err), "Couldn't read index file")
		}

		_, err = (&a.readIndex).ReadFrom(idxBuf)
		if err != nil {
			return errors.Wrap(a.recoverIndex(err), "Couldn't read index file")
		}
	}

	return nil
}

func (a *archive) archiveName() string {
	return a.name + ArchiveSuffix
}

func (a *archive) indexName() string {
	return a.name + IndexExt
}

func (a *archive) getRaw(ref *proto.Ref, loc *indexRecord) (*proto.Object, error) {
	buf := make([]byte, loc.Length)

	if readerAt, ok := a.readFile.(io.ReaderAt); ok {
		_, err := readerAt.ReadAt(buf, int64(loc.Offset))
		if err != nil {
			return nil, errors.Wrap(err, "Failed filling buffer")
		}
	} else {
		// need to get an exclusive lock if we can't use ReadAt
		a.mtx.Lock()

		_, err := a.readFile.Seek(int64(loc.Offset), os.SEEK_SET)
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

	return proto.NewObjectFromCompressedBytes(buf[consumed+int(hdrSize):])
}

func (a *archive) Put(object *proto.Object) error {
	bytes := object.CompressedBytes()
	ref := object.Ref()

	hdr := &proto.ObjectHeader{
		Size:        uint64(len(bytes)),
		Compression: proto.Compression_GZIP,
		Ref:         ref,
		Type:        object.Type(),
	}

	return a.putRaw(hdr, bytes)
}

func (a *archive) putRaw(hdr *proto.ObjectHeader, bytes []byte) error {
	if a.readOnly {
		panic("Cannot write to readonly archive")
	}

	a.mtx.Lock()
	defer a.mtx.Unlock()

	// add type info where it isn't already present
	if hdr.Type == proto.ObjectType_INVALID {
		obj, err := proto.NewObjectFromCompressedBytes(bytes)
		if err != nil {
			return err
		}

		hdr.Type = obj.Type()
	}

	ref := hdr.Ref
	hdr.Timestamp = ptypes.TimestampNow()
	hdr.Predecessor = a.last

	hdrBytesSize := uint64(proto.Size(hdr))
	hdrBytes := proto.Bytes(hdr)

	// construct a buffer with our header preceeded by a varint describing its size
	hdrBytes = append(proto.EncodeVarint(hdrBytesSize), hdrBytes...)

	_, err := a.writeFile.Write(hdrBytes)
	if err != nil {
		return errors.Wrap(err, "Failed writing header")
	}

	_, err = a.writeFile.Write(bytes)
	if err != nil {
		return errors.Wrap(err, "Failed writing data")
	}

	record := &indexRecord{
		Offset: uint32(a.size),
		Length: uint32(len(hdrBytes) + len(bytes)),
		Pack:   a.number,
	}

	copy(record.Sum[:], ref.Sha1)

	a.writeIndex[string(ref.Sha1)] = record

	a.size += uint64(record.Length)
	a.last = ref

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

func (a *archive) storeReadIndex(idx index) error {
	sort.Sort(idx)

	a.readIndex = idx

	idxFile, err := a.storage.Create(a.indexName())
	if err != nil {
		return errors.Wrap(err, "Failed creating index file")
	}

	_, err = a.readIndex.WriteTo(idxFile)
	if err != nil {
		return errors.Wrap(err, "Couldn't encode index file")
	}

	return errors.Wrap(idxFile.Close(), "Failed closing index file")
}

func (a *archive) storeIndex() error {
	idx := make(index, 0, len(a.writeIndex))

	for _, loc := range a.writeIndex {
		idx = append(idx, *loc)
	}

	return a.storeReadIndex(idx)
}

func (a *archive) Close() error {
	a.CloseReader()
	return a.CloseWriter()
}

func (a *archive) CloseReader() error {
	return a.readFile.Close()
}

func (a *archive) CloseWriter() error {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	if a.readOnly {
		return nil
	}

	err := a.writeFile.Close()
	if err != nil {
		return errors.Wrap(err, "Failed closing file")
	}

	// switch to read-only mode
	a.readOnly = true

	err = a.storeIndex()

	// release write index
	a.writeIndex = nil

	return err
}