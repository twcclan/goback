package sql

import (
	"context"
	"testing"
	"time"

	"github.com/gobackio/goback/index/sql/ent"
	"github.com/gobackio/goback/testing/testpg"

	"github.com/stretchr/testify/require"
)

func TestReindexIsDueOnceChurnReachesAShareOfTheLiveRows(t *testing.T) {
	reset := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		table tableChurn
		base  *ent.Reindex
		due   bool
	}{
		"below the share":          {tableChurn{counter: 29_999, live: 100_000}, nil, false},
		"at the share":             {tableChurn{counter: 30_000, live: 100_000}, nil, true},
		"under the floor":          {tableChurn{counter: 9_999, live: 10}, nil, false},
		"little since the last":    {tableChurn{counter: 60_000, live: 100_000}, &ent.Reindex{Churn: 40_000}, false},
		"enough since the last":    {tableChurn{counter: 70_000, live: 100_000}, &ent.Reindex{Churn: 40_000}, true},
		"counters went back":       {tableChurn{counter: 30_000, live: 100_000}, &ent.Reindex{Churn: 50_000}, true},
		"statistics reset since":   {tableChurn{counter: 60_000, live: 100_000, statsReset: reset}, &ent.Reindex{Churn: 40_000}, true},
		"no reset since the last":  {tableChurn{counter: 60_000, live: 100_000, statsReset: reset}, &ent.Reindex{Churn: 40_000, StatsReset: &reset}, false},
		"live rows unknown":        {tableChurn{counter: 10_000}, nil, true},
		"nothing changed at all":   {tableChurn{live: 100_000}, nil, false},
		"churn exactly the last's": {tableChurn{counter: 40_000, live: 10}, &ent.Reindex{Churn: 40_000}, false},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.due, reindexDue(churnSince(tc.table, tc.base), tc.table.live))
		})
	}
}

func TestReindexRebuildsChurnedTablesOnPostgres(t *testing.T) {
	ctx := context.Background()

	x := New(testpg.Start(t), newMemStore())
	require.NoError(t, x.Open())
	t.Cleanup(func() { _ = x.Close() })

	exec := func(query string) {
		t.Helper()
		_, err := x.db.ExecContext(ctx, query)
		require.NoError(t, err)
	}

	counted := func(n int64) {
		t.Helper()
		require.Eventually(t, func() bool {
			var churn int64
			err := x.db.QueryRowContext(ctx, `SELECT n_tup_ins + n_tup_del FROM pg_stat_user_tables WHERE relname = 'churn'`).Scan(&churn)
			return err == nil && churn >= n
		}, time.Minute, 200*time.Millisecond, "the statistics catch up")
	}

	exec(`CREATE TABLE churn (id bigint PRIMARY KEY, v bytea NOT NULL)`)
	exec(`CREATE INDEX churn_v ON churn (v)`)
	exec(`INSERT INTO churn SELECT i, sha256(int8send(i)) FROM generate_series(1, 40000) i`)
	exec(`DELETE FROM churn WHERE id % 4 <> 0`)
	counted(70_000)

	reindexed, err := x.ReindexChurned(ctx)
	require.NoError(t, err)
	require.Len(t, reindexed, 1, "only the churned table is rebuilt")
	require.Equal(t, "churn", reindexed[0].Table)
	require.Len(t, reindexed[0].Indexes, 2)
	for _, idx := range reindexed[0].Indexes {
		require.Less(t, idx.After, idx.Before, "%s shrinks", idx.Name)
	}

	reindexed, err = x.ReindexChurned(ctx)
	require.NoError(t, err)
	require.Empty(t, reindexed, "the churn before the last reindex counts no more")

	exec(`SELECT pg_stat_reset()`)
	exec(`INSERT INTO churn SELECT i, sha256(int8send(i)) FROM generate_series(100001, 112000) i`)
	counted(12_000)

	reindexed, err = x.ReindexChurned(ctx)
	require.NoError(t, err)
	require.Len(t, reindexed, 1, "after a reset the churn counts from the reset")

	exec(`INSERT INTO churn SELECT i, 'same' FROM generate_series(200001, 200002) i`)
	_, err = x.db.ExecContext(ctx, `CREATE UNIQUE INDEX CONCURRENTLY churn_v_ccnew ON churn (v)`)
	require.Error(t, err, "a failed concurrent build leaves an invalid index")

	_, err = x.ReindexChurned(ctx)
	require.NoError(t, err)

	var left bool
	require.NoError(t, x.db.QueryRowContext(ctx, `SELECT to_regclass('churn_v_ccnew') IS NOT NULL`).Scan(&left))
	require.False(t, left, "the leftover is dropped")
}
