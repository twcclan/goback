package pack

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/bits-and-blooms/bitset"
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
		var offset uint64
		for _, obj := range batch {
			length := uint64(len(obj.GetBlob().GetData())) + 1
			parts = append(parts, &proto.FilePart{
				Ref:    obj.Ref(),
				Offset: offset,
				Length: length,
			})
			offset += length
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
		trees = append(trees, treeOf(batch))
	}

	return trees
}

// treeOf builds a tree whose nodes point at the objects under generated names.
func treeOf(children []*proto.Object) *proto.Object {
	var nodes []*proto.TreeNode
	for i, obj := range children {
		stat := &proto.FileInfo{Name: []byte(fmt.Sprintf("node-%03d", i)), Mode: 0644, Size: 1}
		if obj.Type() == proto.ObjectType_TREE {
			stat = &proto.FileInfo{Name: []byte(fmt.Sprintf("node-%03d", i)), Mode: 0755, Type: proto.NodeType_NODE_DIRECTORY}
		}

		nodes = append(nodes, &proto.TreeNode{Stat: stat, Ref: obj.Ref()})
	}

	return proto.NewObject(&proto.Tree{Nodes: nodes})
}

func makeGCCommits(trees []*proto.Object) []*proto.Object {
	var commits []*proto.Object
	for i, tree := range trees {
		commits = append(commits, proto.NewObject(&proto.Commit{
			Tree:      tree.Ref(),
			Timestamp: int64(i + 1),
			BackupSet: "world",
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
	unreachableFiles := makeGCFiles(unreachableBlobs)

	// TODO: generate file splits

	// generate some trees
	t.Log("generating reachable trees")
	reachableTrees := makeGCTrees(reachableFiles)
	unreachableTrees := makeGCTrees(unreachableFiles)

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

func newGCStore(t *testing.T, base string) *PackStorage {
	t.Helper()

	store, err := NewPackStorage(
		WithArchiveStorage(newLocal(base)),
		WithArchiveIndex(NewInMemoryIndex()),
		WithMaxSize(256*1024),
	)
	require.NoError(t, err)
	require.NoError(t, store.Open())

	return store
}

func putAll(t *testing.T, store *PackStorage, objects []*proto.Object) {
	t.Helper()

	ctx := context.Background()
	for _, i := range rand.Perm(len(objects)) {
		require.NoError(t, store.Put(ctx, objects[i]))
	}

	require.NoError(t, store.Flush())
}

func requireStored(t *testing.T, store *PackStorage, objects []*proto.Object, stored bool) {
	t.Helper()

	ctx := context.Background()
	for _, obj := range objects {
		_, err := store.Get(ctx, obj.Ref())
		if stored {
			require.NoErrorf(t, err, "object %x of type %s", obj.Ref().Hash, obj.Type())
		} else {
			require.ErrorIsf(t, err, backup.ErrNotFound, "object %x of type %s", obj.Ref().Hash, obj.Type())
		}
	}
}

func requirePresent(t *testing.T, store *PackStorage, objects []*proto.Object, present bool) {
	t.Helper()

	for _, obj := range objects {
		has, err := store.Has(context.Background(), obj.Ref())
		require.NoError(t, err)
		require.Equalf(t, present, has, "presence of %x of type %s", obj.Ref().Hash, obj.Type())
	}
}

func gcOptions(t *testing.T, ahead time.Duration) CollectOptions {
	return CollectOptions{
		Now:       time.Now().Add(ahead),
		DeadRatio: 1e-9,
		TempDir:   t.TempDir(),
		Readers:   4,
	}
}

func TestCollectMarksThenSweeps(t *testing.T) {
	base := t.TempDir()
	store := newGCStore(t, base)
	ctx := context.Background()

	reachable, unreachable := makeGCTestData(t)
	putAll(t, store, append(append([]*proto.Object{}, reachable...), unreachable...))

	first, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)
	require.EqualValues(t, 1, first.Generation)
	require.Equal(t, "first generation", first.SweepSkipped)
	require.EqualValues(t, len(unreachable), first.DeadObjects)
	require.EqualValues(t, len(reachable), first.Marked)
	require.Greater(t, first.Roots, 0)

	results, err := filepath.Glob(filepath.Join(base, "*"+GCExt))
	require.NoError(t, err)
	require.Len(t, results, first.Archives)

	requireStored(t, store, unreachable, true)
	requirePresent(t, store, unreachable, false)
	requirePresent(t, store, reachable, true)

	second, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 2, second.Generation)
	require.Empty(t, second.SweepSkipped)
	require.Greater(t, second.Swept, 0)
	require.EqualValues(t, len(unreachable), second.ReclaimedObjects)

	requireStored(t, store, unreachable, false)
	requireStored(t, store, reachable, true)
	requirePresent(t, store, reachable, true)

	third, err := store.Collect(ctx, gcOptions(t, 72*time.Hour))
	require.NoError(t, err)
	require.Zero(t, third.DeadObjects)
	require.Zero(t, third.Swept)

	require.NoError(t, store.Close())
}

func TestCollectResumesInterruptedMark(t *testing.T) {
	base := t.TempDir()
	store := newGCStore(t, base)
	ctx := context.Background()

	reachable, unreachable := makeGCTestData(t)
	putAll(t, store, append(append([]*proto.Object{}, reachable...), unreachable...))

	opts := gcOptions(t, 0)

	defer func(n int) { rootBatch = n; gcAfterBatch = nil }(rootBatch)
	rootBatch = 2

	interrupted := errors.New("interrupted")
	gcAfterBatch = func(batch int) error {
		if batch == 1 {
			return interrupted
		}
		return nil
	}

	_, err := store.Collect(ctx, opts)
	require.ErrorIs(t, err, interrupted)

	runDir := filepath.Join(opts.TempDir, "goback-gc", "gen-1")
	done, err := filepath.Glob(filepath.Join(runDir, "batch-*.done"))
	require.NoError(t, err)
	require.Len(t, done, 2, "the completed batches are checkpointed")

	gcAfterBatch = nil
	report, err := store.Collect(ctx, opts)
	require.NoError(t, err)
	require.Equal(t, 2, report.Resumed)
	require.EqualValues(t, len(unreachable), report.DeadObjects)
	require.EqualValues(t, len(reachable), report.Marked)

	_, err = os.Stat(runDir)
	require.True(t, os.IsNotExist(err), "a finished run leaves no checkpoints")

	requirePresent(t, store, unreachable, false)
	requirePresent(t, store, reachable, true)

	require.NoError(t, store.Close())
}

func TestCollectRestartsAnInterruptedMarkWhenTheRootsChange(t *testing.T) {
	base := t.TempDir()
	store := newGCStore(t, base)
	ctx := context.Background()

	reachable, unreachable := makeGCTestData(t)
	putAll(t, store, append(append([]*proto.Object{}, reachable...), unreachable...))

	opts := gcOptions(t, 0)

	defer func(n int) { rootBatch = n; gcAfterBatch = nil }(rootBatch)
	rootBatch = 2

	interrupted := errors.New("interrupted")
	gcAfterBatch = func(batch int) error {
		if batch == 1 {
			return interrupted
		}
		return nil
	}

	_, err := store.Collect(ctx, opts)
	require.ErrorIs(t, err, interrupted)

	// a restore lease adds a root, which shifts every batch after it
	var target *proto.Ref
	for _, obj := range unreachable {
		if obj.Type() == proto.ObjectType_TREE {
			target = obj.Ref()
			break
		}
	}
	require.NotNil(t, target)

	sctx, err := store.BeginSession(ctx, &backup.Session{Set: "world", Restore: target})
	require.NoError(t, err)

	gcAfterBatch = nil
	report, err := store.Collect(ctx, opts)
	require.NoError(t, err)
	require.Zero(t, report.Resumed, "the checkpoints of the old root list are not reused")
	require.Greater(t, int(report.Marked), len(reachable), "the leased tree and its files stay live")
	requirePresent(t, store, []*proto.Object{unreachable[slices.IndexFunc(unreachable, func(o *proto.Object) bool { return o.Ref().Equal(target) })]}, true)

	requirePresent(t, store, reachable, true)
	require.NoError(t, store.EndSession(sctx))
	require.NoError(t, store.Close())
}

func TestCollectKeepsYoungObjects(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	ctx := context.Background()

	reachable, unreachable := makeGCTestData(t)
	putAll(t, store, append(append([]*proto.Object{}, reachable...), unreachable...))

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)

	second, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)
	require.Empty(t, second.SweepSkipped)
	require.Zero(t, second.ReclaimedObjects)

	requireStored(t, store, unreachable, true)
	require.NoError(t, store.Close())
}

func TestCollectWaitsForOlderSessions(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	ctx := context.Background()

	reachable, unreachable := makeGCTestData(t)
	putAll(t, store, append(append([]*proto.Object{}, reachable...), unreachable...))

	sctx, _ := beginSession(t, store, "a1")

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)

	second, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.Contains(t, second.SweepSkipped, "session")
	requireStored(t, store, unreachable, true)

	require.NoError(t, store.EndSession(sctx))

	third, err := store.Collect(ctx, gcOptions(t, 72*time.Hour))
	require.NoError(t, err)
	require.Empty(t, third.SweepSkipped)
	require.EqualValues(t, len(unreachable), third.ReclaimedObjects)
	requireStored(t, store, unreachable, false)
	requireStored(t, store, reachable, true)

	require.NoError(t, store.Close())
}

// makeChain builds files, trees and one commit over the blobs.
func makeChain(blobs []*proto.Object) []*proto.Object {
	files := makeGCFiles(blobs)
	trees := makeGCTrees(files)

	root := treeOf(trees)
	commit := proto.NewObject(&proto.Commit{Tree: root.Ref(), Timestamp: 1, BackupSet: "world"})

	var all []*proto.Object
	all = append(all, blobs...)
	all = append(all, files...)
	all = append(all, trees...)
	all = append(all, root, commit)

	return all
}

func countTombstones(t *testing.T, store *PackStorage) int {
	t.Helper()

	n := 0
	err := store.WalkHeaders(context.Background(), proto.ObjectType_TOMBSTONE, func(*proto.ObjectHeader) error {
		n++
		return nil
	})
	require.NoError(t, err)

	return n
}

func TestCollectReclaimsTombstonedCommit(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	ctx := context.Background()

	blobs := makeTestData(t, 40)
	gone := makeChain(blobs[:20])
	kept := makeChain(blobs[20:])
	putAll(t, store, append(append([]*proto.Object{}, gone...), kept...))

	require.NoError(t, store.Delete(ctx, gone[len(gone)-1].Ref()))
	require.NoError(t, store.Flush())
	require.Equal(t, 1, countTombstones(t, store))

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)
	requirePresent(t, store, gone, false)
	requirePresent(t, store, kept, true)

	second, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, len(gone), second.ReclaimedObjects)
	requireStored(t, store, gone, false)
	requireStored(t, store, kept, true)
	require.Equal(t, 1, countTombstones(t, store), "the tombstone outlives its target by two generations")

	_, err = store.Collect(ctx, gcOptions(t, 72*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, countTombstones(t, store))

	fourth, err := store.Collect(ctx, gcOptions(t, 96*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, fourth.ReclaimedObjects)
	require.Equal(t, 0, countTombstones(t, store))
	requireStored(t, store, kept, true)

	require.NoError(t, store.Close())
}

func TestCollectSweepsErasedArchivesEarly(t *testing.T) {
	for _, erase := range []bool{false, true} {
		t.Run(fmt.Sprintf("erase=%v", erase), func(t *testing.T) {
			store := newGCStore(t, t.TempDir())
			ctx := context.Background()

			blobs := makeTestData(t, 40)
			gone := makeChain(blobs[:20])
			kept := makeChain(blobs[20:])
			putAll(t, store, append(append([]*proto.Object{}, gone...), kept...))

			target := gone[len(gone)-1].Ref()
			if erase {
				require.NoError(t, store.Erase(ctx, target))
			} else {
				require.NoError(t, store.Delete(ctx, target))
			}
			require.NoError(t, store.Flush())

			// neither the dead ratio nor the erasure bound ever selects
			patient := func(ahead time.Duration) CollectOptions {
				opts := gcOptions(t, ahead)
				opts.DeadRatio = 2
				opts.ErasureBound = 365 * 24 * time.Hour
				return opts
			}

			first, err := store.Collect(ctx, patient(0))
			require.NoError(t, err)
			if erase {
				require.Greater(t, first.ErasedArchives, 0)
			} else {
				require.Zero(t, first.ErasedArchives)
			}

			second, err := store.Collect(ctx, patient(48*time.Hour))
			require.NoError(t, err)
			require.Empty(t, second.SweepSkipped)

			if erase {
				require.EqualValues(t, len(gone), second.ReclaimedObjects, "the erasure flag selects the archives")
				requireStored(t, store, gone, false)
			} else {
				require.Zero(t, second.Swept, "a plain tombstone waits for the ratio or the bound")
				requireStored(t, store, gone, true)
			}
			requireStored(t, store, kept, true)

			require.NoError(t, store.Close())
		})
	}
}

// TestCollectCarriesTheErasureClockThroughARewrite compacts the archives
// holding a tombstoned commit after its first unmarked generation and
// expects the erasure bound to count from that generation, not from the
// rewrite.
func TestCollectCarriesTheErasureClockThroughARewrite(t *testing.T) {
	store, err := NewPackStorage(
		WithArchiveStorage(newLocal(t.TempDir())),
		WithArchiveIndex(NewInMemoryIndex()),
		WithMaxSize(1024*1024),
		WithCompaction(CompactionConfig{MinimumCandidates: 0}),
	)
	require.NoError(t, err)
	require.NoError(t, store.Open())
	ctx := context.Background()

	blobs := makeTestData(t, 40)
	gone := makeChain(blobs[:20])
	kept := makeChain(blobs[20:])
	putAll(t, store, append(append([]*proto.Object{}, gone...), kept...))

	require.NoError(t, store.Delete(ctx, gone[len(gone)-1].Ref()))
	require.NoError(t, store.Flush())

	// only the erasure bound may select, ten days after the first unmarked generation
	patient := func(ahead time.Duration) CollectOptions {
		opts := gcOptions(t, ahead)
		opts.DeadRatio = 2
		opts.ErasureBound = 10 * 24 * time.Hour
		return opts
	}

	_, err = store.Collect(ctx, patient(0))
	require.NoError(t, err)

	before, err := store.storage.List(ArchiveSuffix)
	require.NoError(t, err)
	require.NoError(t, store.doCompaction())
	after, err := store.storage.List(ArchiveSuffix)
	require.NoError(t, err)
	require.NotEqual(t, before, after, "the rewrite replaced the archives")

	_, err = store.Collect(ctx, patient(48*time.Hour))
	require.NoError(t, err)
	requireStored(t, store, gone, true)

	report, err := store.Collect(ctx, patient(11*24*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, len(gone), report.ReclaimedObjects, "the bound counts from the generation before the rewrite")
	requireStored(t, store, gone, false)
	requireStored(t, store, kept, true)

	require.NoError(t, store.Close())
}

func TestCollectKeepsRestoreSessionTargets(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	ctx := context.Background()

	blobs := makeTestData(t, 40)
	gone := makeChain(blobs[:20])
	kept := makeChain(blobs[20:])
	putAll(t, store, append(append([]*proto.Object{}, gone...), kept...))

	target := gone[len(gone)-1].Ref()
	require.NoError(t, store.Delete(ctx, target))
	require.NoError(t, store.Flush())

	sctx, err := store.BeginSession(ctx, &backup.Session{Set: "world", Restore: target})
	require.NoError(t, err)

	leases, err := store.RestoreLeases(ctx)
	require.NoError(t, err)
	require.Len(t, leases, 1)

	_, err = store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)
	requirePresent(t, store, gone, true)

	require.NoError(t, store.EndSession(sctx))

	leases, err = store.RestoreLeases(ctx)
	require.NoError(t, err)
	require.Empty(t, leases)

	_, err = store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	requirePresent(t, store, gone, false)
	requirePresent(t, store, kept, true)

	require.NoError(t, store.Close())
}

func TestCollectResultsSurviveReopen(t *testing.T) {
	base := t.TempDir()
	store := newGCStore(t, base)
	ctx := context.Background()

	reachable, unreachable := makeGCTestData(t)
	putAll(t, store, append(append([]*proto.Object{}, reachable...), unreachable...))

	_, err := store.Collect(ctx, gcOptions(t, 0))
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store = newGCStore(t, base)
	requirePresent(t, store, unreachable, false)
	requirePresent(t, store, reachable, true)

	second, err := store.Collect(ctx, gcOptions(t, 48*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 2, second.Generation)
	require.EqualValues(t, len(unreachable), second.ReclaimedObjects)
	requireStored(t, store, reachable, true)

	require.NoError(t, store.Close())
}

func TestCollectRefusesSessionContext(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	sctx, _ := beginSession(t, store, "a1")

	_, err := store.Collect(sctx, CollectOptions{})
	require.Error(t, err)
	require.NoError(t, store.Close())
}

func TestGCFileRoundTrip(t *testing.T) {
	storage := newLocal(t.TempDir())

	current := bitset.New(10)
	current.Set(1)
	current.Set(9)
	previous := bitset.New(10)
	previous.Set(3)

	in := &gcFile{
		Generation:  7,
		Snapshot:    time.Unix(1700000000, 0).UTC(),
		Current:     current,
		Previous:    previous,
		DeadObjects: 8,
		DeadBytes:   4096,
		DeadSince:   time.Unix(1600000000, 0).UTC(),
		Dead:        []uint64{1, 2, 1 << 60},
		Erase:       true,
	}
	require.NoError(t, writeGCFile(storage, "root/a", in))

	out, err := readGCFile(storage, "root/a")
	require.NoError(t, err)
	require.Equal(t, in, out)

	require.True(t, out.dead(0))
	require.False(t, out.dead(1))
	require.False(t, out.dead(3))
	require.True(t, out.candidate([]byte{0, 0, 0, 0, 0, 0, 0, 2, 0xff}))
	require.False(t, out.candidate([]byte{0, 0, 0, 0, 0, 0, 0, 3, 0xff}))

	missing, err := readGCFile(storage, "root/none")
	require.NoError(t, err)
	require.Nil(t, missing)

	in.Previous = nil
	require.NoError(t, writeGCFile(storage, "root/b", in))
	out, err = readGCFile(storage, "root/b")
	require.NoError(t, err)
	require.Nil(t, out.Previous)
	require.False(t, out.dead(0))
}

func TestLiveRunsMergeSortedAndDistinct(t *testing.T) {
	runs := newLiveRuns(t.TempDir(), 100)
	defer runs.close()

	var keys []refKey
	for i := 0; i < 1000; i++ {
		var k refKey
		binary.BigEndian.PutUint64(k[:], uint64(rand.Intn(300)))
		keys = append(keys, k)
	}

	for start := 0; start < len(keys); start += 37 {
		require.NoError(t, runs.add(keys[start:min(start+37, len(keys))]))
	}
	require.NotEmpty(t, runs.files)

	distinct := make(map[refKey]bool)
	for _, k := range keys {
		distinct[k] = true
	}

	it, err := runs.iterator()
	require.NoError(t, err)
	defer it.close()

	var got []refKey
	for {
		k, ok := it.next()
		if !ok {
			break
		}
		got = append(got, k)
	}

	require.Len(t, got, len(distinct))
	require.True(t, sort.SliceIsSorted(got, func(i, j int) bool { return bytes.Compare(got[i][:], got[j][:]) < 0 }))
}
