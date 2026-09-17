package backup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"

	"github.com/twcclan/goback/backup/storekey"
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

// BackupWriter collects a plaintext tree through the TreeWriter methods and
// commits it on Close.
type BackupWriter struct {
	store     ObjectStore
	backupSet string
	*backupTree
}

var _ TreeWriter = (*BackupWriter)(nil)

// Close stores the collected tree and a commit pointing at it.
func (br *BackupWriter) Close(ctx context.Context) error {
	tree, err := PutTree(ctx, br.store, br.sortedNodes(), nil, nil)
	if err != nil {
		return errors.Wrap(err, "Failed to store backup tree")
	}

	commit := proto.NewObject(&proto.Commit{
		Timestamp: time.Now().Unix(),
		Tree:      tree,
		BackupSet: br.backupSet,
	})

	err = br.store.Put(ctx, commit)

	return errors.Wrap(err, "Failed to store commit")
}

// NewBackupWriter returns a writer that commits into backupSet.
func NewBackupWriter(store ObjectStore, backupSet string) *BackupWriter {
	return &BackupWriter{
		store:      store,
		backupTree: newTree(store),
		backupSet:  backupSet,
	}
}

// BackupReader reads the files and trees of a commit back from a store.
type BackupReader struct {
	store ObjectStore
	key   *storekey.Key
}

// NewBackupReader returns a reader for a plaintext store; WithKey adds the
// store key.
func NewBackupReader(store ObjectStore) *BackupReader {
	return &BackupReader{
		store: store,
	}
}

// WithKey returns a reader that opens names and blobs with the store key.
func (br *BackupReader) WithKey(key *storekey.Key) *BackupReader {
	return &BackupReader{store: br.store, key: key}
}

// ReadFile opens the file object at ref for seekable reading.
func (br *BackupReader) ReadFile(ctx context.Context, ref *proto.Ref) (io.ReadSeeker, error) {
	obj, err := br.store.Get(ctx, ref)
	if err != nil {
		return nil, errors.Wrapf(err, "Couldn't get file %x from store", ref.Hash)
	}

	if obj.Type() != proto.ObjectType_FILE {
		return nil, errors.New("Object doesn't describe a file")
	}

	return newFileReader(ctx, br.store, obj.GetFile(), br.key), nil
}

// WalkFn is called for every node a walk visits; a non-nil error stops the
// walk.
type WalkFn func(path string, info os.FileInfo, ref *proto.Ref) error

// walk visits an opened tree; parent is the token of the directory the
// tree describes, which opens the names of its subdirectories.
func (br *BackupReader) walk(ctx context.Context, path string, parent []byte, tree *proto.Tree, walkFn WalkFn) error {
	for _, node := range tree.GetNodes() {
		info := node.Stat
		absPath := filepath.Join(path, string(info.Name))

		err := walkFn(absPath, proto.GetOSFileInfo(info), node.Ref)
		if err != nil {
			return errors.Wrap(err, "WalkFn returned error")
		}

		if info.IsDir() {
			token := NameToken(br.key, parent, info.Name)

			subTree, err := OpenTree(ctx, br.store, node.Ref, br.key, token)
			if err != nil {
				return errors.Wrapf(err, "Failed retrieving sub-tree %x for %s", node.Ref.Hash, absPath)
			}

			err = br.walk(ctx, absPath, token, subTree, walkFn)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// GetTree descends from ref along the plaintext path parts and returns the
// tree ref at the end, with the token of the directory it names.
func (br *BackupReader) GetTree(ctx context.Context, ref *proto.Ref, parts []string) (*proto.Ref, []byte, error) {
	return br.getTree(ctx, ref, nil, parts)
}

func (br *BackupReader) getTree(ctx context.Context, ref *proto.Ref, parent []byte, parts []string) (*proto.Ref, []byte, error) {
	if len(parts) == 0 {
		return ref, parent, nil
	}

	tree, err := OpenTree(ctx, br.store, ref, br.key, parent)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "Couldn't get tree %x from store", ref.Hash)
	}

	name := parts[0]
	for _, node := range tree.Nodes {
		if node.Stat.IsDir() && string(node.Stat.Name) == name {
			return br.getTree(ctx, node.Ref, NameToken(br.key, parent, node.Stat.Name), parts[1:])
		}
	}

	return nil, nil, errors.New("Folder not found")
}

// WalkTree walks the tree at ref, whose directory has the given token (nil
// for the root of a commit).
func (br *BackupReader) WalkTree(ctx context.Context, ref *proto.Ref, parent []byte, walkFn WalkFn) error {
	tree, err := OpenTree(ctx, br.store, ref, br.key, parent)
	if err != nil {
		return errors.Wrapf(err, "Couldn't get tree %x from store", ref.Hash)
	}

	return br.walk(ctx, "", parent, tree, walkFn)
}
