package backup

import (
	"bytes"
	"context"
	"errors"
	"hash/fnv"
	"io"
	"os"
	"sort"
	"sync"

	"github.com/twcclan/goback/backup/storekey"
	"github.com/twcclan/goback/proto"
)

const (
	// treeFanout is the node count above which a directory is split into
	// sub-tree objects.
	treeFanout = 256
	// treeSplitMin and treeSplitMax bound one split's node count; the mean is
	// treeSplitAvg, chosen by a hash of the entry name so a change rewrites
	// O(1) splits.
	treeSplitMin = 64
	treeSplitAvg = 256
	treeSplitMax = 1024
)

type TreeWriter interface {
	File(context.Context, os.FileInfo, func(io.Writer) error) error
	Tree(context.Context, os.FileInfo, func(TreeWriter) error) error
	Node(*proto.TreeNode)
}

type backupTree struct {
	store    ObjectStore
	nodes    []*proto.TreeNode
	nodesMtx sync.Mutex
}

var _ TreeWriter = (*backupTree)(nil)

func (bt *backupTree) Tree(ctx context.Context, info os.FileInfo, writer func(TreeWriter) error) error {
	node := &backupTree{
		nodes: make([]*proto.TreeNode, 0),
		store: bt.store,
	}

	// allow the caller to populate this sub-tree
	err := writer(node)
	if err != nil {
		if errors.Is(err, ErrSkipFile) {
			return nil
		}
		return err
	}

	ref, err := PutTree(ctx, bt.store, node.sortedNodes(), nil, nil)
	if err != nil {
		return err
	}

	// save a reference to the sub-tree
	bt.Node(&proto.TreeNode{
		Stat: proto.GetFileInfo(info),
		Ref:  ref,
	})

	return nil
}

func (bt *backupTree) File(ctx context.Context, info os.FileInfo, writer func(io.Writer) error) error {
	fWriter := newFileWriter(ctx, bt.store, nil, info.Size())
	node := &proto.TreeNode{
		Stat: proto.GetFileInfo(info),
	}

	err := writer(fWriter)
	if err != nil {
		// if the writer is asking to skip the file just continue
		if errors.Is(err, ErrSkipFile) {
			return nil
		}

		return err
	}

	bt.Node(node)

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

// sortedNodes orders nodes by name, since they are appended from parallel
// workers and the tree ref must not depend on arrival order.
func (bt *backupTree) sortedNodes() []*proto.TreeNode {
	bt.nodesMtx.Lock()
	defer bt.nodesMtx.Unlock()

	return SortNodes(bt.nodes)
}

// SortNodes orders tree nodes bytewise by name, as the canonical encoding
// requires.
func SortNodes(nodes []*proto.TreeNode) []*proto.TreeNode {
	sort.Slice(nodes, func(i int, j int) bool {
		return bytes.Compare(nodes[i].Stat.Name, nodes[j].Stat.Name) < 0
	})

	return nodes
}

func newTree(store ObjectStore) *backupTree {
	return &backupTree{
		store: store,
		nodes: make([]*proto.TreeNode, 0),
	}
}

// PutTree stores a directory's nodes as one tree object, or as split trees
// under a parent when the directory is large, and returns the ref of the
// object a parent node should reference. With a key the nodes' names are
// sealed for the directory whose token is parent; the given nodes are not
// modified.
func PutTree(ctx context.Context, store ObjectStore, nodes []*proto.TreeNode, key *storekey.Key, parent []byte) (*proto.Ref, error) {
	nodes = sealNodes(key, parent, nodes)

	if len(nodes) <= treeFanout {
		return putTreeObject(ctx, store, &proto.Tree{Nodes: nodes}, key)
	}

	var splits []*proto.Ref
	for _, chunk := range splitNodes(nodes) {
		ref, err := putTreeObject(ctx, store, &proto.Tree{Nodes: chunk}, key)
		if err != nil {
			return nil, err
		}

		splits = append(splits, ref)
	}

	return putTreeObject(ctx, store, &proto.Tree{Splits: splits}, key)
}

func putTreeObject(ctx context.Context, store ObjectStore, tree *proto.Tree, key *storekey.Key) (*proto.Ref, error) {
	obj := proto.NewObject(tree)
	if key != nil {
		obj.KeyId = key.ID()
	}

	err := store.Put(ctx, obj)
	if err != nil {
		return nil, err
	}

	return obj.Ref(), nil
}

// splitNodes cuts a sorted node list at name-defined boundaries.
func splitNodes(nodes []*proto.TreeNode) [][]*proto.TreeNode {
	var (
		chunks [][]*proto.TreeNode
		start  int
	)

	for i, node := range nodes {
		size := i - start + 1
		if size < treeSplitMin {
			continue
		}

		if size >= treeSplitMax || nameBoundary(node.Stat.Name) {
			chunks = append(chunks, nodes[start:i+1])
			start = i + 1
		}
	}

	if start < len(nodes) {
		chunks = append(chunks, nodes[start:])
	}

	return chunks
}

func nameBoundary(name []byte) bool {
	h := fnv.New64a()
	h.Write(name)

	return h.Sum64()%treeSplitAvg == 0
}

// Getter is the read side of an ObjectStore.
type Getter interface {
	Get(context.Context, *proto.Ref) (*proto.Object, error)
}

// OpenTree loads a tree and opens its names for the directory whose token
// is parent. Without a key it is LoadTree.
func OpenTree(ctx context.Context, store Getter, ref *proto.Ref, key *storekey.Key, parent []byte) (*proto.Tree, error) {
	tree, err := LoadTree(ctx, store, ref)
	if err != nil {
		return nil, err
	}

	if key == nil {
		return tree, nil
	}

	nodes, err := openNodes(key, parent, tree.Nodes)
	if err != nil {
		return nil, err
	}

	return &proto.Tree{Nodes: nodes}, nil
}

// LoadTree fetches a tree and flattens its splits into one node list.
func LoadTree(ctx context.Context, store Getter, ref *proto.Ref) (*proto.Tree, error) {
	obj, err := store.Get(ctx, ref)
	if err != nil {
		return nil, err
	}

	if obj.Type() != proto.ObjectType_TREE {
		return nil, errors.New("object is not a tree")
	}

	tree := obj.GetTree()
	if len(tree.Splits) == 0 {
		return tree, nil
	}

	flat := &proto.Tree{}
	for _, split := range tree.Splits {
		sub, err := LoadTree(ctx, store, split)
		if err != nil {
			return nil, err
		}

		flat.Nodes = append(flat.Nodes, sub.Nodes...)
	}

	return flat, nil
}
