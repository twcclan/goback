package sql

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gobackio/goback/backup"
	"github.com/gobackio/goback/backup/retention"
	"github.com/gobackio/goback/proto"

	"github.com/stretchr/testify/require"
)

// archivedStore files what is put to it into an open archive, which seal
// finishes under a fresh name.
type archivedStore struct {
	*memStore

	mu       sync.Mutex
	open     []*proto.Ref
	finished map[string][]*proto.Ref
	order    []string
	next     int
	// read names the finished archives WalkArchives walked
	read []string
}

func (s *archivedStore) Put(ctx context.Context, obj *proto.Object) error {
	if err := s.memStore.Put(ctx, obj); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.open = append(s.open, obj.Ref())

	return nil
}

// seal finishes the open archive and returns its name.
func (s *archivedStore) seal() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.finish(s.open)
}

// compact rewrites the named archives into a new one and returns its name.
func (s *archivedStore) compact(names ...string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var refs []*proto.Ref

	for _, name := range names {
		refs = append(refs, s.finished[name]...)
		delete(s.finished, name)
		s.order = slices.DeleteFunc(s.order, func(n string) bool { return n == name })
	}

	return s.finish(refs)
}

func (s *archivedStore) finish(refs []*proto.Ref) string {
	name := fmt.Sprintf("archive-%d", s.next)
	s.next++
	s.finished[name] = refs
	s.order = append(s.order, name)
	s.open = nil

	return name
}

func (s *archivedStore) WalkArchives(ctx context.Context, t proto.ObjectType, skip func(string) bool, fn backup.ObjectReceiver) ([]string, error) {
	s.mu.Lock()
	order := slices.Clone(s.order)
	archives := make(map[string][]*proto.Ref, len(order)+1)
	for _, name := range order {
		archives[name] = s.finished[name]
	}
	open := slices.Clone(s.open)
	s.mu.Unlock()

	visit := func(refs []*proto.Ref) error {
		for _, ref := range refs {
			s.memStore.mu.Lock()
			obj := s.memStore.objects[string(ref.Hash)]
			s.memStore.mu.Unlock()

			if obj != nil && obj.Type() == t {
				if err := fn(obj); err != nil {
					return err
				}
			}
		}

		return nil
	}

	for _, name := range order {
		if skip(name) {
			continue
		}

		if err := visit(archives[name]); err != nil {
			return nil, err
		}

		s.mu.Lock()
		s.read = append(s.read, name)
		s.mu.Unlock()
	}

	return order, visit(open)
}

// takeRead returns the archives walked since the last call.
func (s *archivedStore) takeRead() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	read := s.read
	s.read = nil

	return read
}

// newArchivedFixture is newFixture over an archivedStore, with a set whose
// retention keeps its newest commit.
func newArchivedFixture(t *testing.T) (*fixture, *archivedStore) {
	f := newFixture(t)
	store := &archivedStore{memStore: f.store, finished: map[string][]*proto.Ref{}}

	f.x = openIndex(t, store)
	f.x.Now = func() time.Time { return f.clock }
	require.NoError(t, f.x.SetDefaultPolicy(f.ctx, &retention.Policy{KeepLast: 1}))

	return f, store
}

// lostPin writes a pin to the store behind the index's back.
func (f *fixture) lostPin(store backup.ObjectStore, target *proto.Ref) *proto.Object {
	f.t.Helper()

	pin := proto.NewObject(&proto.Pin{Target: target, ReceivedAtNs: f.clock.UnixNano()})
	require.NoError(f.t, store.Put(f.ctx, pin))

	return pin
}

func (f *fixture) walkedArchives() []string {
	f.t.Helper()

	known, err := f.x.walkedArchives(f.ctx)
	require.NoError(f.t, err)

	var names []string
	for name := range known {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

func (f *fixture) retire(days int) int {
	f.t.Helper()

	n, err := f.x.Retire(f.ctx, f.clock.Add(time.Duration(days)*24*time.Hour))
	require.NoError(f.t, err)

	return n
}

func TestRetireReadsOnlyTheArchivesItHasNotWalked(t *testing.T) {
	f, store := newArchivedFixture(t)

	pinned := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "three")), false)

	f.lostPin(store, pinned)
	first := store.seal()
	second := store.seal()

	require.Equal(t, 1, f.retire(15), "the pinned commit stays")
	require.False(t, f.store.tombstoned(pinned))
	require.ElementsMatch(t, []string{first, second}, store.takeRead(), "the first run walks every archive")
	require.ElementsMatch(t, []string{first, second}, f.walkedArchives())

	pins, err := f.x.Pins(f.ctx)
	require.NoError(t, err)
	require.Len(t, pins, 1, "the index learns the pin it missed")

	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "four")), false)
	third := store.seal()
	rewritten := store.compact(first)

	require.Equal(t, 1, f.retire(30))
	require.ElementsMatch(t, []string{third, rewritten}, store.takeRead(), "the archives since, the rewritten pin among them")
	require.ElementsMatch(t, []string{second, third, rewritten}, f.walkedArchives(), "and forgets the one compaction retired")
	require.False(t, f.store.tombstoned(pinned))
}

func TestRetireFindsALostPinOnceTheWalkedArchivesAreForgotten(t *testing.T) {
	f, store := newArchivedFixture(t)

	pinned := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)

	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "three")), false)

	_, err := f.pin(pinned)
	require.NoError(t, err)
	store.seal()

	require.Equal(t, 1, f.retire(15))
	require.NotEmpty(t, f.walkedArchives())

	// an index restored from a copy older than both the pin and the walk
	_, err = f.x.client.Pin.Delete().Exec(f.ctx)
	require.NoError(t, err)
	_, err = f.x.client.WalkedArchive.Delete().Exec(f.ctx)
	require.NoError(t, err)

	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "four")), false)

	require.Equal(t, 1, f.retire(30))
	require.False(t, f.store.tombstoned(pinned))

	pins, err := f.x.Pins(f.ctx)
	require.NoError(t, err)
	require.Len(t, pins, 1)
}

func TestRetireLeavesOutAPinUnpinnedInAnotherArchive(t *testing.T) {
	f, store := newArchivedFixture(t)

	unpinned := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)

	pin := f.lostPin(store, unpinned)
	withPin := store.seal()
	require.NoError(t, store.Delete(f.ctx, pin.Ref()))
	store.seal()

	require.Equal(t, 1, f.retire(15))
	require.True(t, f.store.tombstoned(unpinned))

	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "three")), false)
	rewritten := store.compact(withPin)

	require.Equal(t, 1, f.retire(30))
	require.Contains(t, store.takeRead(), rewritten)

	pins, err := f.x.Pins(f.ctx)
	require.NoError(t, err)
	require.Empty(t, pins, "the unpin outranks the pin wherever each is stored")
}

func TestReIndexForgetsTheWalkedArchives(t *testing.T) {
	f, store := newArchivedFixture(t)

	a := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)
	f.lostPin(store, a)
	store.seal()

	require.Zero(t, f.retire(15))
	require.NotEmpty(t, f.walkedArchives())

	require.NoError(t, reindexErr(f.x.ReIndex(f.ctx)))
	require.Empty(t, f.walkedArchives())
}
