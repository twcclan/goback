package pack

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"

	"github.com/bits-and-blooms/bitset"
	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

// CollectOptions tunes one garbage collection run. Zero values take the
// defaults documented on each field.
type CollectOptions struct {
	// Readers is the number of parallel object reads during the mark (32).
	Readers int
	// DeadRatio selects an archive for sweeping once this share of its bytes
	// is dead (0.3).
	DeadRatio float64
	// ErasureBound selects an archive that has held dead objects this long
	// regardless of ratio (21 days).
	ErasureBound time.Duration
	// MinAge keeps every object younger than this, whatever the mark says (24h).
	MinAge time.Duration
	// NoSweep stops after the mark.
	NoSweep bool
	// TempDir holds the live-set runs and the checkpoints a crashed run
	// resumes from (os.TempDir()).
	TempDir string
	// Now is the snapshot time (time.Now()).
	Now time.Time
}

const (
	defaultGCReaders      = 32
	defaultGCDeadRatio    = 0.3
	defaultGCErasureBound = 21 * 24 * time.Hour
	defaultGCMinAge       = 24 * time.Hour
	liveRunLimit          = 1 << 20
	markChunk             = 64
)

// rootBatch is the number of roots marked between two checkpoints.
var rootBatch = 1024

// gcAfterBatch, when set, runs after every checkpoint; tests use it to
// interrupt a mark.
var gcAfterBatch func(batch int) error

func (o CollectOptions) withDefaults() CollectOptions {
	if o.Readers <= 0 {
		o.Readers = defaultGCReaders
	}
	if o.DeadRatio <= 0 {
		o.DeadRatio = defaultGCDeadRatio
	}
	if o.ErasureBound <= 0 {
		o.ErasureBound = defaultGCErasureBound
	}
	if o.MinAge <= 0 {
		o.MinAge = defaultGCMinAge
	}
	if o.TempDir == "" {
		o.TempDir = os.TempDir()
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}

	return o
}

// CollectReport summarizes one garbage collection run.
type CollectReport struct {
	Generation  uint64
	Archives    int
	Objects     uint64
	Roots       int
	Marked      uint64
	DeadObjects uint64
	DeadBytes   uint64
	// Resumed counts the root batches taken from a previous, interrupted run.
	Resumed int
	// ErasedArchives counts the archives flagged for holding the objects of
	// an erased commit.
	ErasedArchives int
	// SweepSkipped names the reason when the run marked but did not sweep.
	SweepSkipped     string
	Swept            int
	ReclaimedObjects uint64
	ReclaimedBytes   uint64
	Duration         time.Duration
}

// Collector is implemented by stores that can garbage collect themselves.
type Collector interface {
	Collect(ctx context.Context, opts CollectOptions) (*CollectReport, error)
}

var _ Collector = (*PackStorage)(nil)

type gcArchive struct {
	erase bool
	a     *archive
	idx   IndexFile
	bytes uint64
	prev  *gcFile
	cur   *bitset.BitSet
	next  *gcFile
}

type gcTombstone struct {
	ga     *gcArchive
	pos    int
	target refKey
}

type gcRun struct {
	ps       *PackStorage
	opts     CollectOptions
	gen      uint64
	prev     *gcState
	snapshot time.Time

	archives map[string]*gcArchive
	order    []*gcArchive
	sessions []*backup.Session
	erased   []refKey
	visited  *visitedSet
	runDir   string
	resumed  int

	roots      []refKey
	tombstones []gcTombstone
	// targets maps every tombstone target to whether the snapshot still holds it.
	targets map[refKey]bool
}

// Collect marks every object reachable from a live commit or pin, records
// the result beside each archive and rewrites archives whose dead share
// justifies it. Objects are dropped only after two consecutive generations
// found them unreachable. The context must not carry a session.
func (ps *PackStorage) Collect(ctx context.Context, opts CollectOptions) (*CollectReport, error) {
	if _, ok := backup.SessionFromContext(ctx); ok {
		return nil, errors.New("garbage collection runs outside a session")
	}

	opts = opts.withDefaults()
	started := time.Now()

	ps.compactorMtx.Lock()
	defer ps.compactorMtx.Unlock()

	prev, err := loadGCState(ps.storage)
	if err != nil {
		return nil, errors.Wrap(err, "loading gc state")
	}

	run := &gcRun{ps: ps, opts: opts, prev: prev, gen: 1, snapshot: opts.Now.UTC(), archives: make(map[string]*gcArchive), targets: make(map[refKey]bool)}
	if prev != nil {
		run.gen = prev.Generation + 1
	}

	run.sessions, err = ps.index.ListSessions()
	if err != nil {
		return nil, errors.Wrap(err, "listing sessions")
	}

	report := &CollectReport{Generation: run.gen}

	if err := run.takeSnapshot(); err != nil {
		return nil, err
	}

	report.Archives = len(run.order)
	for _, ga := range run.order {
		report.Objects += uint64(len(ga.idx))
	}

	if err := run.collectRoots(ctx); err != nil {
		return nil, err
	}
	report.Roots = len(run.roots)

	markStart := time.Now()
	live, err := run.mark(ctx)
	if err != nil {
		return nil, err
	}
	defer live.close()

	report.Resumed = run.resumed
	gcMarkDuration.Record(ctx, time.Since(markStart).Seconds())

	mergeStart := time.Now()
	report.Marked, err = run.merge(live)
	if err != nil {
		return nil, err
	}

	if err := run.flagErased(ctx); err != nil {
		return nil, err
	}

	if err := run.writeResults(); err != nil {
		return nil, err
	}

	for _, ga := range run.order {
		if ga.next.Erase {
			report.ErasedArchives++
		}
	}
	gcMergeDuration.Record(ctx, time.Since(mergeStart).Seconds())

	live.close()
	_ = os.RemoveAll(run.runDir)

	for _, ga := range run.order {
		report.DeadObjects += ga.next.DeadObjects
		report.DeadBytes += ga.next.DeadBytes
	}
	gcDeadBytes.Record(ctx, int64(report.DeadBytes))

	state := &gcState{Generation: run.gen, Snapshot: run.snapshot, Completed: time.Now().UTC()}
	if err := storeGCState(ps.storage, state); err != nil {
		return nil, errors.Wrap(err, "storing gc state")
	}

	log.Printf("GC generation %d marked %d of %d objects in %d archives, %d objects (%s) dead", run.gen, report.Marked, report.Objects, report.Archives, report.DeadObjects, humanize.Bytes(report.DeadBytes))

	report.SweepSkipped = run.sweepBlocker()
	if report.SweepSkipped == "" {
		sweepStart := time.Now()
		if err := run.sweep(ctx, report); err != nil {
			return nil, err
		}
		gcSweepDuration.Record(ctx, time.Since(sweepStart).Seconds())
		gcReclaimedBytes.Add(ctx, int64(report.ReclaimedBytes))

		state.Swept = true
		if err := storeGCState(ps.storage, state); err != nil {
			return nil, errors.Wrap(err, "storing gc state")
		}
	} else {
		log.Printf("GC generation %d did not sweep: %s", run.gen, report.SweepSkipped)
	}

	report.Duration = time.Since(started)

	return report, nil
}

// takeSnapshot fixes the set of committed archives this generation covers
// and loads their indexes and previous mark results.
func (r *gcRun) takeSnapshot() error {
	r.ps.mtx.RLock()
	archives := make([]*archive, 0, len(r.ps.archives))
	for _, a := range r.ps.archives {
		a.mtx.RLock()
		committed := a.readOnly && a.state == ArchiveCommitted
		a.mtx.RUnlock()

		if committed {
			archives = append(archives, a)
		}
	}
	r.ps.mtx.RUnlock()

	sort.Slice(archives, func(i, j int) bool { return archives[i].name < archives[j].name })

	for _, a := range archives {
		idx, err := a.getIndex()
		if err != nil {
			return errors.Wrapf(err, "loading index of %s", a.name)
		}

		ga := &gcArchive{a: a, idx: idx, prev: a.gcResult(), cur: bitset.New(uint(len(idx)))}
		for _, rec := range idx {
			ga.bytes += uint64(rec.Length)
		}

		r.archives[a.name] = ga
		r.order = append(r.order, ga)
	}

	return nil
}

// collectRoots finds the commits and pins of the snapshot that carry no
// tombstone. Tombstones are remembered so the merge can decide their fate.
func (r *gcRun) collectRoots(ctx context.Context) error {
	tombstoned := make(map[refKey]bool)

	for _, ga := range r.order {
		for pos, rec := range ga.idx {
			if proto.ObjectType(rec.Type) != proto.ObjectType_TOMBSTONE {
				continue
			}

			hdr, err := ga.a.readHeader(&ga.idx[pos])
			if err != nil {
				return errors.Wrapf(err, "reading tombstone %x in %s", rec.Sum, ga.a.name)
			}

			if hdr.TombstoneFor == nil {
				continue
			}

			target := keyOf(hdr.TombstoneFor.Hash)
			tombstoned[target] = true
			r.targets[target] = false
			r.tombstones = append(r.tombstones, gcTombstone{ga: ga, pos: pos, target: target})

			if hdr.Erase {
				r.erased = append(r.erased, target)
			}
		}
	}

	leases, err := r.ps.RestoreLeases(ctx)
	if err != nil {
		return errors.Wrap(err, "listing restore leases")
	}

	for _, ref := range leases {
		r.roots = append(r.roots, keyOf(ref.Hash))
	}

	for _, ga := range r.order {
		for _, rec := range ga.idx {
			t := proto.ObjectType(rec.Type)
			if t != proto.ObjectType_COMMIT && t != proto.ObjectType_PIN {
				continue
			}

			if key := keyOf(rec.Sum[:]); !tombstoned[key] {
				r.roots = append(r.roots, key)
			}
		}
	}

	return ctx.Err()
}

type visitedSet struct {
	shards [256]struct {
		mtx  sync.Mutex
		seen map[refKey]struct{}
	}
}

func newVisitedSet() *visitedSet {
	v := &visitedSet{}
	for i := range v.shards {
		v.shards[i].seen = make(map[refKey]struct{})
	}

	return v
}

// claim returns the keys not seen before and remembers them.
func (v *visitedSet) claim(keys []refKey) []refKey {
	fresh := keys[:0]
	for _, key := range keys {
		shard := &v.shards[key[0]]
		shard.mtx.Lock()
		_, seen := shard.seen[key]
		if !seen {
			shard.seen[key] = struct{}{}
			fresh = append(fresh, key)
		}
		shard.mtx.Unlock()
	}

	return fresh
}

// snapshotID identifies the generation, the archives it covers and the
// sorted roots, so a checkpoint is only resumed against the same snapshot
// and the same batches.
func (r *gcRun) snapshotID() string {
	h := sha256.New()
	fmt.Fprintf(h, "%d\n", r.gen)
	for _, ga := range r.order {
		fmt.Fprintf(h, "%s\n", ga.a.name)
	}

	for _, root := range r.roots {
		h.Write(root[:])
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// openRunDir prepares the run directory, keeping the runs of batches an
// interrupted mark of the same snapshot completed.
func (r *gcRun) openRunDir() (*liveRuns, map[int]bool, error) {
	r.runDir = filepath.Join(r.opts.TempDir, "goback-gc", fmt.Sprintf("gen-%d", r.gen))
	manifest := filepath.Join(r.runDir, "snapshot")

	if data, err := os.ReadFile(manifest); err != nil || string(data) != r.snapshotID() {
		if err := os.RemoveAll(r.runDir); err != nil {
			return nil, nil, err
		}

		if err := os.MkdirAll(r.runDir, 0o755); err != nil {
			return nil, nil, err
		}

		if err := os.WriteFile(manifest, []byte(r.snapshotID()), 0o644); err != nil {
			return nil, nil, err
		}
	}

	entries, err := os.ReadDir(r.runDir)
	if err != nil {
		return nil, nil, err
	}

	done := make(map[int]bool)
	for _, e := range entries {
		if batch, ok := strings.CutSuffix(strings.TrimPrefix(e.Name(), "batch-"), ".done"); ok && strings.HasPrefix(e.Name(), "batch-") {
			if n, err := strconv.Atoi(batch); err == nil {
				done[n] = true
			}
		}
	}

	live := newLiveRuns(r.runDir, liveRunLimit)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".run") {
			continue
		}

		batch, _, _ := strings.Cut(strings.TrimPrefix(e.Name(), "batch-"), "-")
		path := filepath.Join(r.runDir, e.Name())

		if n, err := strconv.Atoi(batch); err == nil && done[n] {
			live.files = append(live.files, path)
		} else {
			_ = os.Remove(path)
		}
	}

	return live, done, nil
}

// mark walks breadth-first from the roots, in checkpointed batches, and
// returns the live refs. Blobs are marked from the File that names them
// and never read.
func (r *gcRun) mark(ctx context.Context) (*liveRuns, error) {
	sortKeys(r.roots)

	live, done, err := r.openRunDir()
	if err != nil {
		return nil, err
	}

	visited := newVisitedSet()
	r.visited = visited

	for batch := 0; batch*rootBatch < len(r.roots); batch++ {
		roots := r.roots[batch*rootBatch : min((batch+1)*rootBatch, len(r.roots))]

		if done[batch] {
			r.resumed++
			continue
		}

		live.prefix = batchPrefix(batch)

		err := r.markBatch(ctx, visited.claim(append([]refKey(nil), roots...)), visited, live)
		if err != nil {
			return nil, err
		}

		if err := live.checkpoint(batch); err != nil {
			return nil, err
		}

		if gcAfterBatch != nil {
			if err := gcAfterBatch(batch); err != nil {
				return nil, err
			}
		}
	}

	return live, nil
}

func (r *gcRun) markBatch(ctx context.Context, frontier []refKey, visited *visitedSet, live *liveRuns) error {
	for len(frontier) > 0 {
		var next []refKey
		var nextMtx sync.Mutex

		grp, gctx := errgroup.WithContext(ctx)
		grp.SetLimit(r.opts.Readers)

		for start := 0; start < len(frontier); start += markChunk {
			chunk := frontier[start:min(start+markChunk, len(frontier))]

			grp.Go(func() error {
				var children, found []refKey

				for _, key := range chunk {
					obj, err := r.ps.Get(gctx, &proto.Ref{Hash: key[:]})
					if errors.Is(err, backup.ErrNotFound) {
						log.Printf("GC: reachable object %x is missing", key)
						continue
					}
					if err != nil {
						return errors.Wrapf(err, "reading %x", key)
					}

					found = append(found, key)
					children = appendChildren(children, &found, obj)
				}

				if err := live.add(found); err != nil {
					return err
				}

				fresh := visited.claim(children)
				if len(fresh) > 0 {
					nextMtx.Lock()
					next = append(next, fresh...)
					nextMtx.Unlock()
				}

				return nil
			})
		}

		if err := grp.Wait(); err != nil {
			return err
		}

		frontier = next
	}

	return nil
}

// appendChildren adds the object's metadata children to children and its
// blob parts straight to live.
func appendChildren(children []refKey, live *[]refKey, obj *proto.Object) []refKey {
	switch obj.Type() {
	case proto.ObjectType_COMMIT:
		if tree := obj.GetCommit().GetTree(); tree != nil {
			children = append(children, keyOf(tree.Hash))
		}
	case proto.ObjectType_TREE:
		tree := obj.GetTree()
		for _, node := range tree.GetNodes() {
			if node.GetRef() != nil {
				children = append(children, keyOf(node.Ref.Hash))
			}
		}
		for _, split := range tree.GetSplits() {
			children = append(children, keyOf(split.Hash))
		}
	case proto.ObjectType_FILE:
		file := obj.GetFile()
		for _, part := range file.GetParts() {
			if part.GetRef() != nil {
				*live = append(*live, keyOf(part.Ref.Hash))
			}
		}
		for _, split := range file.GetSplits() {
			children = append(children, keyOf(split.Hash))
		}
	case proto.ObjectType_PIN:
		if target := obj.GetPin().GetTarget(); target != nil {
			children = append(children, keyOf(target.Hash))
		}
	}

	return children
}

type mergeHead struct {
	ga  *gcArchive
	pos int
}

type mergeHeap []mergeHead

func (h mergeHeap) Len() int { return len(h) }
func (h mergeHeap) Less(i, j int) bool {
	return bytes.Compare(h[i].ga.idx[h[i].pos].Sum[:], h[j].ga.idx[h[j].pos].Sum[:]) < 0
}
func (h mergeHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *mergeHeap) Push(x interface{}) { *h = append(*h, x.(mergeHead)) }
func (h *mergeHeap) Pop() interface{} {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]

	return x
}

// merge walks the live set and every archive index in lockstep and sets the
// bit of each index position that is live. A tombstone is live while the
// snapshot still holds its target.
func (r *gcRun) merge(live *liveRuns) (uint64, error) {
	var marked uint64

	err := r.scan(live, func(ga *gcArchive, pos int) {
		ga.cur.Set(uint(pos))
		marked++
	}, func(sum refKey) {
		if _, isTarget := r.targets[sum]; isTarget {
			r.targets[sum] = true
		}
	})
	if err != nil {
		return 0, err
	}

	for _, t := range r.tombstones {
		if r.targets[t.target] {
			t.ga.cur.Set(uint(t.pos))
			marked++
		}
	}

	return marked, nil
}

// scan walks every index record of the snapshot in ref order alongside the
// sorted runs; hit sees the records the runs name, each sees every record.
func (r *gcRun) scan(runs *liveRuns, hit func(ga *gcArchive, pos int), each func(sum refKey)) error {
	it, err := runs.iterator()
	if err != nil {
		return err
	}
	defer it.close()

	var h mergeHeap
	for _, ga := range r.order {
		if len(ga.idx) > 0 {
			h = append(h, mergeHead{ga: ga})
		}
	}
	heap.Init(&h)

	cur, ok := it.next()

	for h.Len() > 0 {
		top := h[0]
		sum := top.ga.idx[top.pos].Sum

		for ok && bytes.Compare(cur[:], sum[:]) < 0 {
			cur, ok = it.next()
		}

		if ok && cur == sum {
			hit(top.ga, top.pos)
		}

		if each != nil {
			each(sum)
		}

		top.pos++
		if top.pos < len(top.ga.idx) {
			h[0] = top
			heap.Fix(&h, 0)
		} else {
			heap.Pop(&h)
		}
	}

	return nil
}

// flagErased walks the subtrees of the erased commits still in the
// snapshot, skipping everything the live mark reached, and flags the
// archives holding what is left.
func (r *gcRun) flagErased(ctx context.Context) error {
	var roots []refKey
	for _, target := range r.erased {
		if r.targets[target] {
			roots = append(roots, target)
		}
	}

	if len(roots) == 0 {
		return nil
	}

	runs := newLiveRuns(r.runDir, liveRunLimit)
	runs.prefix = "erased-"
	defer runs.close()

	if err := r.markBatch(ctx, r.visited.claim(roots), r.visited, runs); err != nil {
		return err
	}

	return r.scan(runs, func(ga *gcArchive, _ int) { ga.erase = true }, nil)
}

func (r *gcRun) writeResults() error {
	for _, ga := range r.order {
		next := &gcFile{Generation: r.gen, Snapshot: r.snapshot, Current: ga.cur, Erase: ga.erase}

		if ga.prev != nil && ga.prev.Generation == r.gen-1 {
			next.Previous = ga.prev.Current
		}

		for pos, rec := range ga.idx {
			if ga.cur.Test(uint(pos)) {
				continue
			}

			next.DeadObjects++
			next.DeadBytes += uint64(rec.Length)
			next.Dead = append(next.Dead, prefixOf(rec.Sum[:]))
		}

		sort.Slice(next.Dead, func(i, j int) bool { return next.Dead[i] < next.Dead[j] })

		if next.DeadObjects > 0 {
			next.DeadSince = r.snapshot
			if ga.prev != nil && !ga.prev.DeadSince.IsZero() {
				next.DeadSince = ga.prev.DeadSince
			}
		}

		if err := writeGCFile(r.ps.storage, ga.a.name, next); err != nil {
			return errors.Wrapf(err, "writing gc result of %s", ga.a.name)
		}

		ga.next = next
		ga.a.setGCResult(next)
	}

	return nil
}

// sweepBlocker returns why this generation must not sweep, or "".
func (r *gcRun) sweepBlocker() string {
	if r.opts.NoSweep {
		return "sweep disabled"
	}

	if r.prev == nil {
		return "first generation"
	}

	for _, s := range r.sessions {
		if s.Started.Before(r.prev.Completed) {
			return fmt.Sprintf("session %s started before generation %d completed", s.ID, r.prev.Generation)
		}
	}

	return ""
}

// sweep rewrites the archives whose dead share or age selects them, dropping
// objects unmarked in two consecutive generations.
func (r *gcRun) sweep(ctx context.Context, report *CollectReport) error {
	groups := make(map[string]*compactionGroup)
	var keys []string

	for _, ga := range r.order {
		if !r.selected(ga) {
			continue
		}

		placement := ParsePlacement(ga.a.name).Group()
		group, ok := groups[placement.Dir()]
		if !ok {
			group = &compactionGroup{placement: placement, keep: r.keep, marked: r.marked}
			groups[placement.Dir()] = group
			keys = append(keys, placement.Dir())
		}

		group.candidates = append(group.candidates, ga.a)
		group.total += ga.a.size
	}

	sort.Strings(keys)

	for _, key := range keys {
		group := groups[key]

		log.Printf("GC sweeping %d archives under %q", len(group.candidates), key)

		if err := r.ps.compactGroup(ctx, group); err != nil {
			return err
		}

		report.Swept += len(group.candidates)
		report.ReclaimedObjects += group.droppedObjects
		report.ReclaimedBytes += group.droppedBytes
	}

	return nil
}

// selected reports whether an archive is worth rewriting this generation.
func (r *gcRun) selected(ga *gcArchive) bool {
	if ga.next.Previous == nil || ga.bytes == 0 {
		return false
	}

	var droppable uint64
	for pos, rec := range ga.idx {
		if ga.next.dead(pos) {
			droppable += uint64(rec.Length)
		}
	}

	if droppable == 0 {
		return false
	}

	if ga.next.Erase || float64(droppable)/float64(ga.bytes) >= r.opts.DeadRatio {
		return true
	}

	return r.opts.Now.Sub(ga.next.DeadSince) >= r.opts.ErasureBound
}

// keep decides during a rewrite whether the candidate's object survives.
func (r *gcRun) keep(candidate *archive, hdr *proto.ObjectHeader) bool {
	ga := r.archives[candidate.name]
	if ga == nil {
		return true
	}

	pos := ga.idx.position(hdr.Ref.Hash)
	if pos < 0 || !ga.next.dead(pos) {
		return true
	}

	if hdr.Timestamp != nil && r.opts.Now.Sub(hdr.Timestamp.AsTime()) < r.opts.MinAge {
		return true
	}

	return false
}

// marked reports whether the copy at loc was reachable in this generation.
// A copy outside the snapshot is younger than it and counts as reachable.
func (r *gcRun) marked(loc *IndexLocation) bool {
	ga := r.archives[loc.Archive]
	if ga == nil {
		return true
	}

	pos := ga.idx.position(loc.Record.Sum[:])

	return pos < 0 || ga.cur.Test(uint(pos))
}

func prefixOf(hash []byte) uint64 {
	var prefix uint64
	for i := 0; i < 8 && i < len(hash); i++ {
		prefix = prefix<<8 | uint64(hash[i])
	}

	return prefix
}
