package backup

import (
	"io"
	"os"

	"github.com/twcclan/goback/proto"
)

type TreeWriter interface {
	File(os.FileInfo) io.WriteCloser
	Tree(os.FileInfo) TreeWriter
	io.Closer
}

type nodeReceiver interface {
	add(*proto.TreeNode)
}

type backupTree struct {
	parent nodeReceiver
	store  ObjectStore
	info   os.FileInfo
	nodes  []*proto.TreeNode
}

func (bt *backupTree) add(node *proto.TreeNode) {
	bt.nodes = append(bt.nodes, node)
}

func (bt *backupTree) Tree(info os.FileInfo) *backupTree {
	return &backupTree{
		parent: bt,
	}
}

func (bt *backupTree) File(info os.FileInfo) io.WriteCloser {
	return nil
}

func (bt *backupTree) Close() error {
	obj := proto.NewObject(&proto.Tree{
		Nodes: bt.nodes,
	})

	err := bt.store.Put()
}

func newTree(info os.FileInfo, store ObjectStore) *backupTree {
	return &backupTree{
		info:  info,
		store: store,
		nodes: make([]*proto.TreeNode, 0),
	}
}
