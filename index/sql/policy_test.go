package sql

import (
	"testing"
	"time"

	"github.com/gobackio/goback/backup"
	"github.com/gobackio/goback/backup/retention"
	"github.com/gobackio/goback/backup/storekey"
	"github.com/gobackio/goback/index"
	"github.com/gobackio/goback/index/sql/ent/set"

	"github.com/stretchr/testify/require"
)

// policyState is what an operator's changes leave in an index, keyed so
// that two indexes of one store compare equal.
type policyState struct {
	Settings map[string]any
	Sets     map[string]map[string]any
	Commits  map[string]map[string]any
}

func instant(t *time.Time) any {
	if t == nil {
		return nil
	}

	return t.UnixNano()
}

func (f *fixture) policyState(x *Index) policyState {
	f.t.Helper()

	s, err := loadSettings(f.ctx, x.client)
	require.NoError(f.t, err)

	state := policyState{
		Settings: map[string]any{
			"policy": s.Policy, "version": s.PolicyVersion, "acknowledged": instant(s.KeyAcknowledgedAt),
			"retention": s.RetentionPolicy, "trash": s.TrashDays,
		},
		Sets:    map[string]map[string]any{},
		Commits: map[string]map[string]any{},
	}

	sets, err := x.client.Set.Query().All(f.ctx)
	require.NoError(f.t, err)

	for _, s := range sets {
		state.Sets[s.Name] = map[string]any{"id": s.ID, "retention": s.RetentionPolicy, "paused": s.RetentionPaused, "state": s.State, "erase": s.Erase}
	}

	commits, err := x.client.CommitRow.Query().All(f.ctx)
	require.NoError(f.t, err)

	for _, c := range commits {
		state.Commits[string(c.Ref)] = map[string]any{"deleted": instant(c.DeletedAt), "expires": instant(c.ExpiresAt), "retire": instant(c.RetireAt)}
	}

	return state
}

func TestRebuildRestoresWhatOperatorsChanged(t *testing.T) {
	f := newFixture(t)

	worldA := f.commit("world", f.tree(f.file("a.txt", "one")), false)
	f.advance(time.Hour)
	f.commit("world", f.tree(f.file("a.txt", "two")), false)
	f.advance(time.Hour)
	logsA := f.commit("logs", f.tree(f.file("x.log", "1")), false)
	f.advance(time.Hour)
	f.commit("logs", f.tree(f.file("x.log", "2")), false)
	f.advance(time.Hour)
	f.commit("gone", f.tree(f.file("g", "g")), false)
	f.advance(time.Hour)

	_, err := f.x.SetStorePolicy(f.ctx, storekey.DefaultPolicy(), true, f.clock)
	require.NoError(t, err)
	require.NoError(t, f.x.SetDefaultPolicy(f.ctx, &retention.Policy{KeepLast: 50}))
	require.NoError(t, f.x.SetWindows(f.ctx, index.Windows{TrashDays: 5}))
	require.NoError(t, f.x.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 20}))
	f.advance(time.Hour)

	require.NoError(t, f.x.DeleteCommit(f.ctx, worldA))
	f.advance(time.Hour)

	// trashed on its own, then with its set, then the set comes back: the
	// commit comes back with it
	require.NoError(t, f.x.DeleteCommit(f.ctx, logsA))
	f.advance(time.Hour)
	require.NoError(t, f.x.DeleteSet(f.ctx, "logs", false))
	f.advance(time.Hour)
	require.NoError(t, f.x.UndeleteSet(f.ctx, "logs"))
	f.advance(time.Hour)

	require.NoError(t, f.x.DeleteSet(f.ctx, "gone", true))
	f.advance(time.Hour)

	want := f.policyState(f.x)
	require.Equal(t, set.StateClosing, want.Sets["gone"]["state"])
	require.NotNil(t, want.Commits[string(worldA.Hash)]["deleted"])
	require.Nil(t, want.Commits[string(logsA.Hash)]["deleted"])

	y := f.index()
	require.NoError(t, reindexErr(y.ReIndex(f.ctx)))
	require.Equal(t, want, f.policyState(y))

	require.NoError(t, reindexErr(f.x.ReIndex(f.ctx)))
	require.Equal(t, want, f.policyState(f.x), "an index replays only what it has not seen")
}

func TestPolicySequenceFollowsEveryChange(t *testing.T) {
	f := newFixture(t)
	f.commit("world", f.tree(f.file("a.txt", "one")), false)

	before, err := loadSettings(f.ctx, f.x.client)
	require.NoError(t, err)

	require.NoError(t, f.x.SetPolicy(f.ctx, "world", &retention.Policy{KeepLast: 2}))
	require.ErrorIs(t, f.x.SetPolicy(f.ctx, "nowhere", nil), backup.ErrNotFound)

	after, err := loadSettings(f.ctx, f.x.client)
	require.NoError(t, err)
	require.Equal(t, before.PolicySequence+1, after.PolicySequence, "a refused change writes no policy")
}
