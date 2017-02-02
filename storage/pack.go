package storage

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/satori/go.uuid"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

const MaxSize = 1024 * 1024 * 1024 // 1 GB TODO: make this configurable for testing
const ArchiveSuffix = ".goback"
const ArchivePattern = "*" + ArchiveSuffix
const IndexExt = ".idx"
const varIntMaxSize = 10

func NewPackStorage(storage ArchiveStorage) *PackStorage {
	return &PackStorage{
		archives: make(map[string]*archive),
		active:   nil,
		storage:  storage,
	}
}

type PackStorage struct {
	archives map[string]*archive
	active   *archive
	base     string
	mtx      sync.RWMutex
	storage  ArchiveStorage
}

var _ backup.ObjectStore = (*PackStorage)(nil)

func (ps *PackStorage) Put(object *proto.Object) error {
	if err := ps.prepareArchive(); err != nil {
		return errors.Wrap(err, "Couldn't get archive for writing")
	}

	// don't store objects we already know about

	ps.mtx.Lock()
	defer ps.mtx.Unlock()

	if ps.Has(object.Ref()) {
		return nil
	}

	return ps.active.Put(object)
}

func (ps *PackStorage) Has(ref *proto.Ref) (has bool) {
	if ps.active != nil && ps.active.Has(ref) {
		return true
	}

	for _, archive := range ps.archives {
		if archive.Has(ref) {
			return true
		}
	}

	return false
}

func (ps *PackStorage) Get(ref *proto.Ref) (*proto.Object, error) {
	ps.mtx.RLock()
	defer ps.mtx.RUnlock()

	// there is a good chance we are looking for data in the active archive
	if ps.active != nil {
		obj, err := ps.active.Get(ref)
		if err != nil {
			return nil, err
		}

		if obj != nil {
			return obj, nil
		}
	}

	for _, archive := range ps.archives {
		obj, err := archive.Get(ref)
		if err != nil {
			return nil, err
		}

		if obj == nil {
			continue
		}

		return obj, nil
	}

	return nil, nil
}

func (ps *PackStorage) Delete(ref *proto.Ref) error {
	panic("not implemented")
}

func (ps *PackStorage) Walk(t proto.ObjectType, fn backup.ObjectReceiver) error {
	panic("not implemented")
}

func (ps *PackStorage) Close() error {
	if ps.active != nil {
		return ps.active.Close()
	}

	return nil
}

func (ps *PackStorage) Open() error {
	matches, err := ps.storage.List()
	if err != nil {
		return errors.Wrap(err, "failed listing archive names")
	}

	for _, match := range matches {
		archive, err := openArchive(ps.storage, match)
		if err != nil {
			return err
		}

		ps.archives[match] = archive
	}

	return nil
}

func (ps *PackStorage) prepareArchive() (err error) {
	ps.mtx.Lock()
	defer ps.mtx.Unlock()

	// if there is no active archive or the currently active
	// archive is too large already, start a new one
	openNew := ps.active == nil || ps.active.size >= MaxSize

	// close the active archive first
	if openNew && ps.active != nil {
		err = ps.active.CloseWriter()
		if err != nil {
			return
		}

		ps.archives[ps.active.name] = ps.active
		ps.active = nil
	}

	// if there is not active archive
	// create a new one
	if openNew {
		ps.active, err = newArchive(ps.storage)
		if err != nil {
			return
		}
	}

	return
}

type archive struct {
	writeFile  File
	readFile   File
	writer     *bufio.Writer
	reader     *bufio.Reader
	readOnly   bool
	size       uint64
	writeIndex map[string]*indexRecord
	readIndex  index
	mtx        sync.RWMutex
	last       *proto.Ref
	storage    ArchiveStorage
	name       string
}

func newArchive(storage ArchiveStorage) (*archive, error) {
	a := &archive{
		storage:  storage,
		name:     uuid.NewV4().String(),
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

func (a *archive) recoverIndex(err error) error {
	log.Printf("Attempting index recovery. couldn't open index: %v", err)

	recoveredIndex := make(index, 0)

	err = a.foreach(func(o *proto.ObjectHeader, offset uint32) {
		record := indexRecord{
			Offset: offset,
		}
		copy(record.Sum[:], o.Ref.Sha1)

		recoveredIndex = append(recoveredIndex, record)
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

		a.writer = bufio.NewWriter(a.writeFile)
		a.writeIndex = make(map[string]*indexRecord)
	}

	a.readFile, err = a.storage.Open(a.archiveName())
	if err != nil {
		return errors.Wrap(err, "Failed opening archive for reading")
	}
	a.reader = bufio.NewReader(a.readFile)

	if a.readOnly {
		idxFile, err := a.storage.Open(a.indexName())
		if err != nil {
			defer idxFile.Close()
			// attempt to recover index
			// TODO: make this configurable since it may potentially take very long

			return a.recoverIndex(err)
		}
		defer idxFile.Close()

		_, err = (&a.readIndex).ReadFrom(idxFile)
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

// make sure that you are holding a lock when calling this
func (a *archive) indexLocation(ref *proto.Ref) *indexRecord {
	if a.readOnly {
		return a.readIndex.lookup(ref)
	}

	return a.writeIndex[string(ref.Sha1)]
}

func (a *archive) Has(ref *proto.Ref) bool {
	a.mtx.RLock()
	defer a.mtx.RUnlock()

	return a.indexLocation(ref) != nil
}

func (a *archive) Get(ref *proto.Ref) (*proto.Object, error) {
	a.mtx.RLock()
	defer a.mtx.RUnlock()

	if loc := a.indexLocation(ref); loc != nil {

		_, err := a.readFile.Seek(int64(loc.Offset), os.SEEK_SET)
		if err != nil {
			return nil, errors.Wrap(err, "Failed seeking in file")
		}

		a.reader.Reset(a.readFile)

		// read size of object header
		hdrSizeBytes, err := a.reader.Peek(varIntMaxSize)
		if err != nil {
			return nil, err
		}

		hdrSize, consumed := proto.DecodeVarint(hdrSizeBytes)
		_, err = a.reader.Discard(consumed)
		if err != nil {
			return nil, err
		}

		// read object header
		hdrBytes := make([]byte, hdrSize)
		_, err = io.ReadFull(a.reader, hdrBytes)
		if err != nil {
			return nil, errors.Wrap(err, "Failed reading object header")
		}

		hdr, err := proto.NewObjectHeaderFromBytes(hdrBytes)
		if err != nil {
			return nil, errors.Wrap(err, "Failed parsing object header")
		}

		if !bytes.Equal(hdr.Ref.Sha1, ref.Sha1) {
			return nil, errors.New("Object doesn't match Ref, index probably corrupted")
		}

		buf := make([]byte, hdr.Size)
		_, err = io.ReadFull(a.reader, buf)
		if err != nil {
			return nil, errors.Wrap(err, "Failed reading data from archive")
		}

		return proto.NewObjectFromCompressedBytes(buf)
	}

	// it probably shouldn't be an error when we don't have an object
	return nil, nil
}

func (a *archive) Put(object *proto.Object) error {
	if a.readOnly {
		panic("Cannot write to readonly archive")
	}

	a.mtx.Lock()
	defer a.mtx.Unlock()

	ref := object.Ref()

	if loc := a.indexLocation(ref); loc != nil {
		return nil
	}

	bytes := object.CompressedBytes()

	hdr := &proto.ObjectHeader{
		Size:        uint64(len(bytes)),
		Compression: proto.Compression_GZIP,
		Ref:         ref,
		Predecessor: a.last,
	}

	hdrBytesSize := uint64(proto.Size(hdr))
	hdrBytes := proto.Bytes(hdr)

	// construct a buffer with our header preceded by a varint describing its size
	hdrBytes = append(proto.EncodeVarint(hdrBytesSize), hdrBytes...)

	_, err := a.writer.Write(hdrBytes)
	if err != nil {
		return errors.Wrap(err, "Failed writing header")
	}

	_, err = a.writer.Write(bytes)
	if err != nil {
		return errors.Wrap(err, "Failed writing data")
	}

	record := &indexRecord{
		Offset: uint32(a.size),
	}

	copy(record.Sum[:], ref.Sha1)

	a.writeIndex[string(ref.Sha1)] = record

	a.size += uint64(len(hdrBytes)) + hdr.Size
	a.last = ref

	return errors.Wrap(a.writer.Flush(), "Failed flushing object data")
}

func (a *archive) foreach(callback func(o *proto.ObjectHeader, offset uint32)) error {
	a.mtx.RLock()
	defer a.mtx.RUnlock()

	_, err := a.readFile.Seek(0, io.SeekStart)
	if err != nil {
		return errors.Wrap(err, "Couldn't seek to start of archive")
	}

	a.reader.Reset(a.readFile)
	offset := uint32(0)

	for {
		// read size of object header
		hdrSizeBytes, err := a.reader.Peek(varIntMaxSize)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		hdrSize, consumed := proto.DecodeVarint(hdrSizeBytes)
		_, err = a.reader.Discard(consumed)
		if err != nil {
			return err
		}

		// read object header
		hdrBytes := make([]byte, hdrSize)
		_, err = io.ReadFull(a.reader, hdrBytes)
		if err != nil {
			return errors.Wrap(err, "Failed reading object header")
		}

		hdr, err := proto.NewObjectHeaderFromBytes(hdrBytes)
		if err != nil {
			return errors.Wrap(err, "Failed parsing object header")
		}

		callback(hdr, offset)

		offset += uint32(consumed) + uint32(hdr.Size) + uint32(hdrSize)

		// skip the object data
		a.reader.Discard(int(hdr.Size))
	}

	return nil
}

func (a *archive) storeReadIndex(idx index) error {
	sort.Sort(idx)

	a.readIndex = idx

	//log.Printf("Writing index to %s", idxPath)
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

func NewLocalArchiveStorage(base string) *LocalArchiveStorage {
	return &LocalArchiveStorage{base}
}

var _ ArchiveStorage = (*LocalArchiveStorage)(nil)

type LocalArchiveStorage struct {
	base string
}

func (las *LocalArchiveStorage) Open(name string) (File, error) {
	return os.OpenFile(filepath.Join(las.base, name), os.O_RDONLY, 644)
}

func (las *LocalArchiveStorage) Create(name string) (File, error) {
	return os.Create(filepath.Join(las.base, name))
}

func (las *LocalArchiveStorage) Delete(name string) error {
	return os.Remove(filepath.Join(las.base, name))
}

func (las *LocalArchiveStorage) List() ([]string, error) {
	archives, err := filepath.Glob(filepath.Join(las.base, ArchivePattern))
	if err != nil {
		return nil, err
	}

	for i, archive := range archives {
		archives[i] = strings.TrimSuffix(filepath.Base(archive), ArchiveSuffix)
	}

	return archives, nil
}

type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer

	Stat() (os.FileInfo, error)
}

type ArchiveStorage interface {
	Create(name string) (File, error)
	Open(name string) (File, error)
	List() ([]string, error)
	Delete(name string) error
}

type countingWriter struct {
	count int64
}

func (c *countingWriter) Write(data []byte) (int, error) {
	c.count += int64(len(data))
	return len(data), nil
}

var _ io.WriterTo = (index)(nil)
var _ io.ReaderFrom = (*index)(nil)

type index []indexRecord

var indexEndianness = binary.BigEndian

func (b index) Len() int           { return len(b) }
func (b index) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b index) Less(i, j int) bool { return bytes.Compare(b[i].Sum[:], b[j].Sum[:]) < 0 }

func (idx *index) ReadFrom(reader io.Reader) (int64, error) {
	buf := bufio.NewReader(reader)
	var count uint32
	byteCounter := &countingWriter{}

	source := io.TeeReader(buf, byteCounter)

	err := binary.Read(source, indexEndianness, &count)
	if err != nil {
		return 0, err
	}

	*idx = make([]indexRecord, count)

	for i := 0; i < int(count); i++ {

		idxSlice := *idx
		err = binary.Read(source, indexEndianness, &idxSlice[i])
		if err != nil {
			return 0, err
		}
	}

	return byteCounter.count, nil
}

func (idx index) lookup(ref *proto.Ref) *indexRecord {
	n := sort.Search(len(idx), func(i int) bool {
		return bytes.Compare(idx[i].Sum[:], ref.Sha1) >= 0
	})

	if n < len(idx) && bytes.Equal(idx[n].Sum[:], ref.Sha1) {
		return &idx[n]
	}

	return nil
}

func (idx index) WriteTo(writer io.Writer) (int64, error) {
	buf := bufio.NewWriter(writer)
	count := uint32(len(idx))
	byteCounter := &countingWriter{}

	target := io.MultiWriter(buf, byteCounter)

	err := binary.Write(target, indexEndianness, count)
	if err != nil {
		return 0, err
	}

	for _, record := range idx {
		err = binary.Write(target, indexEndianness, &record)
		if err != nil {
			return 0, err
		}
	}

	return byteCounter.count, buf.Flush()
}

type indexRecord struct {
	Sum    [20]byte
	Offset uint32
}
