package backup

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"

	"github.com/twcclan/goback/proto"
)

var (
	// ErrEmptyBuffer is returned when trying to read from a backup file into
	// an empty buffer
	ErrEmptyBuffer = errors.New("no buffer space provided")

	// ErrIllegalOffset is returned when trying to seek past the end of a file
	ErrIllegalOffset = errors.New("illegal offset")

	// ErrSkipFile can be returned during backup to make the TreeWrite skip/ignore a file
	ErrSkipFile = errors.New("skip file")
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
	store     ObjectStore
	backupSet string
	*backupTree
}

var _ TreeWriter = (*BackupWriter)(nil)

func (br *BackupWriter) Close(ctx context.Context) error {
	tree := proto.NewObject(&proto.Tree{
		Nodes: br.nodes,
	})

	err := br.store.Put(ctx, tree)
	if err != nil {
		return errors.Wrap(err, "Failed to store backup tree")
	}

	commit := proto.NewObject(&proto.Commit{
		Timestamp: time.Now().Unix(),
		Tree:      tree.Ref(),
		BackupSet: br.backupSet,
	})

	err = br.store.Put(ctx, commit)

	return errors.Wrap(err, "Failed to store commit")
}

func NewBackupWriter(store ObjectStore, backupSet string) *BackupWriter {
	return &BackupWriter{
		store:      store,
		backupTree: newTree(store),
		backupSet:  backupSet,
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

func (br *BackupReader) ReadFile(ctx context.Context, ref *proto.Ref) (io.ReadSeeker, error) {
	obj, err := br.store.Get(ctx, ref)
	if err != nil {
		return nil, errors.Wrap(err, "Couldn't get object from store")
	}

	if obj == nil {
		return nil, errors.New("Object not found")
	}

	if obj.Type() != proto.ObjectType_FILE {
		return nil, errors.New("Object doesn't describe a file")
	}

	return newFileReader(ctx, br.store, obj.GetFile()), nil
}

type WalkFn func(path string, info os.FileInfo, ref *proto.Ref) error

func (br *BackupReader) walk(ctx context.Context, path string, tree *proto.Tree, walkFn WalkFn) error {
	for _, node := range tree.GetNodes() {
		info := node.Stat
		absPath := filepath.Join(path, info.Name)

		err := walkFn(absPath, proto.GetOSFileInfo(info), node.Ref)
		if err != nil {
			return errors.Wrap(err, "WalkFn returned error")
		}

		if info.Tree {
			subTree, err := br.store.Get(ctx, node.Ref)
			if err != nil {
				return errors.Wrap(err, "Failed retrieving sub-tree")
			}

			err = br.walk(ctx, absPath, subTree.GetTree(), walkFn)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (br *BackupReader) GetTree(ctx context.Context, ref *proto.Ref, parts []string) (*proto.Ref, error) {
	if len(parts) == 0 {
		return ref, nil
	}

	obj, err := br.store.Get(ctx, ref)
	if err != nil {
		return nil, errors.Wrap(err, "Couldn't get object from store")
	}

	if obj == nil {
		return nil, errors.New("Object not found")
	}

	if obj.Type() != proto.ObjectType_TREE {
		return nil, errors.New("Object doesn't describe a tree")
	}

	name := parts[0]
	log.Printf("Searching for %s %v", name, parts)
	for _, node := range obj.GetTree().Nodes {
		if node.Stat.Tree && node.Stat.Name == name {
			return br.GetTree(ctx, node.Ref, parts[1:])
		}
	}

	return nil, errors.New("Folder not found")
}

func (br *BackupReader) WalkTree(ctx context.Context, ref *proto.Ref, walkFn WalkFn) error {
	obj, err := br.store.Get(ctx, ref)
	if err != nil {
		return errors.Wrap(err, "Couldn't get object from store")
	}

	if obj == nil {
		return errors.New("Object not found")
	}

	if obj.Type() != proto.ObjectType_TREE {
		return errors.New("Object doesn't describe a tree")
	}

	return br.walk(ctx, "", obj.GetTree(), walkFn)
}
