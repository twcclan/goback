package backup

import (
	"time"

	"github.com/pkg/errors"

	"github.com/twcclan/goback/proto"
)

var (
	ErrEmptyBuffer   = errors.New("no buffer space provided")
	ErrIllegalOffset = errors.New("illegal offset")
)

const (
	// bufioReaderSize is an explicit size for our bufio.Reader,
	// so we don't rely on NewReader's implicit size.
	// We care about the buffer size because it affects how far
	// in advance we can detect EOF from an io.Reader that doesn't
	// know its size.  Detecting an EOF bufioReaderSize bytes early
	// means we can plan for the final chunk.
	bufioReaderSize = 32 << 10
)

type BackupWriter struct {
	store ObjectStore
	*backupTree
}

var _ TreeWriter = (*BackupWriter)(nil)

func (br *BackupWriter) Close() error {
	tree := proto.NewObject(&proto.Tree{
		Nodes: br.nodes,
	})

	err := br.store.Put(tree)
	if err != nil {
		return errors.Wrap(err, "Failed to store backup tree")
	}

	commit := proto.NewObject(&proto.Commit{
		Timestamp: time.Now().Unix(),
		Tree:      tree.Ref(),
	})

	err = br.store.Put(commit)

	return errors.Wrap(err, "Failed to store commit")
}

func NewBackupWriter(store ObjectStore) *BackupWriter {
	return &BackupWriter{
		store:      store,
		backupTree: newTree(store),
	}
}

/*
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

type backupFileReader struct {
	store     ChunkStore
	data      *proto.File
	chunk     *proto.Blob
	partIndex int
	offset    int64
}

type searchCallback func(index int) bool

func (bfr *backupFileReader) search(index int) bool {
	part := bfr.data.Parts[index]

	return bfr.offset <= int64(part.Offset+part.Length-1)
}

func (bfr *backupFileReader) Read(b []byte) (n int, err error) {
	// check if we reached EOF

	//if bfr.offset >= int64(len(bfr.data)) {
	if bfr.offset >= 0 {
		return 0, io.EOF
	}

	// the number of bytes requested by the caller
	// we return fewer bytes if we hit a chunk border
	n = len(b)

	// the chunk that represents the currently active part
	// of the file that is being read
	part := bfr.data.Parts[bfr.partIndex]

	// calculate the offset for the currently active chunk
	relativeOffset := bfr.offset - int64(part.Offset)

	// calculate the bytes left for reading in this file part

	bytesRemaining := int64(part.Length) - relativeOffset

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
		//bfr.chunk, err = bfr.store.Read(nil)
	}

	if err != nil {
		return
	}

	// read data from chunk
	copy(b, bfr.chunk.Data[relativeOffset:relativeOffset+int64(n)])

	// if we reached the end of the current chunk
	// we'll unload it and increase the part index
	if relativeOffset+int64(n) == int64(part.Length) {
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

*/
