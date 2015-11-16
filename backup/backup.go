package backup

import (
	"crypto/sha1"
	"errors"
	"io"
	"log"
	"os"
	"sort"
	"sync/atomic"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/golang/groupcache/singleflight"

	"camlistore.org/pkg/rollsum"
	"camlistore.org/pkg/syncutil"
)

var (
	ErrEmptyBuffer   = errors.New("no buffer space provided")
	ErrIllegalOffset = errors.New("illegal offset")
)

const (
	// maxBlobSize is the largest blob we ever make when cutting up
	// a file.
	maxBlobSize = 1 << 20

	// bufioReaderSize is an explicit size for our bufio.Reader,
	// so we don't rely on NewReader's implicit size.
	// We care about the buffer size because it affects how far
	// in advance we can detect EOF from an io.Reader that doesn't
	// know its size.  Detecting an EOF bufioReaderSize bytes early
	// means we can plan for the final chunk.
	bufioReaderSize = 32 << 10

	// tooSmallThreshold is the threshold at which rolling checksum
	// boundaries are ignored if the current chunk being built is
	// smaller than this.
	tooSmallThreshold = 64 << 10

	inFlightChunks = 128
)

type backupFileWriter struct {
	store            ChunkStore
	index            Index
	backup           *BackupWriter
	buf              [maxBlobSize]byte
	blobSize         int64
	offset           int64
	rs               *rollsum.RollSum
	parts            []*proto.FilePart
	info             os.FileInfo
	name             string
	storageErr       *atomic.Value
	storageGroup     syncutil.Group
	storageSemaphore *syncutil.Gate

	pending int32
}

func (bfw *backupFileWriter) split() {
	chunkBytes := make([]byte, bfw.blobSize)
	copy(chunkBytes, bfw.buf[:bfw.blobSize])
	sum := sha1.Sum(chunkBytes)

	chunk := &proto.Chunk{
		Ref: &proto.ChunkRef{
			Sum: sum[:],
		},
		Data: chunkBytes,
	}
	bfw.blobSize = 0

	length := int64(len(chunk.Data))

	bfw.parts = append(bfw.parts, &proto.FilePart{
		Chunk:  &proto.ChunkRef{Sum: chunk.Ref.Sum},
		Start:  bfw.offset,
		Length: length,
	})

	bfw.offset += length

	bfw.storageSemaphore.Start()
	atomic.AddInt32(&bfw.pending, 1)

	bfw.storageGroup.Go(func() error {
		err := bfw.backup.storeChunk(chunk)
		bfw.storageSemaphore.Done()
		defer atomic.AddInt32(&bfw.pending, -1)
		if err != nil {
			// store the error here so future calls to write can exit early
			bfw.storageErr.Store(err)
		}
		return err
	})
}

func (bfw *backupFileWriter) Write(bytes []byte) (int, error) {
	deferred := bfw.storageErr.Load()

	if deferred != nil {
		return 0, deferred.(error)
	}

	for _, b := range bytes {
		bfw.rs.Roll(b)
		bfw.buf[bfw.blobSize] = b
		bfw.blobSize++

		onSplit := bfw.rs.OnSplit()

		//split if we found a border, or we reached maxBlobSize
		if (onSplit && bfw.blobSize > tooSmallThreshold) || bfw.blobSize == maxBlobSize {
			bfw.split()
		}
	}

	return len(bytes), nil
}

func (bfw *backupFileWriter) Close() (err error) {
	log.Printf("Closing file")

	if bfw.blobSize > 0 {
		bfw.split()
	}

	// store the file -> chunks mapping
	fileDataChunk := proto.GetMetaChunk(&proto.File{
		Size:  bfw.info.Size(),
		Parts: bfw.parts,
	}, proto.ChunkType_FILE)

	if err = bfw.backup.storeChunk(fileDataChunk); err != nil {
		return
	}

	// store the file info -> file mapping
	info := proto.GetFileInfo(bfw.info)
	info.Name = bfw.name
	info.Data = proto.GetUntypedRef(fileDataChunk.Ref)

	infoChunk := proto.GetMetaChunk(info, proto.ChunkType_FILE_INFO)
	if err = bfw.backup.storeChunk(infoChunk); err != nil {
		return
	}

	bfw.backup.add(infoChunk.Ref)

	// wait for all chunk uploads to finish

	log.Printf("Waiting for %d transfers to finish", atomic.LoadInt32(&bfw.pending))
	return bfw.storageGroup.Err()
}

var _ io.WriteCloser = new(backupFileWriter)

type BackupWriter struct {
	store ChunkStore
	index Index
	files []*proto.ChunkRef
	group *singleflight.Group
}

func (bw *BackupWriter) storeChunk(chunk *proto.Chunk) error {

	// make sure that only a single goroutine is ever trying
	// to store the same chunk
	_, err := bw.group.Do(string(chunk.Ref.Sum), func() (interface{}, error) {
		// don't store chunks that we know exist already
		found, err := bw.index.HasChunk(chunk.Ref)
		if err != nil || found {
			return nil, err
		}

		err = bw.store.Create(chunk)
		if err != nil {
			return nil, err
		}

		return nil, bw.index.Index(chunk)
	})

	return err
}

func (br *BackupWriter) add(file *proto.ChunkRef) {
	br.files = append(br.files, proto.GetUntypedRef(file))
}

func (br *BackupWriter) Create(name string, fInfo os.FileInfo) (io.WriteCloser, error) {
	// check if we have this file already
	info, err := br.index.FileInfo(name, fInfo.ModTime(), 1)
	if err != nil {
		return nil, err
	}

	if len(info) > 0 && info[0].Info.Timestamp <= fInfo.ModTime().UTC().Unix() {
		br.add(info[0].Ref)

		// signal that we have this file already
		return nil, os.ErrExist
	}

	writer := &backupFileWriter{
		store:            br.store,
		index:            br.index,
		rs:               rollsum.New(),
		parts:            make([]*proto.FilePart, 0),
		info:             fInfo,
		name:             name,
		backup:           br,
		storageErr:       new(atomic.Value),
		storageSemaphore: syncutil.NewGate(inFlightChunks),
	}

	return writer, nil
}

func (br *BackupWriter) Close() error {
	snapshot := proto.GetMetaChunk(&proto.Snapshot{
		Files: br.files,
	}, proto.ChunkType_SNAPSHOT)

	err := br.storeChunk(snapshot)

	if err != nil {
		return err
	}

	return br.storeChunk(proto.GetMetaChunk(&proto.SnapshotInfo{
		Data:      snapshot.Ref,
		Timestamp: time.Now().UTC().Unix(),
	}, proto.ChunkType_SNAPSHOT_INFO))
}

func NewBackupWriter(index Index, store ChunkStore) *BackupWriter {
	return &BackupWriter{
		store: store,
		index: index,
		files: make([]*proto.ChunkRef, 0),
		group: new(singleflight.Group),
	}
}

func NewBackupReader(index Index, store ChunkStore, optionalTime ...time.Time) *BackupReader {
	when := time.Unix(int64(^uint64(0)>>1), 0)
	if len(optionalTime) > 0 {
		when = optionalTime[0]
	}

	return &BackupReader{
		index: index,
		store: store,
		when:  when,
	}
}

/*
	backupFileInfo implements os.FileInfo for files from a backup
*/
type backupFileInfo struct {
	info *proto.FileInfo
}

func (bi *backupFileInfo) IsDir() bool {
	return false
}

func (bi *backupFileInfo) Name() string {
	return bi.info.Name
}

func (bi *backupFileInfo) ModTime() time.Time {
	return time.Unix(bi.info.Timestamp, 0)
}

func (bi *backupFileInfo) Mode() os.FileMode {
	return os.FileMode(bi.info.Mode)
}

func (bi *backupFileInfo) Size() int64 {
	return bi.info.Size
}

func (bi *backupFileInfo) Sys() interface{} {
	return nil
}

type backupFileReader struct {
	store     ChunkStore
	data      *proto.File
	chunk     *proto.Chunk
	partIndex int
	offset    int64
}

type searchCallback func(index int) bool

func (bfr *backupFileReader) search(index int) bool {
	part := bfr.data.Parts[index]

	return bfr.offset <= part.Start+part.Length-1

}

func (bfr *backupFileReader) Read(b []byte) (n int, err error) {
	// check if we reached EOF
	if bfr.offset >= bfr.data.Size {
		return 0, io.EOF
	}

	// the number of bytes requested by the caller
	// we return fewer bytes if we hit a chunk border
	n = len(b)

	// the chunk that represents the currently active part
	// of the file that is being read
	part := bfr.data.Parts[bfr.partIndex]

	// calculate the offset for the currently active chunk
	relativeOffset := bfr.offset - part.Start

	// calculate the bytes left for reading in this file part
	bytesRemaining := part.Length - relativeOffset

	// exit early if no buffer was provided
	if n == 0 {
		return 0, ErrEmptyBuffer
	}

	// limit the number of bytes read
	// so we don't cross chunk borders
	if n > int(bytesRemaining) {
		n = int(bytesRemaining)
	}

	// lazily load chunk from our chunk store
	if bfr.chunk == nil {
		bfr.chunk, err = bfr.store.Read(proto.GetTypedRef(part.Chunk, proto.ChunkType_DATA))
	}

	if err != nil {
		return
	}

	// read data from chunk
	copy(b, bfr.chunk.Data[relativeOffset:relativeOffset+int64(n)])

	// if we reached the end of the current chunk
	// we'll unload it and increase the part index
	if relativeOffset+int64(n) == part.Length {
		bfr.chunk = nil
		bfr.partIndex++
	}

	// increase offset
	bfr.offset += int64(n)

	return
}

const (
	seekSet = 0
	seekCur = 1
	seekEnd = 2
)

func (bfr *backupFileReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case seekSet:
		bfr.offset = offset
	case seekCur:
		bfr.offset += offset
	case seekEnd:
		bfr.offset = bfr.data.Size - 1 - offset
	}

	if bfr.offset < 0 || bfr.offset > bfr.data.Size {
		return bfr.offset, ErrIllegalOffset
	}

	// find the part that cointains the requested data
	if i := sort.Search(len(bfr.data.Parts), bfr.search); i != bfr.partIndex {
		// if it is not the currently active part
		// unload the currently loaded chunk
		bfr.partIndex = i
		bfr.chunk = nil
	}

	return bfr.offset, nil
}

type BackupReader struct {
	index Index
	store ChunkStore
	when  time.Time
}

func (b *BackupReader) Open(name string) (io.ReadSeeker, error) {
	var info *proto.FileInfo
	infoList, err := b.index.FileInfo(name, b.when, 1)
	if err != nil {
		return nil, err
	}

	if len(infoList) > 0 {
		info = infoList[0].Info
		log.Printf("%s (%d)", info.Name, info.Timestamp)
	} else {
		log.Fatalf("File %s not found", name)
		return nil, os.ErrNotExist
	}

	fileDataChunk, err := b.store.Read(proto.GetTypedRef(info.Data, proto.ChunkType_FILE))
	if err != nil {
		return nil, err
	}

	fileData := new(proto.File)
	proto.ReadMetaChunk(fileDataChunk, fileData)

	return &backupFileReader{
		store: b.store,
		data:  fileData,
	}, nil
}

func (b *BackupReader) Stat(name string) (os.FileInfo, error) {
	infoList, err := b.index.FileInfo(name, b.when, 1)
	if err != nil {
		return nil, err
	}

	if len(infoList) == 0 {
		return nil, os.ErrNotExist
	}

	return &backupFileInfo{infoList[0].Info}, nil
}
