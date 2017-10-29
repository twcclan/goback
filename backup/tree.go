package backup

import (
	"io"
	"os"
	"sort"
	"sync"

	"github.com/twcclan/goback/proto"
)

type TreeWriter interface {
	File(os.FileInfo, func(io.Writer) error) error
	Tree(os.FileInfo, func(TreeWriter) error) error
	Node(*proto.TreeNode)
}

type backupTree struct {
	store    ObjectStore
	nodes    []*proto.TreeNode
	nodesMtx sync.Mutex
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

	// make sure the nodes are sorted deterministically
	sort.Slice(node.nodes, func(i int, j int) bool {
		return node.nodes[i].Stat.Name < node.nodes[j].Stat.Name
	})

	treeObj := proto.NewObject(&proto.Tree{
		Nodes: node.nodes,
	})

	// store the sub-tree
	err = bt.store.Put(treeObj)
	if err != nil {
		return err
	}

	// save a reference to the sub-tree
	bt.Node(&proto.TreeNode{
		Stat: proto.GetFileInfo(info),
		Ref:  treeObj.Ref(),
	})

	return err
}

func (bt *backupTree) File(info os.FileInfo, writer func(io.Writer) error) error {
	fWriter := newFileWriter(bt.store)
	node := &proto.TreeNode{
		Stat: proto.GetFileInfo(info),
	}

	bt.Node(node)

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

	node.Ref = fWriter.Ref()

	return nil
}

func (bt *backupTree) Node(node *proto.TreeNode) {
	bt.nodesMtx.Lock()
	bt.nodes = append(bt.nodes, node)
	bt.nodesMtx.Unlock()
}

func newTree(store ObjectStore) *backupTree {
	return &backupTree{
		store: store,
		nodes: make([]*proto.TreeNode, 0),
	}
}
