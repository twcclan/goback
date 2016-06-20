package backup

import (
	"io"
	"log"
	"os"
	"path"
	"sync/atomic"
	"time"

	"camlistore.org/pkg/rollsum"
	"github.com/twcclan/goback/proto"
	"go4.org/syncutil"
)

func NewCommit(folder string, store ObjectStore) (*proto.Commit, error) {
	return (&commitWriter{store: store}).do(folder)
}

type commitWriter struct {
	store ObjectStore
}

func (c *commitWriter) do(folder string) (*proto.Commit, error) {
	out := new(proto.Commit)
	ref, err := c.tree(folder)
	if err != nil {
		return nil, err
	}

	out.Tree = ref
	out.Timestamp = time.Now().UTC().Unix()

	return out, c.store.Put(proto.NewObject(out))
}

func (c *commitWriter) file(name string) (*proto.Ref, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	writer := &fileWriter{
		store:            c.store,
		rs:               rollsum.New(),
		parts:            make([]*proto.FilePart, 0),
		storageErr:       new(atomic.Value),
		storageSemaphore: syncutil.NewGate(inFlightChunks),
	}

	_, err = io.Copy(writer, file)
	if err != nil {
		return nil, err
	}

	err = writer.Close()
	return writer.Ref(), err
}

func (c *commitWriter) tree(current string) (*proto.Ref, error) {

	// open input file to query FileInfo
	file, err := os.Open(current)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		log.Fatal(err)
	}

	tree := newTree(info, c.store)

	files, err := file.Readdir(0)
	if err != nil {
		return nil, err
	}

	// iterate over all files in this folder
	for _, file := range files {
		abs := path.Join(current, file.Name())
		node := &proto.TreeNode{
			Stat: proto.GetFileInfo(file),
		}

		// if this is a folder, recursively continue scanning
		// if it's a file, read it
		if file.IsDir() {
			node.Ref, err = c.tree(abs)
		} else {
			node.Ref, err = c.file(abs)
		}

		if err != nil {
			return nil, err
		}

		tree.Add(node)
	}

	object := proto.NewObject(&proto.Tree{
		Nodes: tree.nodes,
	})

	return object.Ref(), c.store.Put(object)
}
