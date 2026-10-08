package backup

import (
	"context"
	"testing"

	"github.com/gobackio/goback/proto"

	"github.com/stretchr/testify/require"
)

// walkingStore serves a fixed set of file objects to a typed walk.
type walkingStore struct {
	*memStore
	files []*proto.Object
}

func (w *walkingStore) Walk(_ context.Context, _ bool, typ proto.ObjectType, fn ObjectReceiver) error {
	if typ != proto.ObjectType_FILE {
		return nil
	}

	for _, obj := range w.files {
		if err := fn(obj); err != nil {
			return err
		}
	}

	return nil
}

// placingIndex answers with the paths it was built with and records what
// it was told to read again.
type placingIndex struct {
	at      []FilePath
	damaged map[int64][]string
	rescan  []int64
	lost    []FilePath
}

func (p *placingIndex) PathsOfFiles(_ context.Context, refs []*proto.Ref) ([]FilePath, error) {
	want := make(map[string]bool, len(refs))
	for _, ref := range refs {
		want[string(ref.GetHash())] = true
	}

	var out []FilePath
	for _, at := range p.at {
		if want[string(at.Ref.GetHash())] {
			out = append(out, at)
		}
	}

	return out, nil
}

func (p *placingIndex) MarkDamaged(_ context.Context, set int64, paths []string) error {
	if p.damaged == nil {
		p.damaged = map[int64][]string{}
	}

	p.damaged[set] = append(p.damaged[set], paths...)

	return nil
}

func (p *placingIndex) MarkLost(_ context.Context, versions []FilePath) error {
	p.lost = append(p.lost, versions...)

	return nil
}

func (p *placingIndex) MarkRescan(_ context.Context, set int64) error {
	p.rescan = append(p.rescan, set)

	return nil
}

// the ids of the sets the tests place damage in
const (
	world int64 = 1
	logs  int64 = 2
)

func partedFile(parts ...*proto.Ref) *proto.Object {
	file := &proto.File{}

	var offset uint64
	for _, ref := range parts {
		file.Parts = append(file.Parts, &proto.FilePart{Offset: offset, Length: 16, Ref: ref})
		offset += 16
	}

	return proto.NewObject(file)
}

func TestDamageIsPlacedAtThePathThatHeldIt(t *testing.T) {
	ctx := context.Background()

	lost := testRef("lost")
	holder := partedFile(testRef("kept"), lost)
	other := partedFile(testRef("elsewhere"))

	store := &walkingStore{memStore: newMemStore(), files: []*proto.Object{holder, other}}
	idx := &placingIndex{at: []FilePath{
		{Ref: holder.Ref(), SetID: world, Set: "world", Path: "world/region/r.0.0.mca", Open: true},
		{Ref: other.Ref(), SetID: world, Set: "world", Path: "world/level.dat", Open: true},
	}}

	report, err := ReportDamage(ctx, store, idx, []int64{world}, []*proto.Ref{lost})
	require.NoError(t, err)
	require.Equal(t, map[int64][]string{world: {"world/region/r.0.0.mca"}}, report.Paths)
	require.Equal(t, map[int64][]string{world: {"world/region/r.0.0.mca"}}, idx.damaged)
	require.Empty(t, report.Rescan, "a placed loss needs no full re-read")
	require.Empty(t, idx.rescan)
}

func TestALostFileObjectIsPlacedByItsOwnRef(t *testing.T) {
	ctx := context.Background()

	gone := partedFile(testRef("a"), testRef("b"))
	store := &walkingStore{memStore: newMemStore()}
	idx := &placingIndex{at: []FilePath{{Ref: gone.Ref(), SetID: world, Set: "world", Path: "world/level.dat", Open: true}}}

	report, err := ReportDamage(ctx, store, idx, []int64{world}, []*proto.Ref{gone.Ref()})
	require.NoError(t, err)
	require.Equal(t, map[int64][]string{world: {"world/level.dat"}}, report.Paths)
	require.Empty(t, report.Rescan)
}

func TestAnUnplaceableLossAsksForAFullReRead(t *testing.T) {
	ctx := context.Background()

	lost := testRef("a tree nobody can walk to")
	store := &walkingStore{memStore: newMemStore()}
	idx := &placingIndex{}

	report, err := ReportDamage(ctx, store, idx, []int64{world, logs}, []*proto.Ref{lost})
	require.NoError(t, err)
	require.Empty(t, report.Paths)
	require.Equal(t, []int64{world, logs}, report.Rescan)
	require.Equal(t, []int64{world, logs}, idx.rescan)
	require.Len(t, report.Unplaced, 1)
}

func TestNoLossReportsNothing(t *testing.T) {
	report, err := ReportDamage(context.Background(), &walkingStore{memStore: newMemStore()}, &placingIndex{}, []int64{world}, nil)
	require.NoError(t, err)
	require.Empty(t, report.Paths)
	require.Empty(t, report.Rescan)
}

func TestAClosedVersionIsLostRatherThanReadAgain(t *testing.T) {
	ctx := context.Background()

	lost := testRef("lost")
	old := partedFile(lost)
	current := partedFile(testRef("fresh"))

	store := &walkingStore{memStore: newMemStore(), files: []*proto.Object{old, current}}
	idx := &placingIndex{at: []FilePath{
		{Ref: old.Ref(), SetID: world, Set: "world", Path: "world/level.dat"},
		{Ref: current.Ref(), SetID: world, Set: "world", Path: "world/level.dat", Open: true},
	}}

	report, err := ReportDamage(ctx, store, idx, []int64{world}, []*proto.Ref{lost})
	require.NoError(t, err)
	require.Empty(t, report.Paths, "no run can produce a version the newest commit no longer points at")
	require.Empty(t, idx.damaged)
	require.Len(t, report.Lost, 1)
	require.Equal(t, "world/level.dat", report.Lost[0].Path)
	require.Equal(t, report.Lost, idx.lost)
	require.Empty(t, report.Rescan, "the loss was placed, it just cannot be undone")
}

func TestALossInASplitIsPlacedAtTheFileThatSplitsIt(t *testing.T) {
	ctx := context.Background()

	lost := testRef("lost")
	split := partedFile(testRef("kept"), lost)
	big := proto.NewObject(&proto.File{Splits: []*proto.Ref{partedFile(testRef("first")).Ref(), split.Ref()}})

	// the split comes before the file that names it
	store := &walkingStore{memStore: newMemStore(), files: []*proto.Object{split, big}}
	idx := &placingIndex{at: []FilePath{{Ref: big.Ref(), SetID: world, Set: "world", Path: "world/region/r.0.0.mca", Open: true}}}

	report, err := ReportDamage(ctx, store, idx, []int64{world}, []*proto.Ref{lost})
	require.NoError(t, err)
	require.Equal(t, map[int64][]string{world: {"world/region/r.0.0.mca"}}, report.Paths)
	require.Empty(t, report.Rescan)
}
