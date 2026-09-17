package storage

import (
	"context"
	"testing"

	"github.com/twcclan/goback/auth"
	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

// TestStoreIsALibrary drives two stores in one process without gRPC: each
// call picks a store and runs the operation on it.
func TestStoreIsALibrary(t *testing.T) {
	stores := map[string]*Store{
		"node-1": NewStore(newMemIndex(), nil),
		"node-2": NewStore(newMemIndex(), nil),
	}
	storeOf := func(ctx context.Context) *Store {
		p, err := auth.Require(ctx)
		require.NoError(t, err)

		return stores[p.AgentID]
	}

	first := auth.WithPrincipal(context.Background(), &auth.Principal{AgentID: "node-1"})
	second := auth.WithPrincipal(context.Background(), &auth.Principal{AgentID: "node-2"})

	blob := proto.NewObject(&proto.Blob{Data: []byte("save data")})
	file := proto.NewObject(&proto.File{Parts: []*proto.FilePart{{Length: 9, Ref: blob.Ref()}}})
	tree := proto.NewObject(&proto.Tree{Nodes: []*proto.TreeNode{{Stat: &proto.FileInfo{Name: []byte("a")}, Ref: file.Ref()}}})

	for _, obj := range []*proto.Object{blob, file, tree} {
		receipt, err := storeOf(first).Put(first, Upload{Object: obj, Ref: obj.Ref()})
		require.NoError(t, err)
		require.True(t, receipt.Ref.Equal(obj.Ref()))
		require.Nil(t, receipt.Object, "only commits and pins come back stamped")
	}

	_, err := storeOf(first).Put(first, Upload{Object: blob, Ref: file.Ref()})
	require.ErrorIs(t, err, proto.ErrRefMismatch)

	got, err := storeOf(first).Get(first, tree.Ref())
	require.NoError(t, err)
	require.True(t, got.Ref().Equal(tree.Ref()))

	_, err = storeOf(first).Get(first, blob.Ref())
	require.ErrorIs(t, err, backup.ErrNotFound, "blobs are never served")

	_, err = storeOf(second).Get(second, tree.Ref())
	require.ErrorIs(t, err, backup.ErrNotFound, "the second store never saw the upload")

	var trees []*proto.Ref
	require.NoError(t, storeOf(first).Tree(first, tree.Ref(), 1, func(ref *proto.Ref, _ *proto.Object) error {
		trees = append(trees, ref)
		return nil
	}))
	require.Len(t, trees, 1)

	err = storeOf(first).Tree(first, file.Ref(), 1, func(*proto.Ref, *proto.Object) error { return nil })
	require.ErrorIs(t, err, ErrInvalidRequest, "a file is not a tree")

	var parts []int
	require.NoError(t, storeOf(first).ReadFile(first, file.Ref(), nil, func(index int, obj *proto.Object) error {
		parts = append(parts, index)
		require.True(t, obj.Ref().Equal(blob.Ref()))
		return nil
	}))
	require.Equal(t, []int{0}, parts)

	grant, err := storeOf(first).BeginCommit(first, "world")
	require.NoError(t, err)
	require.Nil(t, grant.Policy, "an index without a gate grants without a policy")

	_, err = storeOf(first).Retention()
	require.ErrorIs(t, err, backup.ErrNotImplemented)

	_, err = storeOf(first).Presence(first, "world")
	require.NoError(t, err, "an index without filters serves none")

	// a store with sessions refuses a write outside one
	packs := testPackStore(t)
	withSessions := NewStore(&packIndex{PackStorage: packs, latest: map[string]*proto.Ref{}}, packs)
	_, err = withSessions.Put(first, Upload{Object: blob, Ref: blob.Ref()})
	require.ErrorIs(t, err, backup.ErrNoSession)

	session, err := withSessions.BeginSession(first, "world", nil)
	require.NoError(t, err)
	require.NotEmpty(t, session.ID)

	_, err = withSessions.Session(second, session.ID)
	require.ErrorIs(t, err, auth.ErrForbidden, "a session belongs to its caller")

	_, err = withSessions.Session(first, "nope")
	require.ErrorIs(t, err, backup.ErrNotFound)

	_, err = withSessions.Put(backup.WithSession(first, session), Upload{Object: blob, Ref: blob.Ref()})
	require.NoError(t, err)
	require.NoError(t, withSessions.EndSession(first, session.ID))
}
