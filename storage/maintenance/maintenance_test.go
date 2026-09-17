package maintenance

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/twcclan/goback/storage/pack"

	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	sweeps, compactions atomic.Int32
	lastSweep           atomic.Value
}

func (f *fakeStore) Sweep(now time.Time) { f.sweeps.Add(1); f.lastSweep.Store(now) }
func (f *fakeStore) Compact() error      { f.compactions.Add(1); return errors.New("disk full") }

type fakeCollector struct{ runs atomic.Int32 }

func (f *fakeCollector) Collect(context.Context, pack.CollectOptions) (*pack.CollectReport, error) {
	f.runs.Add(1)
	return &pack.CollectReport{Objects: 7}, nil
}

type fakePresence struct{ runs atomic.Int32 }

func (f *fakePresence) BuildPendingPresence(context.Context) (int, error) {
	f.runs.Add(1)
	return 1, nil
}

type fakeRetirer struct{ runs atomic.Int32 }

func (f *fakeRetirer) Retire(context.Context, time.Time) (int, error) {
	f.runs.Add(1)
	return 2, nil
}

func TestRunnerTicksEveryScheduledJob(t *testing.T) {
	store, collector, retirer, presence := &fakeStore{}, &fakeCollector{}, &fakeRetirer{}, &fakePresence{}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	var reports atomic.Int32

	r := &Runner{
		Store:     store,
		Collector: collector,
		Retirer:   retirer,
		Presence:  presence,
		Schedule:  Schedule{Sweep: 10 * time.Millisecond, Compact: 10 * time.Millisecond, Collect: 10 * time.Millisecond, Presence: 10 * time.Millisecond},
		OnCollect: func(report *pack.CollectReport) { reports.Add(int32(report.Objects)) },
		Now:       func() time.Time { return now },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r.Run(ctx)

	require.GreaterOrEqual(t, store.sweeps.Load(), int32(2), "one sweep at start, then on the ticker")
	require.Equal(t, now, store.lastSweep.Load())
	require.GreaterOrEqual(t, store.compactions.Load(), int32(1), "a failing compaction is logged and retried")
	require.GreaterOrEqual(t, collector.runs.Load(), int32(1))
	require.Equal(t, reports.Load(), 7*collector.runs.Load())
	require.GreaterOrEqual(t, presence.runs.Load(), int32(1))
	require.Zero(t, retirer.runs.Load(), "retirement was not scheduled")
}

func TestRunnerSkipsWhatItDoesNotHave(t *testing.T) {
	r := &Runner{Schedule: Schedule{Sweep: time.Millisecond, Compact: time.Millisecond, Collect: time.Millisecond, Retire: time.Millisecond, Presence: time.Millisecond}}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	r.Run(ctx)

	retirer := &fakeRetirer{}
	r.Retirer = retirer
	r.Retire(context.Background())
	require.Equal(t, int32(1), retirer.runs.Load())
}
