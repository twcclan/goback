package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/twcclan/goback/index"
	"github.com/twcclan/goback/index/sql/ent"
	"github.com/twcclan/goback/index/sql/ent/reindex"

	"entgo.io/ent/dialect"
)

const (
	// reindexPercent is the churn, as a share of a table's live rows, at
	// which its indexes are rebuilt.
	reindexPercent = 30
	// reindexFloor is the least churn that rebuilds a table's indexes, so
	// a small table is not rebuilt for every handful of rows.
	reindexFloor = 10_000
)

// tableChurn is a table's statistics counters as Postgres reports them.
type tableChurn struct {
	table string
	// counter is the rows inserted, updated outside HOT and deleted since
	// the statistics were last reset
	counter int64
	live    int64
	// statsReset is when the database's statistics were last reset, zero
	// for never
	statsReset time.Time
}

// churnSince is the churn of t since the counters base held when its
// indexes were last rebuilt; nil base counts everything. Counters that
// went back, or a reset since base, count from the reset.
func churnSince(t tableChurn, base *ent.Reindex) int64 {
	if base == nil || t.counter < base.Churn || !t.statsReset.Equal(deref(base.StatsReset)) {
		return t.counter
	}

	return t.counter - base.Churn
}

// reindexDue says whether churn on a table of live rows bloated its
// indexes enough to rebuild them.
func reindexDue(churn, live int64) bool {
	return churn >= reindexFloor && churn*100 >= live*reindexPercent
}

// ReindexChurned rebuilds, one at a time and without blocking writes, the
// indexes of every table of the index whose rows changed by at least 30%
// of its live rows, and at least 10,000, since its last reindex, and
// reports each table it rebuilt. It is meant for the end of a maintenance
// run that churned the index, such as a compaction, retirement or
// collection. Only one ReindexChurned of a database schema runs at a time;
// another returns nothing. On SQLite it does nothing.
func (x *Index) ReindexChurned(ctx context.Context) ([]index.Reindexed, error) {
	if x.dialect != dialect.Postgres {
		return nil, nil
	}

	conn, err := x.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var locked bool
	err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtext('goback.reindex'), hashtext(current_schema()))`).Scan(&locked)
	if err != nil || !locked {
		return nil, err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtext('goback.reindex'), hashtext(current_schema()))`)

	if err := x.dropReindexLeftovers(ctx, conn); err != nil {
		return nil, err
	}

	tables, err := churnedTables(ctx, conn)
	if err != nil {
		return nil, err
	}

	bases, err := x.client.Reindex.Query().All(ctx)
	if err != nil {
		return nil, err
	}

	last := make(map[string]*ent.Reindex, len(bases))
	for _, b := range bases {
		last[b.ID] = b
	}

	var done []index.Reindexed

	for _, t := range tables {
		churn := churnSince(t, last[t.table])
		if !reindexDue(churn, t.live) {
			continue
		}

		rebuilt, err := x.reindexTable(ctx, conn, t, churn)
		if err != nil {
			return done, fmt.Errorf("reindexing %s: %w", t.table, err)
		}

		done = append(done, rebuilt)
	}

	return done, nil
}

func (x *Index) reindexTable(ctx context.Context, conn *sql.Conn, t tableChurn, churn int64) (index.Reindexed, error) {
	began := time.Now()
	rebuilt := index.Reindexed{Table: t.table, Churn: churn, Live: t.live}

	rows, err := conn.QueryContext(ctx, `
		SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname), c.relname, pg_relation_size(c.oid)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE i.indrelid = (quote_ident(current_schema()) || '.' || quote_ident($1))::regclass AND i.indisvalid
		ORDER BY c.relname`, t.table)
	if err != nil {
		return rebuilt, err
	}

	var qualified []string
	for rows.Next() {
		var (
			name string
			idx  index.RebuiltIndex
		)

		if err := rows.Scan(&name, &idx.Name, &idx.Before); err != nil {
			rows.Close()
			return rebuilt, err
		}

		qualified = append(qualified, name)
		rebuilt.Indexes = append(rebuilt.Indexes, idx)
	}

	if err := rows.Err(); err != nil {
		return rebuilt, err
	}

	for i, name := range qualified {
		// REINDEX CONCURRENTLY refuses to run inside a transaction block
		_, err := conn.ExecContext(ctx, "REINDEX INDEX CONCURRENTLY "+name)
		if err == nil {
			err = conn.QueryRowContext(ctx, `SELECT pg_relation_size($1::regclass)`, name).Scan(&rebuilt.Indexes[i].After)
		}

		if err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			err = errors.Join(err, x.dropReindexLeftovers(cleanup, x.db))
			cancel()

			return rebuilt, err
		}
	}

	rebuilt.Took = time.Since(began)

	err = x.client.Reindex.Create().
		SetID(t.table).
		SetChurn(t.counter).
		SetNillableStatsReset(nilIfZeroTime(t.statsReset)).
		SetReindexedAt(x.now()).
		OnConflictColumns(reindex.FieldID).
		UpdateNewValues().
		Exec(ctx)
	if err != nil {
		return rebuilt, err
	}

	attrs := []any{"table", t.table, "churn", churn, "live", t.live, "took", rebuilt.Took.Round(time.Millisecond)}
	for _, idx := range rebuilt.Indexes {
		attrs = append(attrs, idx.Name, fmt.Sprintf("%d -> %d", idx.Before, idx.After))
	}

	x.logger().InfoContext(ctx, "reindexed table", attrs...)

	return rebuilt, nil
}

// churnedTables reads the statistics counters of every table in the
// current schema.
func churnedTables(ctx context.Context, conn *sql.Conn) ([]tableChurn, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT s.relname,
			s.n_tup_ins + s.n_tup_upd - s.n_tup_hot_upd + s.n_tup_del,
			greatest(s.n_live_tup, c.reltuples::bigint),
			d.stats_reset
		FROM pg_stat_user_tables s
		JOIN pg_class c ON c.oid = s.relid
		LEFT JOIN pg_stat_database d ON d.datname = current_database()
		WHERE s.schemaname = current_schema()
		ORDER BY s.relname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []tableChurn
	for rows.Next() {
		var (
			t     tableChurn
			reset sql.NullTime
		)

		if err := rows.Scan(&t.table, &t.counter, &t.live, &reset); err != nil {
			return nil, err
		}

		if reset.Valid {
			t.statsReset = reset.Time
		}

		tables = append(tables, t)
	}

	return tables, rows.Err()
}

// execQueryer is a pool or one of its connections.
type execQueryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// dropReindexLeftovers drops the invalid copies a failed REINDEX
// CONCURRENTLY leaves in the current schema. While another session of the
// database builds an index none is dropped, since a copy it is building
// reads as invalid until it is done.
func (x *Index) dropReindexLeftovers(ctx context.Context, db execQueryer) error {
	rows, err := db.QueryContext(ctx, `
		SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND NOT i.indisvalid AND c.relname ~ '_cc(new|old)[0-9]*$'
			AND NOT EXISTS (
				SELECT 1 FROM pg_stat_progress_create_index p
				JOIN pg_database d ON d.oid = p.datid
				WHERE d.datname = current_database() AND p.pid <> pg_backend_pid())`)
	if err != nil {
		return err
	}

	var leftovers []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}

		leftovers = append(leftovers, name)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	for _, name := range leftovers {
		if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+name); err != nil {
			return err
		}

		x.logger().InfoContext(ctx, "dropped a failed reindex's leftover", "index", name)
	}

	return nil
}
