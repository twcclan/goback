package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twcclan/goback/proto"

	"go4.org/syncutil"
	"golang.org/x/sync/errgroup"
)

// StatCache remembers, per path, the stat data seen when a file was last
// read and the File ref it produced. It only ever adds a reason to re-read;
// a hit alone never justifies reuse.
type StatCache interface {
	// Lookup reports whether an entry exists for path and whether it still
	// matches the file's identity.
	Lookup(path string, info os.FileInfo) (present bool, matches bool)
	Store(path string, info os.FileInfo, ref *proto.Ref) error
}

// TreeFetcher is implemented by stores that can stream a subtree in one
// round trip.
type TreeFetcher interface {
	GetTree(ctx context.Context, ref *proto.Ref, maxDepth uint32) ([]*proto.Object, error)
}

// Walker builds a commit by diffing a directory against the set's latest
// commit, reading and uploading only what changed.
type Walker struct {
	Index   Index
	Objects ObjectStore
	Set     string
	AgentID string
	Root    string

	// Include decides per slash-separated relative path; nil includes all.
	Include func(rel string) bool

	Workers            int
	ForceHashPercent   int
	CheckpointInterval time.Duration
	ReadRetries        int
	Cache              StatCache
	PrefetchDepth      uint32

	gate      *syncutil.Gate
	trees     *treeSource
	scanStart int64
	base      *proto.Ref
	baseScan  int64
	started   time.Time
	last      time.Time
	rand      *rand.Rand
	result    WalkResult
}

// WalkResult summarises one run.
type WalkResult struct {
	Ref         *proto.Ref
	Commit      *proto.Commit
	Base        *proto.Ref
	Files       int64
	Reused      int64
	Read        int64
	Torn        int64
	Unreadable  int64
	Skipped     int64
	Checkpoints int64
}

// Dirty reports whether any file was recorded from an inconsistent or failed
// read, which a caller should surface as a non-zero exit.
func (r *WalkResult) Dirty() bool {
	return r.Torn > 0 || r.Unreadable > 0
}

var errUnreadable = errors.New("file could not be read")

func (w *Walker) logf(format string, args ...interface{}) {
	log.Printf(format, args...)
}

// Run walks the root, writes the commit and returns its ref.
func (w *Walker) Run(ctx context.Context) (*WalkResult, error) {
	if w.Workers <= 0 {
		w.Workers = 1
	}

	w.gate = syncutil.NewGate(w.Workers)
	w.rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	w.started = time.Now()
	w.last = w.started
	w.scanStart = w.started.UnixNano()
	w.result = WalkResult{}

	fetcher, _ := w.Objects.(TreeFetcher)
	w.trees = &treeSource{getter: w.Objects, fetcher: fetcher, depth: w.PrefetchDepth, objects: map[string]*proto.Object{}}

	baseNodes, err := w.loadBase(ctx)
	if err != nil {
		return nil, err
	}

	nodes, changed, err := w.walkDir(ctx, w.Root, "", baseNodes, true)
	if err != nil {
		return nil, err
	}

	var root *proto.Ref
	if !changed && w.base != nil {
		root = w.result.Commit.Tree
	} else {
		root, err = PutTree(ctx, w.Objects, nodes)
		if err != nil {
			return nil, fmt.Errorf("storing root tree: %w", err)
		}
	}

	ref, commit, err := w.putCommit(ctx, root, false)
	if err != nil {
		return nil, err
	}

	w.result.Ref = ref
	w.result.Commit = commit
	w.result.Base = w.base

	result := w.result

	return &result, nil
}

// loadBase resolves the diff base from the server, never from local state.
func (w *Walker) loadBase(ctx context.Context) ([]*proto.TreeNode, error) {
	ref, err := w.Index.LatestCommit(ctx, w.Set)
	if errors.Is(err, ErrNotFound) {
		w.logf("No previous commit for set %q, reading everything", w.Set)
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("finding the latest commit: %w", err)
	}

	obj, err := w.Objects.Get(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("loading base commit %x: %w", ref.Hash, err)
	}

	commit := obj.GetCommit()
	if commit == nil {
		return nil, fmt.Errorf("base %x is not a commit", ref.Hash)
	}

	tree, err := w.trees.load(ctx, commit.Tree)
	if err != nil {
		return nil, fmt.Errorf("loading base tree %x: %w", commit.Tree.Hash, err)
	}

	w.base = ref
	w.result.Commit = commit
	w.baseScan = commit.ScanStartNs
	if w.baseScan == 0 {
		w.baseScan = commit.Timestamp * int64(time.Second)
	}

	w.logf("Diffing against commit %x from %s", ref.Hash, time.Unix(commit.Timestamp, 0))

	return tree.Nodes, nil
}

func (w *Walker) putCommit(ctx context.Context, tree *proto.Ref, partial bool) (*proto.Ref, *proto.Commit, error) {
	commit := &proto.Commit{
		Timestamp:   time.Now().Unix(),
		Tree:        tree,
		BackupSet:   w.Set,
		Parent:      w.base,
		AgentId:     w.AgentID,
		ScanStartNs: w.scanStart,
		Partial:     partial,
	}

	obj := proto.NewObject(commit)

	err := w.Index.Put(ctx, obj)
	if err != nil {
		return nil, nil, fmt.Errorf("storing commit: %w", err)
	}

	return obj.Ref(), commit, nil
}

func (w *Walker) included(rel string) bool {
	return w.Include == nil || w.Include(rel)
}

// walkDir compares one directory with its base nodes and returns the new
// node list and whether it differs from the base.
func (w *Walker) walkDir(ctx context.Context, dir, rel string, base []*proto.TreeNode, root bool) ([]*proto.TreeNode, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if base != nil && IsPermissionError(err) {
			w.logf("Cannot list %s (%v), keeping the previous version", dir, err)
			atomic.AddInt64(&w.result.Unreadable, 1)
			return base, false, nil
		}

		if IsPermissionError(err) {
			w.logf("Cannot list %s (%v), skipping", dir, err)
			atomic.AddInt64(&w.result.Unreadable, 1)
			return nil, true, nil
		}

		return nil, false, fmt.Errorf("listing %s: %w", dir, err)
	}

	baseByName := make(map[string]*proto.TreeNode, len(base))
	for _, node := range base {
		baseByName[node.Stat.Name] = node
	}

	results := make([]*proto.TreeNode, len(entries))
	changed := false
	group, groupCtx := errgroup.WithContext(ctx)
	var mtx sync.Mutex

	markChanged := func() {
		mtx.Lock()
		changed = true
		mtx.Unlock()
	}

	for i, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}

		name := entry.Name()
		childRel := path.Join(rel, name)
		childPath := filepath.Join(dir, name)
		baseNode := baseByName[name]

		if !w.included(childRel) {
			if baseNode != nil {
				markChanged()
			}
			continue
		}

		info, err := entry.Info()
		if err != nil {
			w.logf("Cannot stat %s (%v), skipping", childPath, err)
			atomic.AddInt64(&w.result.Unreadable, 1)
			markChanged()
			continue
		}

		switch {
		case info.IsDir():
			node, childChanged, err := w.walkChildDir(ctx, childPath, childRel, info, baseNode)
			if err != nil {
				return nil, false, err
			}

			results[i] = node
			if childChanged {
				markChanged()
			}

		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(childPath)
			if err != nil {
				w.logf("Cannot read symlink %s (%v), skipping", childPath, err)
				atomic.AddInt64(&w.result.Skipped, 1)
				markChanged()
				continue
			}

			node := &proto.TreeNode{Stat: w.stat(info, target)}
			results[i] = node
			if !nodeEqual(node, baseNode) {
				markChanged()
			}

		case info.Mode().IsRegular():
			atomic.AddInt64(&w.result.Files, 1)
			index := i
			stat := w.stat(info, "")

			if w.reusable(childPath, info, stat, baseNode) {
				atomic.AddInt64(&w.result.Reused, 1)
				node := &proto.TreeNode{Stat: stat, Ref: baseNode.Ref}
				results[index] = node
				if !nodeEqual(node, baseNode) {
					markChanged()
				}
				continue
			}

			w.gate.Start()
			fileCtx := groupCtx
			group.Go(func() error {
				defer w.gate.Done()

				node, err := w.backupFile(fileCtx, childPath, info, baseNode)
				if err != nil {
					return err
				}

				mtx.Lock()
				results[index] = node
				mtx.Unlock()

				if !nodeEqual(node, baseNode) {
					markChanged()
				}

				return nil
			})

		default:
			w.logf("Skipping irregular file %s (%s)", childPath, info.Mode().Type())
			atomic.AddInt64(&w.result.Skipped, 1)
			if baseNode != nil {
				markChanged()
			}
		}

		if root && w.checkpointDue() {
			// drain the files in flight so every finished entry has a node;
			// Wait cancels the group's context, so start a fresh group after
			err = group.Wait()
			if err != nil {
				return nil, false, err
			}

			group, groupCtx = errgroup.WithContext(ctx)

			err = w.checkpoint(ctx, results[:i+1], base, name)
			if err != nil {
				return nil, false, err
			}
		}
	}

	err = group.Wait()
	if err != nil {
		return nil, false, err
	}

	nodes := make([]*proto.TreeNode, 0, len(results))
	for _, node := range results {
		if node != nil {
			nodes = append(nodes, node)
		}
	}

	if len(nodes) != len(base) {
		changed = true
	}

	return SortNodes(nodes), changed, nil
}

func (w *Walker) walkChildDir(ctx context.Context, dir, rel string, info os.FileInfo, baseNode *proto.TreeNode) (*proto.TreeNode, bool, error) {
	var baseChildren []*proto.TreeNode
	if baseNode != nil && baseNode.Stat.IsDir() {
		tree, err := w.trees.load(ctx, baseNode.Ref)
		if err != nil {
			w.logf("Base tree %x for %s unavailable (%v), reading the directory in full", baseNode.Ref.Hash, dir, err)
		} else {
			baseChildren = tree.Nodes
		}
	}

	children, childChanged, err := w.walkDir(ctx, dir, rel, baseChildren, false)
	if err != nil {
		return nil, false, err
	}

	stat := w.stat(info, "")

	if !childChanged && baseChildren != nil {
		node := &proto.TreeNode{Stat: stat, Ref: baseNode.Ref}
		return node, !nodeEqual(node, baseNode), nil
	}

	ref, err := PutTree(ctx, w.Objects, children)
	if err != nil {
		return nil, false, fmt.Errorf("storing tree for %s: %w", dir, err)
	}

	return &proto.TreeNode{Stat: stat, Ref: ref}, true, nil
}

// reusable applies the change test: same metadata as the base node, older
// than the base run, not contradicted by the stat cache, and not picked for
// a sampled re-hash.
func (w *Walker) reusable(path string, info os.FileInfo, stat *proto.FileInfo, baseNode *proto.TreeNode) bool {
	if baseNode == nil || baseNode.Stat.GetType() != proto.NodeType_NODE_FILE || baseNode.Ref == nil {
		return false
	}

	if !metaEqual(baseNode.Stat, stat) {
		return false
	}

	// the racy rule: a file touched during or after the base run may have
	// been rewritten within the same timestamp tick
	if stat.MtimeNs >= w.baseScan {
		return false
	}

	if w.Cache != nil {
		if present, matches := w.Cache.Lookup(path, info); present && !matches {
			return false
		}
	}

	if w.ForceHashPercent > 0 && w.rand.Intn(100) < w.ForceHashPercent {
		ref, err := HashFile(path)
		if err != nil || !ref.Equal(baseNode.Ref) {
			w.logf("Sampled re-hash of %s differs from the recorded version, reading it", path)
			return false
		}
	}

	return true
}

// backupFile reads a file into blobs, retrying when the file changes under
// the read, and falls back to the base node when it cannot be read.
func (w *Walker) backupFile(ctx context.Context, path string, info os.FileInfo, baseNode *proto.TreeNode) (*proto.TreeNode, error) {
	known := w.knownParts(ctx, baseNode)

	var ref *proto.Ref
	var err error

	for attempt := 0; ; attempt++ {
		ref, err = w.readFile(ctx, path, known)
		if err != nil {
			break
		}

		after, statErr := os.Lstat(path)
		if statErr != nil || (after.Size() == info.Size() && after.ModTime().Equal(info.ModTime())) {
			break
		}

		info = after
		if attempt >= w.ReadRetries {
			w.logf("%s kept changing while it was read, recording the last read", path)
			atomic.AddInt64(&w.result.Torn, 1)
			break
		}
	}

	if errors.Is(err, errUnreadable) {
		atomic.AddInt64(&w.result.Unreadable, 1)

		if baseNode != nil && baseNode.Stat.GetType() == proto.NodeType_NODE_FILE {
			w.logf("Cannot read %s, keeping the previous version", path)
			return baseNode, nil
		}

		w.logf("Cannot read %s and no previous version exists, skipping", path)
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	atomic.AddInt64(&w.result.Read, 1)

	if w.Cache != nil {
		if cErr := w.Cache.Store(path, info, ref); cErr != nil {
			w.logf("Cannot update stat cache for %s: %v", path, cErr)
		}
	}

	return &proto.TreeNode{Stat: w.stat(info, ""), Ref: ref}, nil
}

func (w *Walker) readFile(ctx context.Context, path string, known map[string]struct{}) (*proto.Ref, error) {
	file, err := os.Open(path)
	if err != nil {
		if IsPermissionError(err) || IsLockError(err) {
			return nil, fmt.Errorf("%w: %v", errUnreadable, err)
		}

		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer file.Close()

	writer := newFileWriter(ctx, w.Objects)
	writer.known = known

	_, err = io.Copy(writer, file)
	if err != nil {
		if IsLockError(err) {
			return nil, fmt.Errorf("%w: %v", errUnreadable, err)
		}

		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	err = writer.Close()
	if err != nil {
		return nil, fmt.Errorf("storing %s: %w", path, err)
	}

	return writer.Ref(), nil
}

// knownParts returns the blob refs of the base version of a file, so only
// new parts are uploaded.
func (w *Walker) knownParts(ctx context.Context, baseNode *proto.TreeNode) map[string]struct{} {
	if baseNode == nil || baseNode.Stat.GetType() != proto.NodeType_NODE_FILE || baseNode.Ref == nil {
		return nil
	}

	obj, err := w.Objects.Get(ctx, baseNode.Ref)
	if err != nil {
		return nil
	}

	parts := obj.GetFile().GetParts()
	if len(parts) == 0 {
		return nil
	}

	known := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		known[string(part.Ref.GetHash())] = struct{}{}
	}

	return known
}

func (w *Walker) checkpointDue() bool {
	return w.CheckpointInterval > 0 && time.Since(w.last) >= w.CheckpointInterval
}

// checkpoint writes a partial commit from the finished root entries plus the
// base's nodes for the rest.
func (w *Walker) checkpoint(ctx context.Context, done []*proto.TreeNode, base []*proto.TreeNode, lastName string) error {
	nodes := make([]*proto.TreeNode, 0, len(done)+len(base))
	for _, node := range done {
		if node != nil {
			nodes = append(nodes, node)
		}
	}

	for _, node := range base {
		if node.Stat.Name > lastName {
			nodes = append(nodes, node)
		}
	}

	root, err := PutTree(ctx, w.Objects, SortNodes(nodes))
	if err != nil {
		return fmt.Errorf("storing checkpoint tree: %w", err)
	}

	ref, _, err := w.putCommit(ctx, root, true)
	if err != nil {
		return err
	}

	w.logf("Wrote checkpoint commit %x", ref.Hash)
	w.result.Checkpoints++
	w.last = time.Now()

	return nil
}

func (w *Walker) stat(info os.FileInfo, linkTarget string) *proto.FileInfo {
	user, group := OwnerNames(info)

	return proto.GetFileInfo(proto.WithDetails(info, user, group, linkTarget))
}

// metaEqual is the change test on stat data alone.
func metaEqual(a, b *proto.FileInfo) bool {
	return a.GetMtimeNs() == b.GetMtimeNs() &&
		a.GetSize() == b.GetSize() &&
		a.GetMode() == b.GetMode() &&
		a.GetType() == b.GetType()
}

// nodeEqual reports whether a new node would encode identically to the base.
func nodeEqual(node, base *proto.TreeNode) bool {
	if node == nil || base == nil {
		return false
	}

	a, b := node.Stat, base.Stat

	return metaEqual(a, b) &&
		a.GetName() == b.GetName() &&
		a.GetUser() == b.GetUser() &&
		a.GetGroup() == b.GetGroup() &&
		a.GetLinkTarget() == b.GetLinkTarget() &&
		node.Ref.Equal(base.Ref)
}

// HashFile computes the File ref a backup of path would produce without
// storing anything.
func HashFile(path string) (*proto.Ref, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	writer := newFileWriter(context.Background(), discardStore{})

	_, err = io.Copy(writer, file)
	if err != nil {
		return nil, err
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	return writer.Ref(), nil
}

// discardStore claims to hold everything, so a fileWriter on it only hashes.
type discardStore struct{}

func (discardStore) Put(context.Context, *proto.Object) error { return nil }
func (discardStore) Get(context.Context, *proto.Ref) (*proto.Object, error) {
	return nil, ErrNotFound
}
func (discardStore) Delete(context.Context, *proto.Ref) error { return nil }
func (discardStore) Walk(context.Context, bool, proto.ObjectType, ObjectReceiver) error {
	return nil
}
func (discardStore) Has(context.Context, *proto.Ref) (bool, error) { return true, nil }

// treeSource serves base trees from a prefetch cache, filling it a subtree
// at a time when the store can stream one.
type treeSource struct {
	getter  Getter
	fetcher TreeFetcher
	depth   uint32
	mtx     sync.Mutex
	objects map[string]*proto.Object
}

func (t *treeSource) Get(ctx context.Context, ref *proto.Ref) (*proto.Object, error) {
	key := string(ref.GetHash())

	t.mtx.Lock()
	obj, ok := t.objects[key]
	if ok {
		delete(t.objects, key)
	}
	t.mtx.Unlock()

	if ok {
		return obj, nil
	}

	if t.fetcher != nil {
		objects, err := t.fetcher.GetTree(ctx, ref, t.depth)
		if err == nil {
			t.mtx.Lock()
			for _, o := range objects {
				k := string(o.Ref().Hash)
				if k != key {
					t.objects[k] = o
				} else {
					obj = o
				}
			}
			t.mtx.Unlock()

			if obj != nil {
				return obj, nil
			}
		}
	}

	return t.getter.Get(ctx, ref)
}

func (t *treeSource) load(ctx context.Context, ref *proto.Ref) (*proto.Tree, error) {
	return LoadTree(ctx, t, ref)
}
