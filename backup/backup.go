package backup

import (
	"errors"
	"io"
	"log"
	"os"
	"sort"
	"time"

	"github.com/twcclan/goback/proto"

	"crypto/sha1"

	"camlistore.org/pkg/rollsum"
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

	inFlightChunks = 1
)

type backupFileWriter struct {
	store    ChunkStore
	index    Index
	backup   *BackupWriter
	buf      [maxBlobSize]byte
	blobSize int64
	offset   int64
	rs       *rollsum.RollSum
	parts    []*proto.FilePart
	info     os.FileInfo
	name     string
}

func (bfw *backupFileWriter) split() error {
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

	return bfw.backup.storeChunk(chunk)
}

func (bfw *backupFileWriter) Write(bytes []byte) (int, error) {
	for _, b := range bytes {
		bfw.rs.Roll(b)
		bfw.buf[bfw.blobSize] = b
		bfw.blobSize++

		onSplit := bfw.rs.OnSplit()

		//split if we found a border, or we reached maxBlobSize
		if (onSplit && bfw.blobSize > tooSmallThreshold) || bfw.blobSize == maxBlobSize {
			err := bfw.split()

			if err != nil {
				return int(bfw.blobSize), err
			}
		}
	}

	return len(bytes), nil
}

func (bfw *backupFileWriter) Close() (err error) {
	if bfw.blobSize > 0 {
		err = bfw.split()
	}

	if err != nil {
		return
	}

	// store the file -> chunks mapping
	fileDataChunk := proto.GetMetaChunk(&proto.File{
		Size:  bfw.info.Size(),
		Parts: bfw.parts,
	}, proto.ChunkType_FILE_DATA)

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

	bfw.backup.Add(infoChunk.Ref)

	return
}

var _ io.WriteCloser = new(backupFileWriter)

type BackupWriter struct {
	store ChunkStore
	index Index
	files []*proto.ChunkRef
}

func (bw *BackupWriter) storeChunk(chunk *proto.Chunk) error {
	// don't store chunks that we know exist already
	found, err := bw.index.HasChunk(chunk.Ref)
	if err != nil || found {
		return err
	}

	err = bw.store.Create(chunk)
	if err != nil {
		return err
	}

	return bw.index.Index(chunk)
}

func (br *BackupWriter) Add(file *proto.ChunkRef) {
	br.files = append(br.files, proto.GetUntypedRef(file))
}

func (br *BackupWriter) Create(name string, info os.FileInfo) (io.WriteCloser, error) {
	writer := &backupFileWriter{
		store:  br.store,
		index:  br.index,
		rs:     rollsum.New(),
		parts:  make([]*proto.FilePart, 0),
		info:   info,
		name:   name,
		backup: br,
	}

	return writer, nil
}

func (br *BackupWriter) Close() error {
	return br.storeChunk(proto.GetMetaChunk(&proto.Snapshot{
		Files:     br.files,
		Timestamp: time.Now().UTC().Unix(),
	}, proto.ChunkType_SNAPSHOT))
}

func NewBackupWriter(index Index, store ChunkStore) *BackupWriter {
	return &BackupWriter{
		store: store,
		index: index,
		files: make([]*proto.ChunkRef, 0),
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
	ref, info, err := b.index.Stat(name, b.when)
	if err != nil {
		return nil, err
	}

	if ref != nil {
		log.Printf("%s (%d): %x", info.Name, info.Timestamp, ref.Sum)
	} else {
		log.Fatalf("File %s not found", name)
		return nil, os.ErrNotExist
	}

	fileDataChunk, err := b.store.Read(proto.GetTypedRef(info.Data, proto.ChunkType_FILE_DATA))
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
	_, info, err := b.index.Stat(name, b.when)
	if err != nil {
		return nil, err
	}

	if info == nil {
		return nil, os.ErrNotExist
	}

	return &backupFileInfo{info}, nil
}
