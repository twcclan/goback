package backup

import (
	"context"
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
					Name: "test.dir",
					Tree: true,
				},
				Ref: &proto.Ref{Sha1: []byte("test.dir")},
			},
			{
				Stat: &proto.FileInfo{
					Name: "test.file1",
				},
				Ref: &proto.Ref{Sha1: []byte("test.file1")},
			},
		},
	}),
	"test.dir": proto.NewObject(&proto.Tree{
		Nodes: []*proto.TreeNode{
			{
				Stat: &proto.FileInfo{
					Name: "test.file2",
				},
				Ref: &proto.Ref{Sha1: []byte("test.file2")},
			},
		},
	}),
	"test.file1": proto.NewObject(&proto.File{}),
	"test.file2": proto.NewObject(&proto.File{}),
}

func TestConcurrentTreeTraverser(t *testing.T) {
	for i := 1; i < 10; i++ {
		store := &MockObjectStore{}
		store.On("Get", mock.Anything, &proto.Ref{Sha1: []byte("test.dir")}).Return(testObjects["test.dir"], nil).Once()
		expected := map[string]bool{
			"test.dir":            true,
			"test.dir/test.file2": true,
			"test.file1":          true,
		}

		traverser := &concurrentTreeTraverser{
			store: store,
			queue: make(chan *concurrentTreeNode),

			traverseFn: func(path string, node *proto.TreeNode) error {
				delete(expected, path)
				return nil
			},
		}

		root := testObjects["root"]

		err := traverser.run(context.Background(), root, i)
		assert.Nil(t, err)
		assert.Empty(t, expected)
	}
}
