package storage

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

const MaxSize = 1024 * 1024 * 1024 // 1 GB TODO: make this configurable for testing
const ArchiveName = "archive"
const ArchiveSuffix = ".goback"
const ArchivePattern = ArchiveName + "*" + ArchiveSuffix
const IndexExt = ".idx"
const varIntMaxSize = 10

func NewPackStorage(base string) *PackStorage {
	return &PackStorage{
		archives: make(map[string]*archive),
		active:   nil,
		base:     base,
	}
}

type PackStorage struct {
	archives map[string]*archive
	active   *archive
	base     string
	mtx      sync.RWMutex
}

var _ backup.ObjectStore = (*PackStorage)(nil)

func (ps *PackStorage) Put(object *proto.Object) error {
	if err := ps.prepareArchive(); err != nil {
		return err
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
	matches, err := filepath.Glob(filepath.Join(ps.base, ArchivePattern))
	if err != nil {
		panic(err)
	}

	for _, match := range matches {
		archive, err := openArchive(match)
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
		err = ps.active.Close()
		if err != nil {
			return
		}

		ps.archives[ps.active.readFile.Name()] = ps.active
		ps.active = nil
	}

	// if there is not active archive
	// create a new one
	if openNew {
		ps.active, err = newArchive(ps.base)
		if err != nil {
			return
		}
	}

	return
}

type archive struct {
	archive    *os.File
	readFile   *os.File
	writer     *bufio.Writer
	reader     *bufio.Reader
	base       string
	readOnly   bool
	size       uint64
	writeIndex map[string]*proto.Location
	readIndex  *proto.Index
	mtx        sync.RWMutex
	last       *proto.Ref
}

func newArchive(base string) (*archive, error) {
	writeFile, err := ioutil.TempFile(base, ArchiveName)
	if err != nil {
		return nil, errors.Wrap(err, "Failed creating temp file")
	}

	readFile, err := os.OpenFile(writeFile.Name(), os.O_RDONLY, 644)
	if err != nil {
		return nil, errors.Wrap(err, "Failed opening temp file for reading")
	}

	//log.Printf("Archive path: %s", file.Name())

	return &archive{
		archive:    writeFile,
		readFile:   readFile,
		reader:     bufio.NewReader(readFile),
		writer:     bufio.NewWriter(writeFile),
		base:       base,
		readOnly:   false,
		writeIndex: make(map[string]*proto.Location),
	}, nil
}

func openArchive(path string) (*archive, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	// TODO: extract this into a function
	idxPath := filepath.Join(filepath.Dir(path), strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+IndexExt)

	bytes, err := ioutil.ReadFile(idxPath)
	if err != nil {
		return nil, err
	}

	index, err := proto.NewIndexFromCompressedBytes(bytes)
	if err != nil {
		return nil, err
	}

	return &archive{
		readFile:  file,
		reader:    bufio.NewReader(file),
		readIndex: index,
		readOnly:  true,
	}, nil
}

// make sure that you are holding a lock when calling this
func (a *archive) indexLocation(ref *proto.Ref) *proto.Location {
	if a.readOnly {
		locs := a.readIndex.Locations

		i := sort.Search(len(locs), func(i int) bool {
			return bytes.Compare(locs[i].Ref.Sha1, ref.Sha1) >= 0
		})

		if i < len(locs) && bytes.Equal(locs[i].Ref.Sha1, ref.Sha1) {
			return locs[i]
		}

		return nil
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
		_, err = a.reader.Read(hdrBytes)
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

	// compressing the bytes can be done in parallel
	bytes := object.CompressedBytes()

	a.mtx.Lock()
	defer a.mtx.Unlock()

	if loc := a.indexLocation(object.Ref()); loc != nil {
		return nil
	}

	hdr := &proto.ObjectHeader{
		Size:        uint64(len(bytes)),
		Compression: proto.Compression_GZIP,
		Ref:         object.Ref(),
		Predecessor: a.last,
	}

	hdrBytesSize := uint64(proto.Size(hdr))
	hdrBytes := proto.Bytes(hdr)

	// construct a buffer with our header preceded by a varint describing its size
	hdrBytes = append(proto.EncodeVarint(hdrBytesSize), hdrBytes...)

	_, err := a.archive.Write(hdrBytes)
	if err != nil {
		return errors.Wrap(err, "Failed writing header")
	}

	_, err = a.archive.Write(bytes)
	if err != nil {
		return errors.Wrap(err, "Failed writing data")
	}

	a.writeIndex[string(object.Ref().Sha1)] = &proto.Location{
		Ref:    object.Ref(),
		Offset: a.size,
		Type:   object.Type(),
		Size:   uint64(len(hdrBytes)) + hdr.Size,
	}

	a.size += uint64(len(hdrBytes)) + hdr.Size
	a.last = object.Ref()

	return nil
	//return errors.Wrap(a.writer.Flush(), "Failed flushing tar file")
}

type byRef []*proto.Location

func (b byRef) Len() int           { return len(b) }
func (b byRef) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byRef) Less(i, j int) bool { return bytes.Compare(b[i].Ref.Sha1, b[j].Ref.Sha1) < 0 }

func (a *archive) storeIndex() error {
	idx := &proto.Index{
		Locations: make([]*proto.Location, 0, len(a.writeIndex)),
	}

	for _, loc := range a.writeIndex {
		idx.Locations = append(idx.Locations, loc)
	}

	sort.Sort(byRef(idx.Locations))

	idxPath := filepath.Join(a.base, filepath.Base(a.archive.Name())+IndexExt)

	a.readIndex = idx

	//log.Printf("Writing index to %s", idxPath)

	return ioutil.WriteFile(idxPath, idx.CompressedBytes(), 0644)
}

func (a *archive) CloseReader() error {
	return a.readFile.Close()
}

func (a *archive) Close() error {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	err := a.archive.Close()
	if err != nil {
		return errors.Wrap(err, "Failed closing file")
	}

	err = os.Rename(a.archive.Name(), a.archive.Name()+ArchiveSuffix)
	if err != nil {
		errors.Wrap(err, "Failed renaming archive")
	}

	a.readOnly = true

	return a.storeIndex()
}
