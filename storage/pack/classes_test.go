package pack

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func TestAnAgeClassIsTheDayThenWeekThenMonthThenYear(t *testing.T) {
	now := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC) // a Monday
	day := func(y int, m time.Month, d int) int64 { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() }

	for _, c := range []struct {
		at   time.Time
		want ageClass
	}{
		{now, ageClass{spanDay, day(2026, 10, 5)}},
		{time.Date(2026, 9, 29, 23, 0, 0, 0, time.UTC), ageClass{spanDay, day(2026, 9, 29)}},
		{time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC), ageClass{spanWeek, day(2026, 9, 28)}},
		{time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC), ageClass{spanWeek, day(2026, 9, 7)}},
		{time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC), ageClass{spanMonth, day(2026, 9, 1)}},
		{time.Date(2025, 11, 30, 12, 0, 0, 0, time.UTC), ageClass{spanMonth, day(2025, 11, 1)}},
		{time.Date(2025, 10, 31, 12, 0, 0, 0, time.UTC), ageClass{spanYear, day(2025, 1, 1)}},
	} {
		require.Equal(t, c.want, classOf(c.at, now), "%s", c.at)
	}
}

func TestASmallClassJoinsItsOwnersNextOlderOne(t *testing.T) {
	c := newClasses(nil)
	set := Attribution{Group: 1, Set: 7}
	other := Attribution{Group: 1, Set: 9}

	today := c.id(outputClass{set, ageClass{spanDay, 300}})
	week := c.id(outputClass{set, ageClass{spanWeek, 200}})
	month := c.id(outputClass{set, ageClass{spanMonth, 100}})
	year := c.id(outputClass{set, ageClass{spanYear, 0}})
	alone := c.id(outputClass{other, ageClass{spanDay, 300}})

	into := c.fold(map[int32]uint64{today: 10, week: 50, month: 200, year: 10, alone: 10}, 100)

	require.Equal(t, month, into[today], "the day and the week add up to too little, so they join the month")
	require.Equal(t, month, into[week])
	require.Equal(t, month, into[month])
	require.Equal(t, month, into[year], "the oldest, still small, joins the newer class")
	require.Equal(t, alone, into[alone], "an owner whose only class is small keeps it")
}

func TestAnObjectIsFiledUnderTheSetThatAloneReachedItInTheFirstGroup(t *testing.T) {
	require.Equal(t, Attribution{}, ownerOf(nil))
	require.Equal(t, Attribution{Group: 1, Set: 7}, ownerOf([]Attribution{{Group: 1}, {Group: 1, Set: 7}, {Group: 2, Set: 8}}))
	require.Equal(t, Attribution{Group: 1}, ownerOf([]Attribution{{Group: 1, Set: 7}, {Group: 1, Set: 9}, {Group: 2, Set: 8}}))
}

func TestASweepWritesEachSetsSurvivorsIntoArchivesOfTheirOwn(t *testing.T) {
	t.Run("collected", func(t *testing.T) { testSetsApart(t, false) })
	t.Run("handed off", func(t *testing.T) { testSetsApart(t, true) })
}

func testSetsApart(t *testing.T, handoff bool) {
	store, err := NewPackStorage(
		WithArchiveStorage(newLocal(t.TempDir())),
		WithArchiveIndex(NewInMemoryIndex()),
		WithMaxSize(256*1024),
		WithCompaction(CompactionConfig{Small: 1}),
	)
	require.NoError(t, err)
	require.NoError(t, store.Open())
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	ctx := context.Background()

	blobs := func(n int) []*proto.Object {
		var out []*proto.Object
		for range n {
			data := make([]byte, 4096)
			_, _ = rand.Read(data)
			out = append(out, proto.NewObject(&proto.Blob{Data: data}))
		}

		return out
	}

	setOf := func(name string, blobs []*proto.Object) (*proto.Object, []*proto.Object) {
		var parts []*proto.FilePart
		for i, b := range blobs {
			parts = append(parts, &proto.FilePart{Ref: b.Ref(), Offset: uint64(i) * 4096, Length: 4096})
		}

		file := proto.NewObject(&proto.File{Parts: parts})
		tree := treeOf([]*proto.Object{file})
		commit := proto.NewObject(&proto.Commit{Tree: tree.Ref(), Timestamp: 1, BackupSet: name})

		return commit, append(blobs, file, tree, commit)
	}

	mineCommit, mine := setOf("mine", blobs(60))
	yoursCommit, yours := setOf("yours", blobs(60))
	putAll(t, store, append(append(append([]*proto.Object{}, mine...), yours...), blobs(120)...))

	opts := gcOptions(t, 0)
	opts.Owner = func(root []byte) Attribution {
		switch {
		case bytes.Equal(root, mineCommit.Ref().Hash):
			return Attribution{Group: 1, Set: 7}
		case bytes.Equal(root, yoursCommit.Ref().Hash):
			return Attribution{Group: 1, Set: 9}
		}

		return Attribution{}
	}

	_, err = store.Collect(ctx, opts)
	require.NoError(t, err)

	opts.Now = opts.Now.Add(48 * time.Hour)
	opts.TempDir = t.TempDir()
	opts.Handoff = handoff
	report, err := store.Collect(ctx, opts)
	require.NoError(t, err)

	if handoff {
		rewritten, err := store.RewritePlan(ctx)
		require.NoError(t, err)
		require.Greater(t, rewritten.Swept, 1)
	} else {
		require.Greater(t, report.Swept, 1)
	}

	owner := make(map[string]string)
	for _, o := range mine {
		owner[string(o.Ref().Hash)] = "mine"
	}
	for _, o := range yours {
		owner[string(o.Ref().Hash)] = "yours"
	}

	store.mtx.RLock()
	defer store.mtx.RUnlock()

	for _, a := range store.archives {
		idx, err := a.getIndex()
		require.NoError(t, err)

		held := make(map[string]bool)
		for _, rec := range idx {
			if o := owner[string(rec.Sum[:])]; o != "" {
				held[o] = true
			}
		}

		require.LessOrEqual(t, len(held), 1, "archive %s holds objects of %v", a.name, held)
	}

	requireStored(t, store, mine, true)
	requireStored(t, store, yours, true)
}

func TestAChunkFoldsTheClassesBySizesWithinIt(t *testing.T) {
	c := newClasses(nil)
	set := Attribution{Group: 1, Set: 7}
	day := c.id(outputClass{set, ageClass{spanDay, 300}})
	week := c.id(outputClass{set, ageClass{spanWeek, 200}})

	first, second := &archive{name: "first"}, &archive{name: "second"}
	held := map[string]*classed{
		"first":  {Class: []int32{day, week}, Bytes: map[int32]uint64{day: 80, week: 80}},
		"second": {Class: []int32{day, week}, Bytes: map[int32]uint64{day: 30, week: 30}},
	}
	of := func(a *archive) *classed { return held[a.name] }

	alone := chunkClasses(c, 100, []*archive{second}, of)
	require.Equal(t, alone(second, 0), alone(second, 1), "too little of either in the chunk, so they are written together")

	together := chunkClasses(c, 100, []*archive{first, second}, of)
	require.NotEqual(t, together(first, 0), together(first, 1), "enough of each in the chunk to write them apart")
	require.Equal(t, int32(-1), together(&archive{name: "unclassified"}, 0))
}
