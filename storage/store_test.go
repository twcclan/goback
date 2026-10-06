package storage

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"

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
	require.NoError(t, storeOf(first).Tree(first, tree.Ref(), 1, func(resp *proto.GetTreeResponse) error {
		trees = append(trees, resp.Ref)
		return nil
	}))
	require.Len(t, trees, 1)

	err = storeOf(first).Tree(first, file.Ref(), 1, func(*proto.GetTreeResponse) error { return nil })
	require.ErrorIs(t, err, ErrInvalidRequest, "a file is not a tree")

	var parts []int
	require.NoError(t, storeOf(first).ReadFile(first, file.Ref(), nil, func(resp *proto.ReadFileResponse) error {
		parts = append(parts, int(resp.Index))
		require.True(t, resp.Object.Ref().Equal(blob.Ref()))
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

// slowTrees reads its trees with jitter, failing the one named by fail,
// and locates every other ref it is asked about.
type slowTrees struct {
	*memIndex
	fail     []byte
	inFlight atomic.Int32
	peak     atomic.Int32
}

var errTreeRead = errors.New("tree read failed")

func (s *slowTrees) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	n := s.inFlight.Add(1)
	defer s.inFlight.Add(-1)

	for peak := s.peak.Load(); n > peak && !s.peak.CompareAndSwap(peak, n); peak = s.peak.Load() {
	}

	time.Sleep(time.Duration(rand.Intn(500)) * time.Microsecond)

	if bytes.Equal(ref.Hash, s.fail) {
		return nil, errTreeRead
	}

	return s.memIndex.Get(ctx, ref)
}

func (s *slowTrees) LocateRecords(_ context.Context, refs []*proto.Ref) ([]*proto.LocatedRun, error) {
	var runs []*proto.LocatedRun
	for i := 0; i < len(refs); i += 2 {
		runs = append(runs, &proto.LocatedRun{
			Location: &proto.Location{Url: hex.EncodeToString(refs[i].Hash)},
			Records:  []*proto.LocatedRecord{{Index: uint32(i)}},
		})
	}

	return runs, nil
}

// TestTreeWalksInOrderWhileReadingAhead walks 40 directories of 30
// directories, each holding one more below maxDepth, more than a locate
// batch, and expects every tree under maxDepth once, at the position of a
// serial breadth-first walk.
func TestTreeWalksInOrderWhileReadingAhead(t *testing.T) {
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{AgentID: "node-1"})
	index := &slowTrees{memIndex: newMemIndex()}

	put := func(children ...*proto.Ref) *proto.Ref {
		var nodes []*proto.TreeNode
		for i, child := range children {
			nodes = append(nodes, &proto.TreeNode{Stat: &proto.FileInfo{Name: []byte(fmt.Sprintf("dir%03d", i)), Type: proto.NodeType_NODE_DIRECTORY}, Ref: child})
		}

		nodes = append(nodes, &proto.TreeNode{Stat: &proto.FileInfo{Name: []byte("link"), Type: proto.NodeType_NODE_SYMLINK, LinkTarget: []byte(fmt.Sprint(rand.Int63()))}})

		tree := proto.NewObject(&proto.Tree{Nodes: nodes})
		require.NoError(t, index.Put(ctx, tree))

		return tree.Ref()
	}

	var tops, mids []*proto.Ref
	for range 40 {
		var below []*proto.Ref
		for range 30 {
			below = append(below, put(put()))
		}

		tops = append(tops, put(below...))
		mids = append(mids, below...)
	}

	root := put(tops...)
	want := append(append([]*proto.Ref{root}, tops...), mids...)

	walk := func(index backup.Index) []*proto.Ref {
		got := make([]*proto.Ref, len(want))
		var objects []*proto.Ref

		require.NoError(t, NewStore(index, nil).Tree(ctx, root, 2, func(resp *proto.GetTreeResponse) error {
			for _, run := range resp.Runs {
				i := run.Records[0].Index
				require.Nil(t, got[i], "index %d handed out twice", i)

				hash, err := hex.DecodeString(run.Location.Url)
				require.NoError(t, err)
				got[i] = &proto.Ref{Hash: hash}
			}

			if resp.Object != nil {
				objects = append(objects, resp.Ref)
			}

			return nil
		}))

		for i := range got {
			if got[i] == nil {
				require.NotEmpty(t, objects, "fewer trees than expected")
				got[i], objects = objects[0], objects[1:]
			}
		}
		require.Empty(t, objects, "more trees than expected")

		return got
	}

	for _, located := range []bool{false, true} {
		var idx backup.Index = struct{ backup.Index }{index}
		if located {
			idx = index
		}

		for range 3 {
			got := walk(idx)
			for i := range want {
				require.True(t, want[i].Equal(got[i]), "tree %d out of place", i)
			}
		}
	}

	require.Greater(t, index.peak.Load(), int32(1), "trees are read concurrently")
	require.LessOrEqual(t, index.peak.Load(), int32(treeWorkers))

	index.fail = mids[len(mids)/2].Hash
	err := NewStore(index, nil).Tree(ctx, root, 2, func(*proto.GetTreeResponse) error { return nil })
	require.ErrorIs(t, err, errTreeRead)
}

func TestStoreRefusesAPolicyFromAClient(t *testing.T) {
	store := NewStore(newMemIndex(), nil)
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{AgentID: "node-1"})

	policy := proto.NewObject(&proto.Policy{Sequence: 1, Scope: &proto.Policy_Store{Store: &proto.StoreScope{}}})
	sealed := proto.NewObject(&proto.Sealed{Ref: policy.Ref(), Type: proto.ObjectType_POLICY, Data: []byte("x"), Encryption: proto.Encryption_SEALED})

	for _, obj := range []*proto.Object{policy, sealed} {
		_, err := store.Put(ctx, Upload{Object: obj})
		require.ErrorIs(t, err, ErrInvalidRequest)
	}
}
