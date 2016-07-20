package backup

import (
	"io"
	"os"
	"sync/atomic"

	"camlistore.org/pkg/rollsum"
	"go4.org/syncutil"

	"github.com/twcclan/goback/proto"
)

type TreeWriter interface {
	File(os.FileInfo, func(io.Writer) error) error
	Tree(os.FileInfo, func(TreeWriter) error) error
	Node(*proto.TreeNode)
}

type backupTree struct {
	store ObjectStore
	nodes []*proto.TreeNode
}

var _ TreeWriter = (*backupTree)(nil)

func (bt *backupTree) Tree(info os.FileInfo, writer func(TreeWriter) error) error {
	node := &backupTree{
		nodes: make([]*proto.TreeNode, 0),
		store: bt.store,
	}

	// allow the caller to populate this sub-tree
	err := writer(node)
	if err != nil {
		return err
	}

	treeObj := proto.NewObject(&proto.Tree{
		Nodes: node.nodes,
	})

	// store the sub-tree
	err = bt.store.Put(treeObj)
	if err != nil {
		return err
	}

	// save a reference to the sub-tree
	bt.nodes = append(bt.nodes, &proto.TreeNode{
		Stat: proto.GetFileInfo(info),
		Ref:  treeObj.Ref(),
	})

	return err
}

func (bt *backupTree) File(info os.FileInfo, writer func(io.Writer) error) error {
	fWriter := &fileWriter{
		store:            bt.store,
		rs:               rollsum.New(),
		parts:            make([]*proto.FilePart, 0),
		storageErr:       new(atomic.Value),
		storageSemaphore: syncutil.NewGate(inFlightChunks),
	}

	err := writer(fWriter)
	if err != nil {
		return err
	}

	// closing the writer will finish uploading all parts
	// and also store the metadata
	err = fWriter.Close()
	if err != nil {
		return err
	}

	bt.nodes = append(bt.nodes, &proto.TreeNode{
		Stat: proto.GetFileInfo(info),
		Ref:  fWriter.Ref(),
	})

	return nil
}

func (bt *backupTree) Node(node *proto.TreeNode) {
	bt.nodes = append(bt.nodes, node)
}

func newTree(store ObjectStore) *backupTree {
	return &backupTree{
		store: store,
		nodes: make([]*proto.TreeNode, 0),
	}
}
