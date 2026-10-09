package progress_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gobackio/goback/progress"

	"github.com/stretchr/testify/require"
)

func TestAPhaseWithoutAFuncIsNil(t *testing.T) {
	phase := progress.Start(context.Background(), progress.OpScrub, progress.PhaseScrub, 1, 1)
	require.Nil(t, phase)

	phase.Add(1, 1)
	phase.Finish()

	ctx := progress.NewContext(context.Background(), nil)
	require.Nil(t, progress.Start(ctx, progress.OpScrub, progress.PhaseScrub, 1, 1))
}

func TestConcurrentAddsReportInOrderOneAtATime(t *testing.T) {
	var (
		updates            []progress.Update
		inFlight, overlaps atomic.Int32
	)

	ctx := progress.NewContext(context.Background(), func(u progress.Update) {
		if inFlight.Add(1) != 1 {
			overlaps.Add(1)
		}
		updates = append(updates, u)
		inFlight.Add(-1)
	})

	phase := progress.Start(ctx, progress.OpCollect, progress.PhaseMark, 0, 0)

	var wg sync.WaitGroup
	var added atomic.Int64
	until := time.Now().Add(progress.Interval + 200*time.Millisecond)

	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for time.Now().Before(until) {
				phase.Add(1, 2)
				added.Add(1)
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	phase.Finish()

	require.Zero(t, overlaps.Load(), "updates never overlap")
	require.Greater(t, len(updates), 2, "a phase that outlasts the interval reports in between")
	require.Equal(t, progress.Update{Op: progress.OpCollect, Phase: progress.PhaseMark}, updates[0])

	for i := 1; i < len(updates); i++ {
		require.GreaterOrEqual(t, updates[i].Done, updates[i-1].Done)
		require.GreaterOrEqual(t, updates[i].Bytes, updates[i-1].Bytes)
	}

	last := updates[len(updates)-1]
	require.Equal(t, added.Load(), last.Done)
	require.Equal(t, 2*added.Load(), last.Bytes)
}
