package pack

import (
	"math/rand"
	"testing"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func batchObjects(objects []*proto.Object, batchSize int) [][]*proto.Object {
	var batches [][]*proto.Object

	for batchSize < len(objects) {
		objects, batches = objects[batchSize:], append(batches, objects[0:batchSize:batchSize])
	}
	batches = append(batches, objects)

	return batches
}

func makeGCFiles(parts []*proto.Object) []*proto.Object {
	var files []*proto.Object
	for _, batch := range batchObjects(parts, 5) {
		var parts []*proto.FilePart
		for _, obj := range batch {
			parts = append(parts, &proto.FilePart{
				Ref: obj.Ref(),
			})
		}

		files = append(files, proto.NewObject(&proto.File{
			Parts: parts,
		}))
	}

	return files
}

func makeGCTrees(nodes []*proto.Object) []*proto.Object {
	var trees []*proto.Object
	for _, batch := range batchObjects(nodes, 3) {
		var treeNodes []*proto.TreeNode
		for _, obj := range batch {
			treeNodes = append(treeNodes, &proto.TreeNode{
				Ref: obj.Ref(),
			})
		}

		trees = append(trees, proto.NewObject(&proto.Tree{
			Nodes: treeNodes,
		}))
	}

	return trees
}

func makeGCCommits(trees []*proto.Object) []*proto.Object {
	var commits []*proto.Object
	for _, tree := range trees {
		commits = append(commits, proto.NewObject(&proto.Commit{
			Tree: tree.Ref(),
		}))
	}

	return commits
}

// makeGCTestData returns two slices of objects. the first one contains objects that
// should be marked reachable after a gc mark phase and the second contains only
// objects that should be unreachable
func makeGCTestData(t *testing.T) ([]*proto.Object, []*proto.Object) {
	var reachable []*proto.Object
	var unreachable []*proto.Object

	// generate a bunch of blobs
	blobs := makeTestData(t, numObjects)
	perm := rand.Perm(len(blobs))

	var reachableBlobs []*proto.Object
	for _, i := range perm[:len(perm)/2] {
		reachableBlobs = append(reachableBlobs, blobs[i])
	}

	var unreachableBlobs []*proto.Object
	for _, i := range perm[len(perm)/2:] {
		unreachableBlobs = append(unreachableBlobs, blobs[i])
	}
	require.EqualValues(t, len(perm), len(reachableBlobs)+len(unreachableBlobs))

	// generate some files
	reachableFiles := makeGCFiles(reachableBlobs)
	unreachableFiles := append(makeGCFiles(unreachableBlobs))

	// TODO: generate file splits

	// generate some trees
	t.Log("generating reachable trees")
	reachableTrees := makeGCTrees(reachableFiles)
	unreachableTrees := append(makeGCTrees(unreachableFiles))

	commits := makeGCCommits(reachableTrees)

	reachable = append(reachable, reachableBlobs...)
	reachable = append(reachable, reachableFiles...)
	reachable = append(reachable, reachableTrees...)
	reachable = append(reachable, commits...)

	unreachable = append(unreachable, unreachableBlobs...)
	unreachable = append(unreachable, unreachableFiles...)
	unreachable = append(unreachable, unreachableTrees...)

	return reachable, unreachable
}

//func TestMark(t *testing.T) {
//	base := getTempDir(t)
//	storage := NewLocalArchiveStorage(base)
//	options := []PackOption{
//		WithArchiveStorage(storage),
//		WithMaxParallel(4),
//		WithMaxSize(1024 * 1024),
//	}
//	store, err := NewPackStorage(options...)
//	require.Nil(t, err)
//
//	reachable, unreachable := makeGCTestData(t)
//	t.Logf("Storing %d objects", len(reachable)+len(unreachable))
//
//	var allObjects []*proto.Object
//	allObjects = append(allObjects, reachable...)
//	allObjects = append(allObjects, unreachable...)
//
//	for _, i := range rand.Perm(len(allObjects)) {
//		require.Nil(t, store.Put(context.Background(), allObjects[i]))
//	}
//
//	require.Nil(t, store.Flush())
//	require.Nil(t, store.doMark())
//
//	for _, obj := range reachable {
//		require.Truef(t, store.isObjectReachable(obj.Ref()), "expected object %x of type %s to be reachable", obj.Ref().Sha1, obj.Type())
//	}
//
//	for _, obj := range unreachable {
//		require.Falsef(t, store.isObjectReachable(obj.Ref()), "expected object %x of type %s to be unreachable", obj.Ref().Sha1, obj.Type())
//	}
//
//	require.Nil(t, store.Close())
//}
