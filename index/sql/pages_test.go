package sql

import (
	"fmt"
	"testing"
	"time"

	"github.com/twcclan/goback/index/sql/ent/commitrow"
	"github.com/twcclan/goback/proto"

	"github.com/stretchr/testify/require"
)

func TestCommitDetailsBeforeKeepsInstantsWhole(t *testing.T) {
	l := newLocalIndex(t)

	// the index stamps receipts strictly increasing, but several writers
	// sharing a database can still land commits on one instant
	received := []int64{5e9, 4e9, 4e9, 4e9, 3e9, 2e9}
	for i, at := range received {
		ref := l.commit("a", "world.dat", int64(i), int64(10-i)*1e9, fmt.Sprint("version ", i))
		require.NoError(t, l.x.client.CommitRow.Update().Where(commitrow.Ref(ref.Hash)).SetReceivedAt(time.Unix(0, at).UTC()).Exec(l.ctx))
	}

	all, err := l.x.CommitDetailsBefore(l.ctx, "a", time.Time{}, 0)
	require.NoError(t, err)
	require.Len(t, all, len(received))

	var (
		pages  [][]int64
		seen   = map[string]bool{}
		before time.Time
	)

	for range len(received) + 1 {
		page, err := l.x.CommitDetailsBefore(l.ctx, "a", before, 2)
		require.NoError(t, err)

		if len(page) == 0 {
			break
		}

		var stamps []int64
		for _, c := range page {
			require.False(t, seen[string(c.Ref.Hash)], "a commit shows up once")
			seen[string(c.Ref.Hash)] = true
			stamps = append(stamps, c.Commit.ReceivedAtNs)
		}

		pages = append(pages, stamps)
		before = time.Unix(0, page[len(page)-1].Commit.ReceivedAtNs)
	}

	require.Equal(t, [][]int64{{5e9, 4e9, 4e9, 4e9}, {3e9, 2e9}}, pages, "a page runs on through the instant it ends at")
	require.Len(t, seen, len(received))
}

func TestVersionsBeforePagesNewestFirst(t *testing.T) {
	l := newLocalIndex(t)

	for i := range 3 {
		l.commit("a", "world.dat", int64(i), int64(i+1)*1e9, fmt.Sprint("version ", i))
	}

	page, err := l.x.VersionsBefore(l.ctx, "a", "world.dat", time.Time{}, 2)
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.True(t, page[0].From.Equal(time.Unix(3, 0)))
	require.True(t, page[1].From.Equal(time.Unix(2, 0)))

	page, err = l.x.VersionsBefore(l.ctx, "a", "world.dat", page[1].From, 2)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.True(t, page[0].From.Equal(time.Unix(1, 0)))
	require.EqualValues(t, 0, page[0].Node.Stat.MtimeNs)

	page, err = l.x.VersionsBefore(l.ctx, "a", "world.dat", page[0].From, 2)
	require.NoError(t, err)
	require.Empty(t, page)
}

func TestReadDirAfterPagesByteByByte(t *testing.T) {
	f := newFixture(t)

	f.commit("world", f.tree(
		f.file("a.txt", "a"), f.file("B.txt", "b"), f.file("b.txt", "b"),
		f.dir("c", f.file("x", "x")), f.dir("Z", f.file("y", "y")),
	), false)

	all, err := f.x.ReadDirAfter(f.ctx, "world", "", f.clock, "", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"B.txt", "Z", "a.txt", "b.txt", "c"}, names(all))

	var (
		pages [][]string
		after string
	)

	for range len(all) + 1 {
		page, err := f.x.ReadDirAfter(f.ctx, "world", "", f.clock, after, 2)
		require.NoError(t, err)

		if len(page) == 0 {
			break
		}

		pages = append(pages, names(page))
		after = proto.JoinPath("", page[len(page)-1].Stat.Name)
	}

	require.Equal(t, [][]string{{"B.txt", "Z"}, {"a.txt", "b.txt"}, {"c"}}, pages)
	require.Equal(t, proto.NodeType_NODE_DIRECTORY, all[1].Stat.Type)
}

func TestListSetsAfterPagesByName(t *testing.T) {
	l := newLocalIndex(t)

	for _, set := range []string{"c", "a", "b"} {
		l.commit(set, "world.dat", 1, 0, set)
	}

	page, err := l.x.ListSetsAfter(l.ctx, "", 2)
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.Equal(t, "a", page[0].Name)
	require.Equal(t, "b", page[1].Name)

	page, err = l.x.ListSetsAfter(l.ctx, page[1].Name, 2)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, "c", page[0].Name)
	require.EqualValues(t, 1, page[0].LogicalSize)
}

func names(nodes []*proto.TreeNode) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = string(n.Stat.Name)
	}

	return out
}

func TestTrashedCommitsListsTheTrashUntilItsTombstone(t *testing.T) {
	f := newFixture(t)

	a := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	b := f.commit("world", f.tree(f.file("a.txt", "two")), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "three")), false)

	require.NoError(t, f.x.DeleteCommit(f.ctx, a))
	deletedA := f.clock
	f.advance(48 * time.Hour)
	require.NoError(t, f.x.DeleteCommit(f.ctx, b))
	deletedB := f.clock

	trash, err := f.x.TrashedCommits(f.ctx, "world", time.Time{}, 0)
	require.NoError(t, err)
	require.Len(t, trash, 2)
	require.True(t, trash[0].Ref.Equal(b), "the newest deleted comes first")
	require.True(t, trash[1].Ref.Equal(a))
	require.Equal(t, deletedB.UnixNano(), trash[0].DeletedAtNs)
	require.Equal(t, deletedB.Add(14*24*time.Hour).UnixNano(), trash[0].ExpiresAtNs)
	require.Equal(t, "world", trash[0].Commit.BackupSet)

	n, err := f.x.Retire(f.ctx, deletedA.Add(14*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, n)

	trash, err = f.x.TrashedCommits(f.ctx, "world", time.Time{}, 0)
	require.NoError(t, err)
	require.Len(t, trash, 1, "a tombstoned commit leaves the trash")
	require.True(t, trash[0].Ref.Equal(b))

	require.NoError(t, f.x.UndeleteCommit(f.ctx, b))
	trash, err = f.x.TrashedCommits(f.ctx, "world", time.Time{}, 0)
	require.NoError(t, err)
	require.Empty(t, trash, "an undeleted commit is live again")

	trash, err = f.x.TrashedCommits(f.ctx, "unknown", time.Time{}, 0)
	require.NoError(t, err)
	require.Empty(t, trash)
}

func TestTrashedCommitsKeepsInstantsWhole(t *testing.T) {
	f := newFixture(t)

	deleted := []int64{5e9, 4e9, 4e9, 3e9}
	for i, at := range deleted {
		ref := f.commit("world", f.tree(f.file("a.txt", fmt.Sprint("version ", i))), false)
		f.advance(time.Hour)
		require.NoError(t, f.x.client.CommitRow.Update().Where(commitrow.Ref(ref.Hash)).SetDeletedAt(time.Unix(0, at).UTC()).Exec(f.ctx))
	}

	f.commit("world", f.tree(f.file("a.txt", "live")), false)

	var (
		pages  [][]int64
		before time.Time
	)

	for range len(deleted) + 1 {
		page, err := f.x.TrashedCommits(f.ctx, "world", before, 2)
		require.NoError(t, err)

		if len(page) == 0 {
			break
		}

		var stamps []int64
		for _, c := range page {
			stamps = append(stamps, c.DeletedAtNs)
		}

		pages = append(pages, stamps)
		before = time.Unix(0, page[len(page)-1].DeletedAtNs)
	}

	require.Equal(t, [][]int64{{5e9, 4e9, 4e9}, {3e9}}, pages, "a page runs on through the instant it ends at")
}
