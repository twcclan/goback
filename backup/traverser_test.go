package backup

import (
	"context"
	"sync"
	"testing"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var testObjects = map[string]*proto.Object{
	"root": proto.NewObject(&proto.Tree{
		Nodes: []*proto.TreeNode{
			{
				Stat: &proto.FileInfo{
					Name: []byte("test.dir"),
					Type: proto.NodeType_NODE_DIRECTORY,
				},
				Ref: testRef("test.dir"),
			},
			{
				Stat: &proto.FileInfo{
					Name: []byte("test.file1"),
				},
				Ref: testRef("test.file1"),
			},
		},
	}),
	"test.dir": proto.NewObject(&proto.Tree{
		Nodes: []*proto.TreeNode{
			{
				Stat: &proto.FileInfo{
					Name: []byte("test.file2"),
				},
				Ref: testRef("test.file2"),
			},
		},
	}),
	"test.file1": proto.NewObject(&proto.File{}),
	"test.file2": proto.NewObject(&proto.File{}),
}

func TestConcurrentTreeTraverser(t *testing.T) {
	for i := 1; i < 10; i++ {
		store := &MockObjectStore{}
		store.On("Get", mock.Anything, testRef("test.dir")).Return(testObjects["test.dir"], nil).Once()
		expected := map[string]bool{
			"test.dir":            true,
			"test.dir/test.file2": true,
			"test.file1":          true,
		}

		var mtx sync.Mutex
		traverser := &concurrentTreeTraverser{
			store: store,
			queue: make(chan *concurrentTreeNode),

			traverseFn: func(path string, node *proto.TreeNode) error {
				mtx.Lock()
				delete(expected, path)
				mtx.Unlock()
				return nil
			},
		}

		root := testObjects["root"]

		err := traverser.run(context.Background(), root, i)
		assert.Nil(t, err)
		assert.Empty(t, expected)
	}
}
