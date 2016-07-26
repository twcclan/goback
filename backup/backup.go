package backup

import (
	"io"
	"os"
	"path/filepath"
	"sort"
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

type BackupReader struct {
	store ObjectStore
}

func NewBackupReader(store ObjectStore) *BackupReader {
	return &BackupReader{
		store: store,
	}
}

func (br *BackupReader) ReadFile(ref *proto.Ref) (io.ReadSeeker, error) {
	obj, err := br.store.Get(ref)
	if err != nil {
		return nil, errors.Wrap(err, "Couldn't get object from store")
	}

	if obj == nil {
		return nil, errors.New("Object not found")
	}

	if obj.Type() != proto.ObjectType_FILE {
		return nil, errors.New("Object doesn't describe a file")
	}

	return &backupFileReader{
		store: br.store,
		file:  obj.GetFile(),
	}, nil
}

type WalkFn func(path string, info os.FileInfo, ref *proto.Ref) error

func (br *BackupReader) walk(path string, tree *proto.Tree, walkFn WalkFn) error {
	for _, node := range tree.GetNodes() {
		info := node.Stat
		absPath := filepath.Join(path, info.Name)

		err := walkFn(absPath, proto.GetOSFileInfo(info), node.Ref)
		if err != nil {
			return errors.Wrap(err, "WalkFn returned error")
		}

		if info.Tree {
			subTree, err := br.store.Get(node.Ref)
			if err != nil {
				return errors.Wrap(err, "Failed retrieving sub-tree")
			}

			return br.walk(absPath, subTree.GetTree(), walkFn)
		}
	}

	return nil
}

func (br *BackupReader) WalkTree(ref *proto.Ref, walkFn WalkFn) error {
	obj, err := br.store.Get(ref)
	if err != nil {
		return errors.Wrap(err, "Couldn't get object from store")
	}

	if obj == nil {
		return errors.New("Object not found")
	}

	if obj.Type() != proto.ObjectType_TREE {
		return errors.New("Object doesn't describe a tree")
	}

	return br.walk("", obj.GetTree(), walkFn)
}

type backupFileReader struct {
	store     ObjectStore
	file      *proto.File
	blob      *proto.Object
	partIndex int
	offset    int64
}

type searchCallback func(index int) bool

func (bfr *backupFileReader) search(index int) bool {
	part := bfr.file.Parts[index]

	return bfr.offset <= int64(part.Offset+part.Length-1)
}

func (bfr *backupFileReader) Size() int64 {
	parts := bfr.file.Parts
	length := len(parts)
	if length > 0 {
		last := parts[length-1]
		return int64(last.Offset + last.Length)
	}

	return 0
}

func (bfr *backupFileReader) Read(b []byte) (n int, err error) {
	// check if we reached EOF

	if bfr.offset >= bfr.Size() {
		return 0, io.EOF
	}

	// the number of bytes requested by the caller
	// we return fewer bytes if we hit a chunk border
	n = len(b)

	// the chunk that represents the currently active part
	// of the file that is being read
	part := bfr.file.Parts[bfr.partIndex]

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

	// lazily load blob from our object store
	if bfr.blob == nil {
		bfr.blob, err = bfr.store.Get(part.Ref)
	}

	if err != nil {
		return
	}

	// read data from chunk
	copy(b, bfr.blob.GetBlob().Data[relativeOffset:relativeOffset+int64(n)])

	// if we reached the end of the current chunk
	// we'll unload it and increase the part index
	if relativeOffset+int64(n) == int64(part.Length) {
		bfr.blob = nil
		bfr.partIndex++
	}

	// increase offset
	bfr.offset += int64(n)

	return
}

func (bfr *backupFileReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case os.SEEK_SET:
		bfr.offset = offset
	case os.SEEK_CUR:
		bfr.offset += offset
	case os.SEEK_END:
		bfr.offset = bfr.Size() - 1 - offset
	}

	if bfr.offset < 0 || bfr.offset > bfr.Size() {
		return bfr.offset, ErrIllegalOffset
	}

	// find the part that cointains the requested data
	if i := sort.Search(len(bfr.file.Parts), bfr.search); i != bfr.partIndex {
		// if it is not the currently active part
		// unload the currently loaded chunk
		bfr.partIndex = i
		bfr.blob = nil
	}

	return bfr.offset, nil
}
