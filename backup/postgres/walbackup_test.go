package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gobackio/goback/backup"
	"github.com/gobackio/goback/index/sql"
	"github.com/gobackio/goback/proto"
	"github.com/gobackio/goback/storage"
	"github.com/gobackio/goback/storage/pack"

	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/memblob"
)

type walFixture struct {
	t     *testing.T
	ctx   context.Context
	index *sql.Index
	spool Spool
	now   time.Time
}

func newWALFixture(t *testing.T) *walFixture {
	t.Helper()

	bucket := memblob.OpenBucket(nil)
	store, err := pack.NewPackStorage(pack.WithArchiveStorage(storage.NewBucketStore(bucket)), pack.WithArchiveIndex(pack.NewInMemoryIndex()))
	require.NoError(t, err)
	require.NoError(t, store.Open())
	t.Cleanup(func() { _ = store.Close() })

	index := sql.NewMemory(t.Name(), store)
	require.NoError(t, index.Open())
	t.Cleanup(func() { _ = index.Close() })

	return &walFixture{t: t, ctx: context.Background(), index: index, spool: Spool{Dir: t.TempDir()}, now: time.Now()}
}

// archive spools the segment of the cluster systemID at segment n of
// timeline 1.
func (f *walFixture) archive(n uint64, systemID uint64) string {
	f.t.Helper()

	name := walName(1, n)
	path := filepath.Join(f.t.TempDir(), name)
	require.NoError(f.t, os.WriteFile(path, append(header(1, n*segSize, systemID), byte(n)), 0o600))
	require.NoError(f.t, f.spool.Add(path, name))

	return name
}

func walName(timeline uint32, n uint64) string {
	perLog := uint64(0x100000000) / segSize
	return fmt.Sprintf("%08X%08X%08X", timeline, n/perLog, n%perLog)
}

// base records a base backup that started in segment n.
func (f *walFixture) base(n uint64) {
	f.t.Helper()

	dir := f.t.TempDir()
	require.NoError(f.t, os.WriteFile(filepath.Join(dir, "base.tar"), []byte("base"), 0o600))

	w := &backup.Walker{Index: f.index, Objects: f.index, Set: "db-base", AgentID: "a", Root: dir, Workers: 1,
		Metadata: map[string]string{MetaStartWALFile: walName(1, n)}}
	_, err := w.Run(f.ctx)
	require.NoError(f.t, err)
}

func (f *walFixture) run() (*backup.WalkResult, error) {
	b := &WALBackup{
		Walker:  &backup.Walker{Index: f.index, Objects: f.index, Set: "db-wal", AgentID: "a", Root: f.spool.Dir, Workers: 2},
		Spool:   f.spool,
		BaseSet: "db-base",
		Window:  14 * 24 * time.Hour,
		Now:     func() time.Time { return f.now },
	}

	return b.Run(f.ctx)
}

func (f *walFixture) held(result *backup.WalkResult) []string {
	f.t.Helper()

	tree, err := backup.LoadTree(f.ctx, f.index, result.Commit.Tree)
	require.NoError(f.t, err)

	var names []string
	for _, node := range tree.Nodes {
		names = append(names, string(node.Stat.Name))
	}

	return names
}

func (f *walFixture) spooled() []string {
	f.t.Helper()

	files, err := f.spool.Files()
	require.NoError(f.t, err)

	return files
}

func TestEachWALCommitHoldsEverythingBackToTheOldestBase(t *testing.T) {
	f := newWALFixture(t)

	result, err := f.run()
	require.NoError(t, err)
	require.Nil(t, result, "an empty spool commits nothing")

	one, two := f.archive(1, 42), f.archive(2, 42)
	result, err = f.run()
	require.NoError(t, err)
	require.Equal(t, []string{one, two}, f.held(result))
	require.Equal(t, "42", result.Commit.Metadata[MetaSystemID])
	require.Empty(t, f.spooled())

	f.base(2)
	three := f.archive(3, 42)

	result, err = f.run()
	require.NoError(t, err)
	require.Equal(t, []string{two, three}, f.held(result), "what precedes the oldest base is let go")
	require.Equal(t, two, result.Commit.Metadata[MetaFirstWALFile])
	require.Equal(t, three, result.Commit.Metadata[MetaLastWALFile])
}

func TestWALFromAnotherClusterOrWithAGapStaysInTheSpool(t *testing.T) {
	f := newWALFixture(t)

	f.archive(1, 42)
	_, err := f.run()
	require.NoError(t, err)

	foreign := f.archive(2, 7)
	_, err = f.run()
	require.ErrorIs(t, err, ErrForeignSegment)
	require.Equal(t, []string{foreign}, f.spooled())
	require.NoError(t, f.spool.Remove(foreign))

	skipped := f.archive(3, 42)
	_, err = f.run()
	require.ErrorIs(t, err, ErrGap)
	require.Equal(t, []string{skipped}, f.spooled())
}

// treeCounting counts the trees read one at a time, and streams a tree
// with its splits in one call as a remote store does.
type treeCounting struct {
	*sql.Index
	reads atomic.Int64
}

func (c *treeCounting) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	obj, err := c.Index.Get(ctx, ref)
	if obj.Type() == proto.ObjectType_TREE {
		c.reads.Add(1)
	}

	return obj, err
}

func (c *treeCounting) GetTree(ctx context.Context, ref *proto.Ref, _ uint32) ([]*proto.Object, error) {
	var objects []*proto.Object

	for queue := []*proto.Ref{ref}; len(queue) > 0; queue = queue[1:] {
		obj, err := c.Index.Get(ctx, queue[0])
		if err != nil {
			return nil, err
		}

		objects = append(objects, obj)
		queue = append(queue, obj.GetTree().GetSplits()...)
	}

	return objects, nil
}

func TestAWALCommitReadsThePreviousTreeInOneCall(t *testing.T) {
	f := newWALFixture(t)

	for n := uint64(1); n <= 600; n++ {
		f.archive(n, 42)
	}

	_, err := f.run()
	require.NoError(t, err)

	f.archive(601, 42)

	objects := &treeCounting{Index: f.index}
	b := &WALBackup{
		Walker:  &backup.Walker{Index: f.index, Objects: objects, Set: "db-wal", AgentID: "a", Root: f.spool.Dir, Workers: 2},
		Spool:   f.spool,
		BaseSet: "db-base",
		Window:  14 * 24 * time.Hour,
		Now:     func() time.Time { return f.now },
	}

	result, err := b.Run(f.ctx)
	require.NoError(t, err)
	require.Len(t, f.held(result), 601)
	require.Zero(t, objects.reads.Load(), "the previous tree's splits were read one by one")
}
