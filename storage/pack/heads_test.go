package pack

import (
	"testing"
	"time"

	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func TestASetsHeadIsItsLatestCompleteCommit(t *testing.T) {
	store := newGCStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })

	tree := makeTestData(t, 1)[0]
	putAll(t, store, []*proto.Object{tree})

	commit := func(set uint64, received int64, partial bool) *proto.Object {
		obj := proto.NewObject(&proto.Commit{Tree: tree.Ref(), BackupSet: "world", Partial: partial})
		obj.Stamp(set, time.Unix(0, received))
		putAll(t, store, []*proto.Object{obj})

		return obj
	}

	first := commit(1, 10, false)
	other := commit(2, 15, false)
	commit(1, 20, true)
	commit(1, 5, false)

	heads, err := store.Heads()
	require.NoError(t, err)
	require.ElementsMatch(t, []Head{
		{SetID: 1, Set: "world", Commit: first.Ref().Hash, ReceivedAtNs: 10},
		{SetID: 2, Set: "world", Commit: other.Ref().Hash, ReceivedAtNs: 15},
	}, heads)

	latest := commit(1, 30, false)

	heads, err = store.Heads()
	require.NoError(t, err)
	require.Contains(t, heads, Head{SetID: 1, Set: "world", Commit: latest.Ref().Hash, ReceivedAtNs: 30})
}
