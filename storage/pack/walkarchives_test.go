package pack

import (
	"context"
	"testing"

	"github.com/gobackio/goback/proto"

	"github.com/stretchr/testify/require"
)

func TestWalkArchivesReadsOnlyTheArchivesNotSkipped(t *testing.T) {
	store := newTestStore(t, t.TempDir(), WithMaxSize(64*1024*1024), WithCompaction(CompactionConfig{MinimumCandidates: 0, Workers: 1}))
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	target := makeTestData(t, 1)[0].Ref()
	first := proto.NewObject(&proto.Pin{Target: target, ReceivedAtNs: 1})
	second := proto.NewObject(&proto.Pin{Target: target, ReceivedAtNs: 2})

	walk := func(known []string) ([]string, []string) {
		t.Helper()

		skip := make(map[string]bool, len(known))
		for _, name := range known {
			skip[name] = true
		}

		var pins []string
		names, err := store.WalkArchives(ctx, proto.ObjectType_PIN, func(name string) bool { return skip[name] }, func(obj *proto.Object) error {
			pins = append(pins, obj.Ref().String())
			return nil
		})
		require.NoError(t, err)

		return names, pins
	}

	require.NoError(t, store.Put(ctx, first))
	require.NoError(t, store.Flush())

	known, pins := walk(nil)
	require.Len(t, known, 1)
	require.Equal(t, []string{first.Ref().String()}, pins)

	require.NoError(t, store.Put(ctx, second))
	require.NoError(t, store.Flush())

	names, pins := walk(known)
	require.Len(t, names, 2)
	require.Subset(t, names, known)
	require.Equal(t, []string{second.Ref().String()}, pins, "the skipped archive is not read")

	known = names
	require.NoError(t, store.Compact(context.Background()))

	names, pins = walk(known)
	require.NotEmpty(t, names)
	require.NotSubset(t, known, names, "a rewrite is a new archive")
	require.ElementsMatch(t, []string{first.Ref().String(), second.Ref().String()}, pins)
}
