package sql

import (
	"context"
	"testing"

	"github.com/twcclan/goback/backup/retention"
	"github.com/twcclan/goback/index"

	"github.com/stretchr/testify/require"
)

func TestOperatorRetentionSurface(t *testing.T) {
	ctx := context.Background()
	x := openIndex(t, newMemStore())

	_, err := ensureSet(ctx, x.client, "world", "node-1", 0, true)
	require.NoError(t, err)
	_, err = ensureSet(ctx, x.client, "rebuilt", "node-1", 42, true)
	require.NoError(t, err)

	policy, stored, err := x.GetDefaultPolicy(ctx)
	require.NoError(t, err)
	require.False(t, stored)
	require.Equal(t, retention.Default, policy)

	ret, err := x.GetPolicy(ctx, "world")
	require.NoError(t, err)
	require.Nil(t, ret.Policy, "a fresh set inherits")
	require.Equal(t, retention.Default, ret.Effective)
	require.False(t, ret.Paused)

	ret, err = x.GetPolicy(ctx, "rebuilt")
	require.NoError(t, err)
	require.True(t, ret.Paused, "a rebuilt set waits for a policy")

	require.NoError(t, x.SetDefaultPolicy(ctx, &retention.Policy{KeepLast: 5}))
	policy, stored, err = x.GetDefaultPolicy(ctx)
	require.NoError(t, err)
	require.True(t, stored)
	require.Equal(t, retention.Policy{KeepLast: 5}, policy)

	ret, err = x.GetPolicy(ctx, "world")
	require.NoError(t, err)
	require.Nil(t, ret.Policy)
	require.Equal(t, retention.Policy{KeepLast: 5}, ret.Effective, "the store's default reaches the set")

	require.NoError(t, x.SetPolicy(ctx, "rebuilt", &retention.Policy{KeepLast: 2}))
	ret, err = x.GetPolicy(ctx, "rebuilt")
	require.NoError(t, err)
	require.Equal(t, &retention.Policy{KeepLast: 2}, ret.Policy)
	require.Equal(t, retention.Policy{KeepLast: 2}, ret.Effective)
	require.False(t, ret.Paused, "setting a policy resumes retirement")

	require.NoError(t, x.SetPolicy(ctx, "rebuilt", nil))
	ret, err = x.GetPolicy(ctx, "rebuilt")
	require.NoError(t, err)
	require.Nil(t, ret.Policy)
	require.Equal(t, retention.Policy{KeepLast: 5}, ret.Effective)

	_, err = x.GetPolicy(ctx, "nowhere")
	require.Error(t, err)

	w, err := x.Windows(ctx)
	require.NoError(t, err)
	require.Equal(t, index.Windows{HoldDays: 14, TrashDays: 14}, w)

	require.NoError(t, x.SetWindows(ctx, index.Windows{HoldDays: 7, TrashDays: 3}))
	w, err = x.Windows(ctx)
	require.NoError(t, err)
	require.Equal(t, index.Windows{HoldDays: 7, TrashDays: 3}, w)

	require.ErrorIs(t, x.SetWindows(ctx, index.Windows{HoldDays: -1}), retention.ErrInvalidPolicy)
}
