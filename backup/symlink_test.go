package backup

import (
	"context"
	"os"
	"testing"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func TestSymlinkNodesRoundTrip(t *testing.T) {
	store := newMemStore()
	writer := NewBackupWriter(store, "set")

	writer.Node(&proto.TreeNode{
		Stat: &proto.FileInfo{Name: []byte("link"), Mode: uint32(os.ModeSymlink | 0777), Type: proto.NodeType_NODE_SYMLINK, LinkTarget: []byte("../shared/plugins")},
	})
	writer.Node(&proto.TreeNode{
		Stat: &proto.FileInfo{Name: []byte("file"), Mode: 0644, Size: 1},
		Ref:  testRef("file"),
	})

	tree := proto.NewObject(&proto.Tree{Nodes: writer.sortedNodes()})
	require.NoError(t, store.Put(context.Background(), tree))

	var seen []string
	err := NewBackupReader(store).WalkTree(context.Background(), tree.Ref(), nil, func(path string, info os.FileInfo, ref *proto.Ref) error {
		seen = append(seen, path)

		if path == "link" {
			require.Nil(t, ref)
			stat := info.Sys().(*proto.FileInfo)
			require.Equal(t, proto.NodeType_NODE_SYMLINK, stat.Type)
			require.Equal(t, "../shared/plugins", string(stat.LinkTarget))
		}

		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"file", "link"}, seen)

	// the details wrapper carries the target into GetFileInfo
	info := proto.GetFileInfo(proto.WithDetails(proto.GetOSFileInfo(&proto.FileInfo{Name: []byte("x"), Mode: uint32(os.ModeSymlink)}), "root", "wheel", "target"))
	require.Equal(t, proto.NodeType_NODE_SYMLINK, info.Type)
	require.Equal(t, "target", string(info.LinkTarget))
	require.Equal(t, "root", string(info.User))
	require.Equal(t, "wheel", string(info.Group))
}
