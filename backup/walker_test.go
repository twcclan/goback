package backup

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

// memIndex is an Index over memStore that rejects commits with unreachable
// trees, like the real indexes do on Put.
type memIndex struct {
	*memStore
	mtx     sync.Mutex
	latest  map[string]*proto.Ref
	commits []*proto.Commit
	fetches int64
}

func newMemIndex(store *memStore) *memIndex {
	return &memIndex{memStore: store, latest: map[string]*proto.Ref{}}
}

func (m *memIndex) Open() error  { return nil }
func (m *memIndex) Close() error { return nil }

func (m *memIndex) FileInfo(context.Context, string, string, time.Time, int) ([]*proto.TreeNode, error) {
	return nil, ErrNotImplemented
}

func (m *memIndex) CommitInfo(context.Context, string, time.Time, int) ([]*proto.Commit, error) {
	return nil, ErrNotImplemented
}

func (m *memIndex) ReIndex(context.Context) error { return nil }

func (m *memIndex) LatestCommit(_ context.Context, set string) (*proto.Ref, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	ref, ok := m.latest[set]
	if !ok {
		return nil, ErrNotFound
	}

	return ref, nil
}

func (m *memIndex) Put(ctx context.Context, obj *proto.Object) error {
	if commit := obj.GetCommit(); commit != nil {
		err := m.reachable(ctx, commit.Tree)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrDanglingRef, err)
		}

		m.mtx.Lock()
		m.latest[commit.BackupSet] = obj.Ref()
		m.commits = append(m.commits, commit)
		m.mtx.Unlock()
	}

	return m.memStore.Put(ctx, obj)
}

func (m *memIndex) reachable(ctx context.Context, ref *proto.Ref) error {
	tree, err := LoadTree(ctx, m.memStore, ref)
	if err != nil {
		return err
	}

	for _, node := range tree.Nodes {
		switch {
		case node.Stat.IsDir():
			if err := m.reachable(ctx, node.Ref); err != nil {
				return err
			}
		case node.Ref != nil:
			if ok, _ := m.memStore.Has(ctx, node.Ref); !ok {
				return fmt.Errorf("file %x missing", node.Ref.Hash)
			}
		}
	}

	return nil
}

// GetTree mirrors the server's breadth-first prefetch.
func (m *memIndex) GetTree(ctx context.Context, ref *proto.Ref, maxDepth uint32) ([]*proto.Object, error) {
	atomic.AddInt64(&m.fetches, 1)

	type pending struct {
		ref   *proto.Ref
		depth uint32
	}

	var out []*proto.Object
	queue := []pending{{ref: ref}}

	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]

		obj, err := m.memStore.Get(ctx, next.ref)
		if err != nil {
			return nil, err
		}

		out = append(out, obj)
		tree := obj.GetTree()

		for _, split := range tree.GetSplits() {
			queue = append(queue, pending{ref: split, depth: next.depth})
		}

		if next.depth >= maxDepth {
			continue
		}

		for _, node := range tree.GetNodes() {
			if node.Stat.IsDir() {
				queue = append(queue, pending{ref: node.Ref, depth: next.depth + 1})
			}
		}
	}

	return out, nil
}

// countingStore counts object uploads and exposes the index's prefetch.
type countingStore struct {
	*memStore
	index *memIndex
	puts  int64
}

func (c *countingStore) Put(ctx context.Context, obj *proto.Object) error {
	atomic.AddInt64(&c.puts, 1)
	return c.memStore.Put(ctx, obj)
}

func (c *countingStore) GetTree(ctx context.Context, ref *proto.Ref, maxDepth uint32) ([]*proto.Object, error) {
	return c.index.GetTree(ctx, ref, maxDepth)
}

type walkerFixture struct {
	t       *testing.T
	root    string
	store   *memStore
	objects *countingStore
	index   *memIndex
	walker  *Walker
}

func newWalkerFixture(t *testing.T) *walkerFixture {
	t.Helper()

	store := newMemStore()
	index := newMemIndex(store)
	f := &walkerFixture{
		t:       t,
		root:    t.TempDir(),
		store:   store,
		objects: &countingStore{memStore: store, index: index},
		index:   index,
	}

	f.walker = &Walker{
		Index:         f.index,
		Objects:       f.objects,
		Set:           "test",
		AgentID:       "agent",
		Root:          f.root,
		Workers:       4,
		ReadRetries:   3,
		PrefetchDepth: 2,
	}

	return f
}

func (f *walkerFixture) write(rel string, content []byte) {
	f.t.Helper()

	full := filepath.Join(f.root, filepath.FromSlash(rel))
	require.NoError(f.t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(f.t, os.WriteFile(full, content, 0o644))

	// NTFS publishes a directory's new mtime only once a handle on it is
	// closed; without this a later run sees the directory as changed
	for dir := filepath.Dir(full); len(dir) >= len(f.root); dir = filepath.Dir(dir) {
		handle, err := os.Open(dir)
		require.NoError(f.t, err)
		require.NoError(f.t, handle.Close())
	}
}

func (f *walkerFixture) random(size int) []byte {
	buf := make([]byte, size)
	_, err := rand.Read(buf)
	require.NoError(f.t, err)
	return buf
}

// run performs one walk, leaving enough of a gap that mtimes written before
// it sort strictly before its scan start on coarse clocks.
func (f *walkerFixture) run() *WalkResult {
	f.t.Helper()

	time.Sleep(30 * time.Millisecond)
	atomic.StoreInt64(&f.objects.puts, 0)

	result, err := f.walker.Run(context.Background())
	require.NoError(f.t, err)
	require.NotNil(f.t, result.Ref)

	latest, err := f.index.LatestCommit(context.Background(), "test")
	require.NoError(f.t, err)
	require.True(f.t, latest.Equal(result.Ref))

	return result
}

func (f *walkerFixture) tree(ref *proto.Ref) map[string]*proto.TreeNode {
	f.t.Helper()

	tree, err := LoadTree(context.Background(), f.store, ref)
	require.NoError(f.t, err)

	nodes := make(map[string]*proto.TreeNode, len(tree.Nodes))
	for _, node := range tree.Nodes {
		nodes[node.Stat.Name] = node
	}

	return nodes
}

func TestWalkerUnchangedRunUploadsOnlyTheCommit(t *testing.T) {
	f := newWalkerFixture(t)
	f.write("a.txt", []byte("hello"))
	f.write("sub/b.bin", f.random(150<<10))
	f.write("sub/deep/c.txt", []byte("deep"))
	f.write("other/d.txt", []byte("other"))

	first := f.run()
	require.Nil(t, first.Base)
	require.EqualValues(t, 4, first.Files)
	require.EqualValues(t, 4, first.Read)
	require.EqualValues(t, 0, first.Reused)
	require.False(t, first.Commit.Partial)
	require.Equal(t, "agent", first.Commit.AgentId)
	require.NotZero(t, first.Commit.ScanStartNs)

	root := f.tree(first.Commit.Tree)
	require.Len(t, root, 3)
	require.True(t, root["sub"].Stat.IsDir())
	require.Equal(t, proto.NodeType_NODE_FILE, root["a.txt"].Stat.Type)

	second := f.run()
	require.True(t, second.Base.Equal(first.Ref))
	require.True(t, second.Commit.Parent.Equal(first.Ref))
	require.True(t, second.Commit.Tree.Equal(first.Commit.Tree))
	require.EqualValues(t, 4, second.Reused)
	require.EqualValues(t, 0, second.Read)
	require.EqualValues(t, 0, atomic.LoadInt64(&f.objects.puts))
	require.False(t, second.Dirty())
}

func TestWalkerChangedFileRewritesOnlyItsPath(t *testing.T) {
	f := newWalkerFixture(t)
	big := f.random(200 << 10)
	f.write("sub/b.bin", big)
	f.write("sub/deep/c.txt", []byte("deep"))
	f.write("other/d.txt", []byte("other"))

	first := f.run()
	before := f.tree(first.Commit.Tree)

	f.write("sub/b.bin", append(big, f.random(10<<10)...))

	second := f.run()
	require.EqualValues(t, 1, second.Read)
	require.EqualValues(t, 2, second.Reused)
	require.False(t, second.Commit.Tree.Equal(first.Commit.Tree))

	after := f.tree(second.Commit.Tree)
	require.True(t, after["other"].Ref.Equal(before["other"].Ref), "untouched directory must keep its tree")
	require.False(t, after["sub"].Ref.Equal(before["sub"].Ref))

	sub := f.tree(after["sub"].Ref)
	subBefore := f.tree(before["sub"].Ref)
	require.True(t, sub["deep"].Ref.Equal(subBefore["deep"].Ref))
	require.False(t, sub["b.bin"].Ref.Equal(subBefore["b.bin"].Ref))

	// the unchanged prefix of the file is served from known parts, so fewer
	// blobs than the file has parts get uploaded
	fileObj, err := f.store.Get(context.Background(), sub["b.bin"].Ref)
	require.NoError(t, err)
	parts := len(fileObj.GetFile().Parts)
	require.Greater(t, parts, 1)
	require.Less(t, int(atomic.LoadInt64(&f.objects.puts)), parts+3, "puts: blobs for new parts, file, sub tree, root tree")
}

func TestWalkerRacyRuleRereadsFilesTouchedDuringTheBaseRun(t *testing.T) {
	f := newWalkerFixture(t)
	f.write("racy.txt", []byte("version-1"))

	future := time.Now().Add(time.Hour)
	full := filepath.Join(f.root, "racy.txt")
	require.NoError(t, os.Chtimes(full, future, future))

	first := f.run()
	before := f.tree(first.Commit.Tree)

	// same size, same mtime, different content: only the racy rule catches it
	f.write("racy.txt", []byte("version-2"))
	require.NoError(t, os.Chtimes(full, future, future))

	second := f.run()
	require.EqualValues(t, 1, second.Read)
	require.EqualValues(t, 0, second.Reused)

	after := f.tree(second.Commit.Tree)
	require.False(t, after["racy.txt"].Ref.Equal(before["racy.txt"].Ref))
}

func TestWalkerDeletedAndAddedEntries(t *testing.T) {
	f := newWalkerFixture(t)
	f.write("keep.txt", []byte("keep"))
	f.write("gone.txt", []byte("gone"))

	first := f.run()

	require.NoError(t, os.Remove(filepath.Join(f.root, "gone.txt")))
	f.write("new.txt", []byte("new"))

	second := f.run()
	require.EqualValues(t, 1, second.Read)
	require.EqualValues(t, 1, second.Reused)

	after := f.tree(second.Commit.Tree)
	require.Len(t, after, 2)
	require.NotContains(t, after, "gone.txt")
	require.Contains(t, after, "new.txt")
	require.True(t, after["keep.txt"].Ref.Equal(f.tree(first.Commit.Tree)["keep.txt"].Ref))
}

func TestWalkerIncludeFilter(t *testing.T) {
	f := newWalkerFixture(t)
	f.write("logs/x.log", []byte("log"))
	f.write("world/level.dat", []byte("level"))

	f.walker.Include = func(rel string) bool { return rel != "logs" }

	result := f.run()
	root := f.tree(result.Commit.Tree)
	require.Len(t, root, 1)
	require.Contains(t, root, "world")
}

func TestWalkerSymlinkIsRecordedNotFollowed(t *testing.T) {
	f := newWalkerFixture(t)
	f.write("target.txt", []byte("target"))

	err := os.Symlink("target.txt", filepath.Join(f.root, "link"))
	if err != nil && runtime.GOOS == "windows" {
		t.Skipf("symlinks unavailable: %v", err)
	}
	require.NoError(t, err)

	first := f.run()
	require.EqualValues(t, 1, first.Files)

	root := f.tree(first.Commit.Tree)
	link := root["link"]
	require.Equal(t, proto.NodeType_NODE_SYMLINK, link.Stat.Type)
	require.Equal(t, "target.txt", link.Stat.LinkTarget)
	require.Nil(t, link.Ref)

	second := f.run()
	require.True(t, second.Commit.Tree.Equal(first.Commit.Tree))
	require.EqualValues(t, 0, atomic.LoadInt64(&f.objects.puts))
}

func TestWalkerSplitTreesRoundTripAndPrefetch(t *testing.T) {
	f := newWalkerFixture(t)

	const count = 700
	for i := 0; i < count; i++ {
		f.write(fmt.Sprintf("many/file-%04d.txt", i), []byte(fmt.Sprintf("content %d", i)))
	}

	first := f.run()
	require.EqualValues(t, count, first.Read)

	root := f.tree(first.Commit.Tree)
	manyObj, err := f.store.Get(context.Background(), root["many"].Ref)
	require.NoError(t, err)
	require.NotEmpty(t, manyObj.GetTree().Splits, "a directory this large is split")
	require.Empty(t, manyObj.GetTree().Nodes)

	many := f.tree(root["many"].Ref)
	require.Len(t, many, count)

	f.write("many/file-0350.txt", []byte("changed"))

	second := f.run()
	require.EqualValues(t, 1, second.Read)
	require.EqualValues(t, count-1, second.Reused)
	require.Greater(t, atomic.LoadInt64(&f.index.fetches), int64(0), "base trees come through GetTree")

	after := f.tree(f.tree(second.Commit.Tree)["many"].Ref)
	require.Len(t, after, count)
	require.False(t, after["file-0350.txt"].Ref.Equal(many["file-0350.txt"].Ref))
	require.True(t, after["file-0000.txt"].Ref.Equal(many["file-0000.txt"].Ref))
}

func TestWalkerCheckpointsWritePartialCommits(t *testing.T) {
	f := newWalkerFixture(t)
	for i := 0; i < 5; i++ {
		f.write(fmt.Sprintf("dir-%d/file.txt", i), []byte(fmt.Sprint(i)))
	}

	f.walker.CheckpointInterval = time.Nanosecond

	result := f.run()
	require.Greater(t, result.Checkpoints, int64(0))
	require.False(t, result.Commit.Partial)

	partial := 0
	for _, commit := range f.index.commits {
		if commit.Partial {
			partial++
			require.EqualValues(t, result.Commit.ScanStartNs, commit.ScanStartNs)
		}
	}
	require.EqualValues(t, result.Checkpoints, partial)

	// the final commit is a valid diff base; directory mtimes may still
	// settle after the first run, so compare the subtrees rather than the root
	f.walker.CheckpointInterval = 0
	second := f.run()
	require.EqualValues(t, 5, second.Reused)
	require.EqualValues(t, 0, second.Read)

	before, after := f.tree(result.Commit.Tree), f.tree(second.Commit.Tree)
	for name, node := range before {
		require.True(t, after[name].Ref.Equal(node.Ref), name)
	}
}

func TestWalkerUnreadableFileKeepsPreviousVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on windows")
	}

	f := newWalkerFixture(t)
	f.write("secret.txt", []byte("v1"))

	first := f.run()
	before := f.tree(first.Commit.Tree)

	full := filepath.Join(f.root, "secret.txt")
	f.write("secret.txt", []byte("v2-longer"))
	require.NoError(t, os.Chmod(full, 0))
	t.Cleanup(func() { _ = os.Chmod(full, 0o644) })

	second := f.run()
	require.EqualValues(t, 1, second.Unreadable)
	require.True(t, second.Dirty())
	require.True(t, f.tree(second.Commit.Tree)["secret.txt"].Ref.Equal(before["secret.txt"].Ref))
}

func TestWalkerRejectsCommitWithDanglingTree(t *testing.T) {
	f := newWalkerFixture(t)
	f.write("a.txt", []byte("a"))

	first := f.run()
	root := f.tree(first.Commit.Tree)
	require.NoError(t, f.store.Delete(context.Background(), root["a.txt"].Ref))

	commit := proto.NewObject(&proto.Commit{Timestamp: time.Now().Unix(), Tree: first.Commit.Tree, BackupSet: "test"})
	err := f.index.Put(context.Background(), commit)
	require.True(t, errors.Is(err, ErrDanglingRef), "got %v", err)
}

func TestHashFileMatchesStoredRef(t *testing.T) {
	f := newWalkerFixture(t)
	f.write("big.bin", f.random(300<<10))

	result := f.run()
	root := f.tree(result.Commit.Tree)

	ref, err := HashFile(filepath.Join(f.root, "big.bin"))
	require.NoError(t, err)
	require.True(t, ref.Equal(root["big.bin"].Ref))
}
