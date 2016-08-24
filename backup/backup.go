package backup

import (
	"io"
	"os"
	"path/filepath"
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

	return &fileReader{
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

			err = br.walk(absPath, subTree.GetTree(), walkFn)
			if err != nil {
				return err
			}
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
