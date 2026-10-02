package pack

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/sha256"
	"fmt"
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
	// Handoff publishes the sweep as a plan for RewritePlan instead of
	// running it.
	Handoff bool
	// Quarantine is how long the files of an archive a rewrite retired
	// are kept before a collection deletes them (DefaultQuarantine).
	Quarantine time.Duration
	// TempDir holds the live-set runs and the checkpoints a crashed run
	// resumes from (os.TempDir()).
	TempDir string
	// Now is the snapshot time (time.Now()).
	Now time.Time
	// Owner names what a root belongs to, so the mark can attribute the
	// objects it reaches; nil, or a zero Attribution, attributes nothing.
	Owner func(root []byte) Attribution
}

// Attribution is what a root belongs to: the group its objects are
// counted in and the set they are recorded against. An object is counted
// once per group, so two groups that both hold it each carry it in full,
// and within a group it goes to the first set the mark reached it from.
type Attribution struct {
	Group int64
	Set   int64
}

func (a Attribution) before(b Attribution) bool {
	if a.Group != b.Group {
		return a.Group < b.Group
	}

	return a.Set < b.Set
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
	if o.Quarantine <= 0 {
		o.Quarantine = DefaultQuarantine
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
	// SetBytes is what each set's live objects take up in the archives,
	// keyed by the set ids Owner returned. Within a group an object counts
	// once, for the first set the mark reached it from; across groups it
	// counts in each.
	SetBytes map[int64]uint64
	// SetDeduplicated is the uncompressed size of the distinct file content
	// each set's live objects carry, attributed as SetBytes is.
	SetDeduplicated map[int64]uint64
	// SetAlone is what each set's live objects would take up were it the
	// only set of its group, and SetExclusive what of that no other set of
	// the group reaches: what deleting the set would free.
	SetAlone, SetExclusive map[int64]uint64
	// Condemned counts the unreachable objects the run stored tombstones for.
	Condemned int
	// Waiting is the generation whose published plan has not been
	// rewritten yet; a run that waits does nothing else.
	Waiting uint64
	// SweepSkipped names the reason when the run marked but did not sweep.
	SweepSkipped string
	// Published counts the archives of the plan the run published.
	Published        int
	Swept            int
	ReclaimedObjects uint64
	ReclaimedBytes   uint64
	// Purged counts the quarantined files the run deleted.
	Purged   int
	Duration time.Duration
}

// Collector is implemented by stores that can garbage collect themselves.
type Collector interface {
	Collect(ctx context.Context, opts CollectOptions) (*CollectReport, error)
}

var _ Collector = (*PackStorage)(nil)

type gcArchive struct {
	erase bool
	a     *archive
	// count is how many records the archive's index holds, and droppable
	// how many bytes of it this generation may drop.
	count     int
	droppable uint64
	lookup    *indexLookup
	bytes     uint64
	prev      *gcFile
	cur       *bitset.BitSet
	next      *gcFile
}

type gcTombstone struct {
	ga      *gcArchive
	pos     int
	target  refKey
	version Version
}

// indexLookup answers the random lookups of a sweep. It loads the
// archive's index the first time it is asked and drops it when the run
// ends, so only the archives a sweep touches are ever held.
type indexLookup struct {
	a *archive

	mu     sync.Mutex
	idx    IndexFile
	loaded bool
}

// position returns the index position of hash, or -1.
func (l *indexLookup) position(hash []byte) (int, error) {
	pos, _, err := l.record(hash)

	return pos, err
}

// record returns the index position and record of hash, or -1.
func (l *indexLookup) record(hash []byte) (int, *IndexRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.loaded {
		idx, err := l.a.getIndex()
		if err != nil {
			return -1, nil, err
		}

		l.idx, l.loaded = idx, true
	}

	pos := l.idx.position(hash)
	if pos < 0 {
		return -1, nil, nil
	}

	return pos, &l.idx[pos], nil
}

func (l *indexLookup) release() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.idx, l.loaded = nil, false
}

type gcRun struct {
	ps       *PackStorage
	opts     CollectOptions
	gen      uint64
	prev     *gcState
	snapshot time.Time

	archives map[string]*gcArchive
	order    []*gcArchive
	// pending are the finalized archives of live sessions that have not
	// committed, read for the un-tombstones a committing session wrote
	pending []*archive
	erased  []refKey
	visited  *visitedSet
	runDir   string
	resumed  int

	roots      []gcRoot
	tombstones []gcTombstone
	// targets maps every tombstone target to whether the snapshot still holds it.
	targets map[refKey]bool
	// newestTomb is the version of the newest tombstone of each target in
	// the snapshot, and condemning that of the newest one the previous
	// generation's horizon covers.
	newestTomb  map[refKey]Version
	condemning  map[refKey]Version
	condemnedAt map[int64]bool
	// condemned maps what this generation condemns to its newest copy.
	condemned map[refKey]Version
	// tombTimes are the versions the snapshot's tombstones carry.
	tombTimes map[int64]bool
	// untombed are the objects an un-tombstone takes back, roots of the
	// mark; oldestCopy is the version of the oldest copy of each tombstone
	// target, and spent the tombstones with nothing left to condemn.
	untombed   map[refKey]bool
	oldestCopy map[refKey]Version
	spent      map[recordAt]bool
	// horizonEnded is whether every session of the previous horizon has
	// ended.
	horizonEnded bool
	// setBytes and setDeduplicated are what the mark attributed to each set.
	setBytes        map[int64]uint64
	setDeduplicated map[int64]uint64
	// setAlone and setExclusive are what each set reaches, and what only
	// it reaches within its group.
	setAlone, setExclusive map[int64]uint64
}

// gcRoot is a root with what it belongs to, zero when nothing names it.
type gcRoot struct {
	key   refKey
	owner Attribution
}

// Collect marks every object reachable from a live commit or pin, records
// the result beside each archive and rewrites archives whose dead share
// justifies it. An unreachable object gets a tombstone, and the next
// generation drops it if it is still unreachable and no session that
// relied on it took the tombstone back as it committed. The context must
// not carry a session.
func (ps *PackStorage) Collect(ctx context.Context, opts CollectOptions) (*CollectReport, error) {
	if _, ok := backup.SessionFromContext(ctx); ok {
		return nil, errors.New("garbage collection runs outside a session")
	}

	opts = opts.withDefaults()
	started := time.Now()

	ctx, span := tracer.Start(ctx, "PackStorage.Collect")
	defer span.End()

	ps.compactorMtx.Lock()
	defer ps.compactorMtx.Unlock()

	pending, err := ps.PendingPlans()
	if err != nil {
		return nil, errors.Wrap(err, "listing published plans")
	}

	if len(pending) > 0 {
		return &CollectReport{Waiting: pending[0], Duration: time.Since(started)}, nil
	}

	prev, err := loadGCState(ps.storage)
	if err != nil {
		return nil, errors.Wrap(err, "loading gc state")
	}

	run := &gcRun{ps: ps, opts: opts, prev: prev, gen: 1, snapshot: opts.Now.UTC(),
		archives: make(map[string]*gcArchive), targets: make(map[refKey]bool),
		newestTomb: make(map[refKey]Version), condemning: make(map[refKey]Version),
		condemned: make(map[refKey]Version), condemnedAt: make(map[int64]bool), tombTimes: make(map[int64]bool),
		untombed: make(map[refKey]bool), oldestCopy: make(map[refKey]Version), spent: make(map[recordAt]bool),
		setBytes: make(map[int64]uint64), setDeduplicated: make(map[int64]uint64),
		setAlone: make(map[int64]uint64), setExclusive: make(map[int64]uint64)}
	if prev != nil {
		run.gen = prev.Generation + 1

		for _, t := range prev.Condemned {
			run.condemnedAt[t.UnixNano()] = true
		}
	}

	report := &CollectReport{Generation: run.gen}

	sessions, err := run.liveSessions()
	if err != nil {
		return nil, errors.Wrap(err, "listing sessions")
	}

	if err := ps.pruneSeals(sessions); err != nil {
		return nil, errors.Wrap(err, "pruning seals")
	}

	if prev != nil {
		run.horizonEnded = true
		for _, id := range prev.Horizon {
			run.horizonEnded = run.horizonEnded && !sessions[id]
		}
	}

	if err := run.takeSnapshot(); err != nil {
		return nil, err
	}

	defer func() {
		for _, ga := range run.order {
			ga.lookup.release()
		}
	}()

	report.Archives = len(run.order)
	for _, ga := range run.order {
		report.Objects += uint64(ga.count)
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
	markTook := time.Since(markStart)
	gcMarkDuration.Record(ctx, markTook.Seconds())

	mergeStart := time.Now()
	report.SetBytes = run.setBytes
	report.SetDeduplicated = run.setDeduplicated
	report.SetAlone, report.SetExclusive = run.setAlone, run.setExclusive
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
	mergeTook := time.Since(mergeStart)
	gcMergeDuration.Record(ctx, mergeTook.Seconds())

	live.close()
	_ = os.RemoveAll(run.runDir)

	for _, ga := range run.order {
		report.DeadObjects += ga.next.DeadObjects
		report.DeadBytes += ga.next.DeadBytes
	}
	gcDeadBytes.Record(ctx, int64(report.DeadBytes))

	if err := run.condemn(ctx); err != nil {
		return nil, err
	}
	report.Condemned = len(run.condemned)

	condemned, horizon, err := run.horizon()
	if err != nil {
		return nil, err
	}

	// a session that commits from here on copies what these condemn under
	// what it relied on, or wrote its un-tombstones before this
	if err := ps.writeSeal(seal{Generation: run.gen, Condemned: condemned}); err != nil {
		return nil, errors.Wrap(err, "sealing")
	}

	state := &gcState{Generation: run.gen, Snapshot: run.snapshot, Condemned: condemned, Horizon: horizon}
	if err := storeGCState(ps.storage, state); err != nil {
		return nil, errors.Wrap(err, "storing gc state")
	}

	ps.logger.Info("gc marked", "generation", run.gen, "marked", report.Marked, "objects", report.Objects, "archives", report.Archives, "dead", report.DeadObjects, "deadBytes", humanize.Bytes(report.DeadBytes),
		"mark", markTook.Round(time.Millisecond), "merge", mergeTook.Round(time.Millisecond))

	report.SweepSkipped = run.sweepBlocker()
	if report.SweepSkipped == "" && run.anySelected() {
		if err := run.confirmDrops(ctx); err != nil {
			return nil, err
		}
	}

	if report.SweepSkipped == "" && opts.Handoff {
		if report.Published, err = run.publish(); err != nil {
			return nil, err
		}
	} else if report.SweepSkipped == "" {
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
		ps.logger.Info("gc did not sweep", "generation", run.gen, "reason", report.SweepSkipped)
	}

	report.Purged, err = ps.PurgeQuarantine(opts.Quarantine, opts.Now)
	if err != nil {
		return nil, errors.Wrap(err, "purging the quarantine")
	}

	report.Duration = time.Since(started)

	return report, nil
}

// scanArchive walks an archive's index record by record, counting the
// records and their bytes when asked.
func scanArchive(a *archive, fn func(pos int, rec *IndexRecord) error, count *int, size *uint64) error {
	scanner, err := a.scanIndex()
	if err != nil {
		return err
	}
	defer scanner.close()

	for pos := 0; ; pos++ {
		record, err := scanner.next()
		if err != nil {
			return err
		}

		if record == nil {
			return nil
		}

		if count != nil {
			*count++
		}

		if size != nil {
			*size += uint64(record.Length)
		}

		if err := fn(pos, record); err != nil {
			return err
		}
	}
}

// takeSnapshot fixes the set of committed archives this generation covers
// and loads their indexes and previous mark results.
func (r *gcRun) takeSnapshot() error {
	// listed first: a session that commits after this is either live here
	// or has its archives committed in the snapshot
	live, err := r.liveSessions()
	if err != nil {
		return errors.Wrap(err, "listing sessions")
	}

	if err := r.ps.refreshArchives(); err != nil {
		return errors.Wrap(err, "catching up with the storage's archives")
	}

	r.ps.mtx.RLock()
	archives := make([]*archive, 0, len(r.ps.archives))
	for _, a := range r.ps.archives {
		a.mtx.RLock()
		committed := a.readOnly && a.state == ArchiveCommitted
		pending := a.readOnly && a.state == ArchivePending && live[a.session]
		a.mtx.RUnlock()

		if committed {
			archives = append(archives, a)
		}

		if pending {
			r.pending = append(r.pending, a)
		}
	}
	r.ps.mtx.RUnlock()

	sort.Slice(archives, func(i, j int) bool { return archives[i].name < archives[j].name })

	for _, a := range archives {
		ga := &gcArchive{a: a, prev: a.gcResult()}

		err := scanArchive(a, func(int, *IndexRecord) error { return nil }, &ga.count, &ga.bytes)
		if err != nil {
			return errors.Wrapf(err, "reading index of %s", a.name)
		}

		ga.cur = bitset.New(uint(ga.count))
		ga.lookup = &indexLookup{a: a}

		r.archives[a.name] = ga
		r.order = append(r.order, ga)
	}

	return nil
}

// collectRoots finds the commits and pins of the snapshot that carry no
// tombstone. Tombstones are remembered so the merge can decide their fate.
// root pairs a root ref with the set Owner names it for.
func (r *gcRun) root(hash []byte) gcRoot {
	root := gcRoot{key: keyOf(hash)}
	if r.opts.Owner != nil {
		root.owner = r.opts.Owner(hash)
	}

	return root
}

// batches groups the roots into the units the mark checkpoints. A batch
// holds the roots of one set only, so every run it spills belongs to that
// set.
func (r *gcRun) batches() [][]gcRoot {
	var batches [][]gcRoot

	for start := 0; start < len(r.roots); {
		end := start
		for end < len(r.roots) && end-start < rootBatch && r.roots[end].owner == r.roots[start].owner {
			end++
		}

		batches = append(batches, r.roots[start:end])
		start = end
	}

	return batches
}

func (r *gcRun) collectRoots(ctx context.Context) error {
	tombstoned := make(map[refKey]bool)
	// tombOf maps a tombstone's own ref to its target
	tombOf := make(map[refKey]refKey)

	for _, ga := range r.order {
		err := scanArchive(ga.a, func(pos int, rec *IndexRecord) error {
			if proto.ObjectType(rec.Type) != proto.ObjectType_TOMBSTONE {
				return nil
			}

			hdr, err := ga.a.readHeader(rec)
			if err != nil {
				return errors.Wrapf(err, "reading tombstone %x in %s", rec.Sum, ga.a.name)
			}

			if hdr.TombstoneFor == nil {
				return nil
			}

			target := keyOf(hdr.TombstoneFor.Hash)
			tombstoned[target] = true
			tombOf[keyOf(rec.Sum[:])] = target
			r.targets[target] = false

			v := ga.a.version(*rec)
			r.tombstones = append(r.tombstones, gcTombstone{ga: ga, pos: pos, target: target, version: v})

			newestAt(r.newestTomb, target, v)
			r.tombTimes[v.Time.UnixNano()] = true
			if r.condemnedAt[v.Time.UnixNano()] {
				newestAt(r.condemning, target, v)
			}

			if hdr.Erase {
				r.erased = append(r.erased, target)
			}

			return nil
		}, nil, nil)
		if err != nil {
			return errors.Wrapf(err, "reading index of %s", ga.a.name)
		}
	}

	takeBack := func(tomb refKey) {
		if untombed, ok := tombOf[tomb]; ok && !r.untombed[untombed] {
			r.untombed[untombed] = true
			r.roots = append(r.roots, r.root(untombed[:]))
		}
	}

	for _, t := range r.tombstones {
		takeBack(t.target)
	}

	// a session commits only after copying what it relies on, so its
	// un-tombstones stay its own until then; they keep what they take
	// back from the moment they are written
	for _, a := range r.pending {
		err := scanArchive(a, func(_ int, rec *IndexRecord) error {
			if proto.ObjectType(rec.Type) != proto.ObjectType_TOMBSTONE {
				return nil
			}

			hdr, err := a.readHeader(rec)
			if err != nil {
				return errors.Wrapf(err, "reading tombstone %x in %s", rec.Sum, a.name)
			}

			if hdr.TombstoneFor != nil {
				takeBack(keyOf(hdr.TombstoneFor.Hash))
			}

			return nil
		}, nil, nil)
		if err != nil && !notExist(err) {
			return errors.Wrapf(err, "reading index of %s", a.name)
		}
	}

	leases, err := r.ps.RestoreLeases(ctx)
	if err != nil {
		return errors.Wrap(err, "listing restore leases")
	}

	for _, ref := range leases {
		r.roots = append(r.roots, r.root(ref.Hash))
	}

	for _, ga := range r.order {
		err := scanArchive(ga.a, func(_ int, rec *IndexRecord) error {
			t := proto.ObjectType(rec.Type)
			if _, isTarget := r.targets[keyOf(rec.Sum[:])]; isTarget && t != proto.ObjectType_TOMBSTONE {
				oldestAt(r.oldestCopy, keyOf(rec.Sum[:]), ga.a.version(*rec))
			}

			if t != proto.ObjectType_COMMIT && t != proto.ObjectType_PIN && t != proto.ObjectType_POLICY {
				return nil
			}

			if key := keyOf(rec.Sum[:]); !tombstoned[key] {
				r.roots = append(r.roots, r.root(rec.Sum[:]))
			}

			return nil
		}, nil, nil)
		if err != nil {
			return errors.Wrapf(err, "reading index of %s", ga.a.name)
		}
	}

	r.spendTombstones(tombOf)

	return ctx.Err()
}

// recordAt names a record of the snapshot.
type recordAt struct {
	archive string
	pos     int
}

// spendTombstones finds the tombstones the previous horizon covers that
// condemn nothing any more: an un-tombstone took them back, or no copy
// they hide is left. Un-tombstones are left to outlive their targets.
func (r *gcRun) spendTombstones(tombOf map[refKey]refKey) {
	for _, t := range r.tombstones {
		if _, untomb := tombOf[t.target]; untomb {
			continue
		}

		if !r.condemnedAt[t.version.Time.UnixNano()] {
			continue
		}

		oldest, hides := r.oldestCopy[t.target]
		if r.untombed[t.target] || !hides || t.version.Before(oldest) {
			r.spent[recordAt{t.ga.a.name, t.pos}] = true
		}
	}
}

func oldestAt(versions map[refKey]Version, key refKey, v Version) {
	if old, ok := versions[key]; !ok || v.Before(old) {
		versions[key] = v
	}
}

// visitedSet remembers, for each object of a group, the bits of the sets
// that reached it, so a set walks into what another set already walked
// and its run holds everything it reaches. A group of more than 64 sets
// shares the last bit among the rest, which then do not walk into what
// one of them reached.
type visitedSet struct {
	shards [256]struct {
		mtx  sync.Mutex
		seen map[refKey]uint64
	}
	bits map[int64]uint64
}

func newVisitedSet() *visitedSet {
	v := &visitedSet{bits: make(map[int64]uint64)}
	for i := range v.shards {
		v.shards[i].seen = make(map[refKey]uint64)
	}

	return v
}

// bit is the set's bit in the group, given out in the order sets come.
func (v *visitedSet) bit(set int64) uint64 {
	if b, ok := v.bits[set]; ok {
		return b
	}

	b := uint64(1) << min(len(v.bits), 63)
	v.bits[set] = b

	return b
}

// claim returns the keys the set with that bit has not reached before,
// and remembers that it has.
func (v *visitedSet) claim(keys []refKey, bit uint64) []refKey {
	fresh := keys[:0]
	for _, key := range keys {
		shard := &v.shards[key[0]]
		shard.mtx.Lock()
		if seen := shard.seen[key]; seen&bit == 0 {
			shard.seen[key] = seen | bit
			fresh = append(fresh, key)
		}
		shard.mtx.Unlock()
	}

	return fresh
}

// claimUnreached returns the keys no set has reached, and marks them
// reached by all.
func (v *visitedSet) claimUnreached(keys []refKey) []refKey {
	fresh := keys[:0]
	for _, key := range keys {
		shard := &v.shards[key[0]]
		shard.mtx.Lock()
		if shard.seen[key] == 0 {
			shard.seen[key] = ^uint64(0)
			fresh = append(fresh, key)
		}
		shard.mtx.Unlock()
	}

	return fresh
}

// snapshotID identifies the generation, the run format, the archives it
// covers and the sorted roots, so a checkpoint is only resumed against the
// same snapshot and the same batches, by a build that reads its runs.
func (r *gcRun) snapshotID() string {
	h := sha256.New()
	fmt.Fprintf(h, "%d %d\n", r.gen, liveRefSize)
	for _, ga := range r.order {
		fmt.Fprintf(h, "%s\n", ga.a.name)
	}

	for _, root := range r.roots {
		h.Write(root.key[:])
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// openRunDir prepares the run directory, keeping the runs of batches an
// interrupted mark of the same snapshot completed.
func (r *gcRun) openRunDir(batches [][]gcRoot) (*liveRuns, map[int]bool, error) {
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

		if n, err := strconv.Atoi(batch); err == nil && done[n] && n < len(batches) {
			live.adopt(path, batches[n][0].owner)
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
	sortRoots(r.roots)
	batches := r.batches()

	live, done, err := r.openRunDir(batches)
	if err != nil {
		return nil, err
	}

	// objects are counted once per group, so each group walks with a
	// visited set of its own; the roots are sorted by group, so only the
	// one being walked is ever held
	var group int64
	visited := newVisitedSet()
	r.visited = visited

	for batch, roots := range batches {
		if owner := roots[0].owner; owner.Group != group {
			group, visited = owner.Group, newVisitedSet()
			r.visited = visited
		}

		if done[batch] {
			r.resumed++
			continue
		}

		live.begin(batchPrefix(batch), roots[0].owner)

		bit := visited.bit(roots[0].owner.Set)
		claim := func(keys []refKey) []refKey { return visited.claim(keys, bit) }

		err := r.markBatch(ctx, claim(keysOf(roots)), claim, live)
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

func (r *gcRun) markBatch(ctx context.Context, frontier []refKey, claim func([]refKey) []refKey, live *liveRuns) error {
	for len(frontier) > 0 {
		var next []refKey
		var nextMtx sync.Mutex

		grp, gctx := errgroup.WithContext(ctx)
		grp.SetLimit(r.opts.Readers)

		for start := 0; start < len(frontier); start += markChunk {
			chunk := frontier[start:min(start+markChunk, len(frontier))]

			grp.Go(func() error {
				var children []refKey
				var found []liveRef

				reads, err := r.readChunk(gctx, chunk)
				if err != nil {
					return err
				}

				for _, read := range reads {
					found = append(found, liveRef{key: read.key, size: uint64(len(read.obj.GetFile().GetInline()))})
					children = appendChildren(children, &found, read.obj)
				}

				if err := live.add(found); err != nil {
					return err
				}

				fresh := claim(children)
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

const (
	// markGap is how far apart two records may sit and still be worth
	// fetching together; markSpan bounds what one such read covers.
	markGap  = 64 << 10
	markSpan = 8 << 20
)

// markRead is one object the mark read, with the key it was asked for.
type markRead struct {
	key refKey
	obj *proto.Object
}

// placed is a record's position, kept so a run of them can be read at once.
type placed struct {
	key refKey
	a   *archive
	rec *IndexRecord
}

// readChunk reads the objects of the chunk, one ranged read per run of
// records that sit close together in the same archive. An object that is
// gone is skipped: a mark cannot mend it, and the sweep will not drop what
// it never saw.
func (r *gcRun) readChunk(ctx context.Context, keys []refKey) ([]markRead, error) {
	byArchive := make(map[string][]placed)

	for _, key := range keys {
		ref := &proto.Ref{Hash: key[:]}

		a, rec, err := r.ps.indexLocation(ctx, ref)
		if err != nil {
			return nil, errors.Wrapf(err, "locating %x", key)
		}

		if rec == nil {
			r.ps.logger.Warn("reachable object is missing", "ref", fmt.Sprintf("%x", key))
			continue
		}

		byArchive[a.name] = append(byArchive[a.name], placed{key: key, a: a, rec: rec})
	}

	reads := make([]markRead, 0, len(keys))

	for _, records := range byArchive {
		sort.Slice(records, func(i, j int) bool { return records[i].rec.Offset < records[j].rec.Offset })

		for start := 0; start < len(records); {
			end, span := spanOf(records, start)

			read, err := r.readSpan(ctx, records[start:end], span)
			if err != nil {
				return nil, err
			}

			reads = append(reads, read...)
			start = end
		}
	}

	return reads, nil
}

// spanOf extends the run starting at start for as long as the records stay
// close together, and returns where it ends and how many bytes it covers.
func spanOf(records []placed, start int) (int, int64) {
	from := int64(records[start].rec.Offset)
	span := int64(records[start].rec.Length)

	end := start + 1
	for end < len(records) {
		previous := int64(records[end-1].rec.Offset) + int64(records[end-1].rec.Length)
		grown := int64(records[end].rec.Offset) + int64(records[end].rec.Length) - from

		if int64(records[end].rec.Offset)-previous > markGap || grown > markSpan {
			break
		}

		span = grown
		end++
	}

	return end, span
}

// readSpan reads one run of records in a single read and decodes each one.
func (r *gcRun) readSpan(ctx context.Context, records []placed, span int64) ([]markRead, error) {
	a := records[0].a
	from := int64(records[0].rec.Offset)

	// an archive still being written is read the ordinary way, which
	// finalizes it first
	if len(records) == 1 || !a.readOnlyNow() {
		reads := make([]markRead, 0, len(records))

		for _, record := range records {
			obj, err := r.ps.Get(ctx, &proto.Ref{Hash: record.key[:]})
			if errors.Is(err, backup.ErrNotFound) {
				r.ps.logger.Warn("reachable object is missing", "ref", fmt.Sprintf("%x", record.key))
				continue
			}

			if err != nil {
				return nil, errors.Wrapf(err, "reading %x", record.key)
			}

			reads = append(reads, markRead{key: record.key, obj: obj})
		}

		return reads, nil
	}

	buf, _, err := a.readSpan(from, span)
	if err != nil {
		return nil, errors.Wrapf(err, "reading %d bytes at %d of %s", span, from, a.name)
	}

	reads := make([]markRead, 0, len(records))

	for _, record := range records {
		at := int64(record.rec.Offset) - from

		obj, err := a.objectFromRecord(ctx, &proto.Ref{Hash: record.key[:]}, record.rec, buf[at:at+int64(record.rec.Length)], 0)
		if err != nil {
			return nil, errors.Wrapf(err, "reading %x", record.key)
		}

		reads = append(reads, markRead{key: record.key, obj: obj})
	}

	return reads, nil
}

// appendChildren adds the object's metadata children to children and its
// blob parts straight to live.
func appendChildren(children []refKey, live *[]liveRef, obj *proto.Object) []refKey {
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
				*live = append(*live, liveRef{key: keyOf(part.Ref.Hash), size: part.GetLength()})
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
	ga      *gcArchive
	pos     int
	rec     *IndexRecord
	scanner indexScanner
}

type mergeHeap []mergeHead

func (h mergeHeap) Len() int { return len(h) }
func (h mergeHeap) Less(i, j int) bool {
	return bytes.Compare(h[i].rec.Sum[:], h[j].rec.Sum[:]) < 0
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

	var counted refKey
	err := r.scan(live, func(ga *gcArchive, pos int, rec *IndexRecord, owners []Attribution, size uint64) {
		ga.cur.Set(uint(pos))
		marked++

		// a second copy of an object adds to what it takes up, not to the
		// content it carries
		first := rec.Sum != counted
		counted = rec.Sum

		// owners come in group order, so the first of each group is the
		// set that group carries the object in
		group := int64(0)
		for i, owner := range owners {
			if owner.Set == 0 || (i > 0 && owner.Group == group) {
				continue
			}

			group = owner.Group
			r.setBytes[owner.Set] += uint64(rec.Length)

			if first {
				r.setDeduplicated[owner.Set] += size
			}
		}

		r.share(owners, uint64(rec.Length))
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

// share counts a record for every set that reached it, and for the one
// set of a group that alone reached it.
func (r *gcRun) share(owners []Attribution, length uint64) {
	for start := 0; start < len(owners); {
		end, reached := start, 0
		for ; end < len(owners) && owners[end].Group == owners[start].Group; end++ {
			if owners[end].Set != 0 {
				r.setAlone[owners[end].Set] += length
				reached++
			}
		}

		if reached == 1 {
			for _, owner := range owners[start:end] {
				if owner.Set != 0 {
					r.setExclusive[owner.Set] += length
				}
			}
		}

		start = end
	}
}

// scan walks every index record of the snapshot in ref order alongside the
// sorted runs; hit sees the records the runs name, with everything that
// reached them, and each sees every record.
func (r *gcRun) scan(runs *liveRuns, hit func(ga *gcArchive, pos int, rec *IndexRecord, owners []Attribution, size uint64), each func(sum refKey)) error {
	it, err := runs.iterator()
	if err != nil {
		return err
	}
	defer it.close()

	var h mergeHeap
	defer func() {
		for _, head := range h {
			_ = head.scanner.close()
		}
	}()

	for _, ga := range r.order {
		scanner, err := ga.a.scanIndex()
		if err != nil {
			return errors.Wrapf(err, "reading index of %s", ga.a.name)
		}

		record, err := scanner.next()
		if err != nil {
			_ = scanner.close()

			return errors.Wrapf(err, "reading index of %s", ga.a.name)
		}

		if record == nil {
			_ = scanner.close()

			continue
		}

		h = append(h, mergeHead{ga: ga, rec: record, scanner: scanner})
	}
	heap.Init(&h)

	for h.Len() > 0 {
		top := h[0]
		sum := top.rec.Sum

		if owners, size, ok := it.at(sum); ok {
			hit(top.ga, top.pos, top.rec, owners, size)
		}

		if each != nil {
			each(sum)
		}

		record, err := top.scanner.next()
		if err != nil {
			return errors.Wrapf(err, "reading index of %s", top.ga.a.name)
		}

		if record == nil {
			_ = top.scanner.close()
			heap.Pop(&h)

			continue
		}

		top.pos++
		top.rec = record
		h[0] = top
		heap.Fix(&h, 0)
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
	runs.begin("erased-", Attribution{})
	defer runs.close()

	if err := r.markBatch(ctx, r.visited.claimUnreached(roots), r.visited.claimUnreached, runs); err != nil {
		return err
	}

	return r.scan(runs, func(ga *gcArchive, _ int, _ *IndexRecord, _ []Attribution, _ uint64) { ga.erase = true }, nil)
}

func (r *gcRun) writeResults() error {
	for _, ga := range r.order {
		next := &gcFile{Generation: r.gen, Snapshot: r.snapshot, Current: ga.cur, Erase: ga.erase}

		if ga.prev != nil && ga.prev.Generation == r.gen-1 {
			next.Previous = ga.prev.Current
		}

		ga.droppable = 0

		err := scanArchive(ga.a, func(pos int, rec *IndexRecord) error {
			if r.droppable(ga, next, pos, rec) {
				ga.droppable += uint64(rec.Length)
			}

			if ga.cur.Test(uint(pos)) {
				return nil
			}

			if r.uncondemned(ga, rec) {
				newestAt(r.condemned, keyOf(rec.Sum[:]), ga.a.version(*rec))
			}

			next.DeadObjects++
			next.DeadBytes += uint64(rec.Length)
			next.Dead = append(next.Dead, prefixOf(rec.Sum[:]))

			return nil
		}, nil, nil)
		if err != nil {
			return errors.Wrapf(err, "reading index of %s", ga.a.name)
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

	reason, err := r.ps.halted()
	if err != nil {
		return fmt.Sprintf("reading whether collections are halted: %s", err)
	}

	return reason
}

// sweep rewrites the archives whose dead share or age selects them, dropping
// objects unmarked in two consecutive generations.
func (r *gcRun) sweep(ctx context.Context, report *CollectReport) error {
	group := &compactionGroup{keep: r.keep, marked: r.marked}

	for _, ga := range r.order {
		if !r.selected(ga) {
			continue
		}

		group.candidates = append(group.candidates, ga.a)
		group.total += ga.a.size
	}

	if len(group.candidates) == 0 {
		return nil
	}

	r.ps.logger.Info("gc sweeping archives", "count", len(group.candidates))

	if err := r.ps.compactGroup(ctx, group); err != nil {
		return err
	}

	report.Swept += len(group.candidates)
	report.ReclaimedObjects += group.droppedObjects
	report.ReclaimedBytes += group.droppedBytes

	return nil
}

// anySelected reports whether a sweep would rewrite any archive.
func (r *gcRun) anySelected() bool {
	for _, ga := range r.order {
		if r.selected(ga) {
			return true
		}
	}

	return false
}

// selected reports whether an archive is worth rewriting this generation.
func (r *gcRun) selected(ga *gcArchive) bool {
	if ga.next.Previous == nil || ga.bytes == 0 {
		return false
	}

	if ga.droppable == 0 {
		return false
	}

	if ga.next.Erase || float64(ga.droppable)/float64(ga.bytes) >= r.opts.DeadRatio {
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

	pos, rec, err := ga.lookup.record(hdr.Ref.Hash)
	if err != nil {
		r.ps.logger.Warn("looking an object up in its index failed, keeping it", "archive", candidate.name, "ref", fmt.Sprintf("%x", hdr.Ref.Hash), "err", err)

		return true
	}

	if pos < 0 || !r.droppable(ga, ga.next, pos, rec) {
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

	pos, err := ga.lookup.position(loc.Record.Sum[:])
	if err != nil {
		r.ps.logger.Warn("looking an object up in its index failed, treating it as reachable", "archive", loc.Archive, "err", err)

		return true
	}

	return pos < 0 || ga.cur.Test(uint(pos))
}

// droppable reports whether a rewrite may drop the record at pos. A copy
// goes once two generations in a row found it unreachable and a tombstone
// newer than it was stored before the previous generation's horizon;
// what an un-tombstone takes back is marked. A tombstone goes once spent,
// or once its target is gone, the previous horizon covers it and every
// session of that horizon has ended.
func (r *gcRun) droppable(ga *gcArchive, g *gcFile, pos int, rec *IndexRecord) bool {
	if proto.ObjectType(rec.Type) == proto.ObjectType_TOMBSTONE {
		if r.spent[recordAt{ga.a.name, pos}] {
			return true
		}

		return g.dead(pos) && r.horizonEnded && r.condemnedAt[ga.a.version(*rec).Time.UnixNano()]
	}

	if !g.dead(pos) {
		return false
	}

	target := keyOf(rec.Sum[:])

	tomb, ok := r.condemning[target]

	return ok && ga.a.version(*rec).Before(tomb)
}

// uncondemned reports whether an unreachable record still needs a
// tombstone: none newer than it stands.
func (r *gcRun) uncondemned(ga *gcArchive, rec *IndexRecord) bool {
	if proto.ObjectType(rec.Type) == proto.ObjectType_TOMBSTONE {
		return false
	}

	tomb, ok := r.newestTomb[keyOf(rec.Sum[:])]

	return !ok || !ga.a.version(*rec).Before(tomb)
}

func newestAt(versions map[refKey]Version, key refKey, v Version) {
	if old, ok := versions[key]; !ok || old.Before(v) {
		versions[key] = v
	}
}

// condemnAttempts bounds how often a collection writes a tombstone that
// came out no newer than the copies it condemns.
const condemnAttempts = 3

// condemn stores a tombstone for every unreachable object that has none
// standing, in archives of their own, each newer than every copy it
// condemns: a session that begins after the seal must find those copies
// absent.
func (r *gcRun) condemn(ctx context.Context) error {
	pending := r.condemned

	for attempt := 1; len(pending) > 0; attempt++ {
		if attempt > condemnAttempts {
			return errors.Errorf("%d tombstones came out no newer than the copies they condemn", len(pending))
		}

		for target := range pending {
			if err := r.ps.Delete(ctx, &proto.Ref{Hash: append([]byte(nil), target[:]...)}); err != nil {
				return errors.Wrap(err, "condemning an unreachable object")
			}
		}

		if err := r.ps.Flush(); err != nil {
			return err
		}

		stale, err := r.notNewer(pending)
		if err != nil {
			return err
		}

		pending = stale
	}

	return nil
}

// notNewer returns the targets whose newest tombstone is not newer than
// their newest copy.
func (r *gcRun) notNewer(targets map[refKey]Version) (map[refKey]Version, error) {
	keys := make([]refKey, 0, len(targets))
	tombs := make([]*proto.Ref, 0, len(targets))

	for target := range targets {
		keys = append(keys, target)
		tombs = append(tombs, proto.TombstoneRef(&proto.Ref{Hash: append([]byte(nil), target[:]...)}))
	}

	found, err := r.ps.index.LocateCopies(tombs, Scope{})
	if err != nil {
		return nil, err
	}

	stale := make(map[refKey]Version)

	for i, target := range keys {
		newest, err := r.ps.newest(found[string(tombs[i].Hash)])
		if err != nil {
			return nil, err
		}

		if newest == nil || !targets[target].Before(*newest) {
			stale[target] = targets[target]
		}
	}

	return stale, nil
}

// horizon lists, once the tombstones are stored, the versions of the
// committed archives there are and the sessions that have begun and not
// ended.
func (r *gcRun) horizon() ([]time.Time, []string, error) {
	if err := r.ps.refreshArchives(); err != nil {
		return nil, nil, errors.Wrap(err, "catching up with the storage's archives")
	}

	seen := make(map[int64]bool)
	var condemned []time.Time

	if r.prev != nil {
		for _, t := range r.prev.Condemned {
			if r.tombTimes[t.UnixNano()] && !seen[t.UnixNano()] {
				seen[t.UnixNano()] = true
				condemned = append(condemned, t)
			}
		}
	}

	r.ps.mtx.RLock()
	for _, a := range r.ps.archives {
		a.mtx.RLock()
		created, committed := a.created, a.readOnly && a.state == ArchiveCommitted
		a.mtx.RUnlock()

		if committed && !created.IsZero() && !seen[created.UnixNano()] {
			seen[created.UnixNano()] = true
			condemned = append(condemned, created)
		}
	}
	r.ps.mtx.RUnlock()

	sort.Slice(condemned, func(i, j int) bool { return condemned[i].Before(condemned[j]) })

	live, err := r.liveSessions()
	if err != nil {
		return nil, nil, err
	}

	horizon := make([]string, 0, len(live))
	for id := range live {
		horizon = append(horizon, id)
	}

	sort.Strings(horizon)

	return condemned, horizon, nil
}

// liveSessions are the sessions that began and have not ended. A session
// the index still holds is live even with an end marker, since its commit
// may not have reached the index yet.
func (r *gcRun) liveSessions() (map[string]bool, error) {
	begun, err := r.ps.markerIDs(SessionBeginExt)
	if err != nil {
		return nil, err
	}

	ended, err := r.ps.markerIDs(SessionEndExt)
	if err != nil {
		return nil, err
	}

	indexed, err := r.ps.index.ListSessions()
	if err != nil {
		return nil, err
	}

	live := make(map[string]bool, len(begun)+len(indexed))
	for id := range begun {
		if !ended[id] {
			live[id] = true
		}
	}

	for _, s := range indexed {
		live[s.ID] = true
	}

	return live, nil
}

func prefixOf(hash []byte) uint64 {
	var prefix uint64
	for i := 0; i < 8 && i < len(hash); i++ {
		prefix = prefix<<8 | uint64(hash[i])
	}

	return prefix
}
