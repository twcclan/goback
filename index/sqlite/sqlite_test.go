package sqlite

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

type memStore struct {
	mtx     sync.RWMutex
	objects map[string]*proto.Object
}

func (m *memStore) Put(_ context.Context, obj *proto.Object) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.objects[string(obj.Ref().Hash)] = obj
	return nil
}

func (m *memStore) Get(_ context.Context, ref *proto.Ref) (*proto.Object, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	obj, ok := m.objects[string(ref.Hash)]
	if !ok {
		return nil, backup.ErrNotFound
	}

	return obj, nil
}

func (m *memStore) Delete(_ context.Context, ref *proto.Ref) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	delete(m.objects, string(ref.Hash))
	return nil
}

func (m *memStore) Walk(_ context.Context, _ bool, t proto.ObjectType, fn backup.ObjectReceiver) error {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	for _, obj := range m.objects {
		if obj.Type() == t {
			if err := fn(obj); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *memStore) Has(_ context.Context, ref *proto.Ref) (bool, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	_, ok := m.objects[string(ref.Hash)]
	return ok, nil
}

func commitWithFile(t *testing.T, idx *Index, set, name string, timestamp int64, content string) {
	t.Helper()

	ctx := context.Background()
	blob := proto.NewObject(&proto.Blob{Data: []byte(content)})
	require.NoError(t, idx.Put(ctx, blob))

	file := proto.NewObject(&proto.File{Parts: []*proto.FilePart{{Ref: blob.Ref(), Length: uint64(len(content))}}})
	require.NoError(t, idx.Put(ctx, file))

	tree := proto.NewObject(&proto.Tree{Nodes: []*proto.TreeNode{{
		Stat: &proto.FileInfo{Name: name, MtimeNs: timestamp, Size: int64(len(content)), Mode: 0644},
		Ref:  file.Ref(),
	}}})
	require.NoError(t, idx.Put(ctx, tree))

	commit := proto.NewObject(&proto.Commit{Timestamp: timestamp, Tree: tree.Ref(), BackupSet: set})
	require.NoError(t, idx.Put(ctx, commit))
}

func TestSameSecondCommitsAreKept(t *testing.T) {
	store := &memStore{objects: map[string]*proto.Object{}}
	idx := NewIndex(t.TempDir(), "a", store)
	require.NoError(t, idx.Open())
	defer idx.Close()

	now := time.Now().Unix()
	commitWithFile(t, idx, "a", "world.dat", now, "first")
	commitWithFile(t, idx, "a", "world.dat", now, "second")
	commitWithFile(t, idx, "b", "world.dat", now, "other set")

	commits, err := idx.CommitInfo(context.Background(), "a", time.Now().Add(time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, commits, 2)

	files, err := idx.FileInfo(context.Background(), "a", "world.dat", time.Now().Add(time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, files, 2)

	files, err = idx.FileInfo(context.Background(), "b", "world.dat", time.Now().Add(time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, int64(len("other set")), files[0].Stat.Size)
}

func TestReIndexIsIdempotent(t *testing.T) {
	store := &memStore{objects: map[string]*proto.Object{}}
	idx := NewIndex(t.TempDir(), "a", store)
	require.NoError(t, idx.Open())
	defer idx.Close()

	commitWithFile(t, idx, "a", "world.dat", 1, "v1")
	commitWithFile(t, idx, "a", "world.dat", 2, "v2")
	commitWithFile(t, idx, "b", "world.dat", 3, "not ours")

	require.NoError(t, idx.ReIndex(context.Background()))

	commits, err := idx.CommitInfo(context.Background(), "a", time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, commits, 2)
	require.EqualValues(t, 2, commits[0].Timestamp)
}

func TestLatestCommit(t *testing.T) {
	store := &memStore{objects: map[string]*proto.Object{}}
	idx := NewIndex(t.TempDir(), "a", store)
	require.NoError(t, idx.Open())
	defer idx.Close()

	_, err := idx.LatestCommit(context.Background(), "a")
	require.ErrorIs(t, err, backup.ErrNotFound)

	commitWithFile(t, idx, "a", "world.dat", 20, "newer")
	commitWithFile(t, idx, "a", "world.dat", 10, "older")
	commitWithFile(t, idx, "b", "world.dat", 30, "other set")

	ref, err := idx.LatestCommit(context.Background(), "a")
	require.NoError(t, err)

	obj, err := idx.Get(context.Background(), ref)
	require.NoError(t, err)
	require.EqualValues(t, 20, obj.GetCommit().Timestamp)
}

func TestPutRejectsDanglingCommit(t *testing.T) {
	store := &memStore{objects: map[string]*proto.Object{}}
	idx := NewIndex(t.TempDir(), "a", store)
	require.NoError(t, idx.Open())
	defer idx.Close()

	sum := sha256.Sum256([]byte("nowhere"))
	commit := proto.NewObject(&proto.Commit{Timestamp: 1, Tree: &proto.Ref{Hash: sum[:]}, BackupSet: "a"})
	err := idx.Put(context.Background(), commit)
	require.ErrorIs(t, err, backup.ErrDanglingRef)

	_, err = idx.LatestCommit(context.Background(), "a")
	require.ErrorIs(t, err, backup.ErrNotFound)

	// trees and files are checked on Put as well, so a stored tree always
	// has its children
	tree := proto.NewObject(&proto.Tree{Nodes: []*proto.TreeNode{{
		Stat: &proto.FileInfo{Name: "gone.dat", Size: 1, Mode: 0644},
		Ref:  &proto.Ref{Hash: sum[:]},
	}}})
	require.ErrorIs(t, idx.Put(context.Background(), tree), backup.ErrDanglingRef)

	file := proto.NewObject(&proto.File{Parts: []*proto.FilePart{{Ref: &proto.Ref{Hash: sum[:]}, Length: 1}}})
	require.ErrorIs(t, idx.Put(context.Background(), file), backup.ErrDanglingRef)

	ok, err := store.Has(context.Background(), file.Ref())
	require.NoError(t, err)
	require.False(t, ok, "a rejected object must not be stored")
}
