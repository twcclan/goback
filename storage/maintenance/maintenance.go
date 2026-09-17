// Package maintenance runs a store's housekeeping on a schedule. The
// store only exposes the operations; whoever owns the store, a server
// with one or a host with many, decides when they run.
package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/storage/pack"
)

// Store is what the runner sweeps and compacts.
type Store interface {
	Sweep(now time.Time)
	Compact() error
}

// Presence is an index that builds presence filters on demand.
type Presence interface {
	BuildPendingPresence(ctx context.Context) (int, error)
}

// Schedule is how often each job runs; zero never runs it.
type Schedule struct {
	Sweep    time.Duration
	Compact  time.Duration
	Collect  time.Duration
	Retire   time.Duration
	Presence time.Duration
}

// Runner ticks the jobs of one store. A nil member skips its jobs.
type Runner struct {
	Store     Store
	Collector pack.Collector
	Retirer   backup.Retirer
	Presence  Presence
	Schedule  Schedule
	// OnCollect sees every garbage collection report; nil ignores them.
	OnCollect func(*pack.CollectReport)
	// Logger is where failures go; nil means slog.Default.
	Logger *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (r *Runner) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}

	return slog.Default()
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}

	return time.Now()
}

// Run sweeps once, then runs every scheduled job at its interval until ctx
// ends. Jobs of one runner never overlap.
func (r *Runner) Run(ctx context.Context) {
	r.Sweep()

	jobs := []struct {
		every time.Duration
		run   func(context.Context)
	}{
		{r.Schedule.Sweep, func(context.Context) { r.Sweep() }},
		{r.Schedule.Compact, func(context.Context) { r.Compact() }},
		{r.Schedule.Collect, r.Collect},
		{r.Schedule.Retire, r.Retire},
		{r.Schedule.Presence, r.BuildPresence},
	}

	var tickers []*time.Ticker
	cases := make([]<-chan time.Time, len(jobs))
	for i, job := range jobs {
		if job.every <= 0 {
			continue
		}

		t := time.NewTicker(job.every)
		tickers = append(tickers, t)
		cases[i] = t.C
	}

	defer func() {
		for _, t := range tickers {
			t.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-cases[0]:
			jobs[0].run(ctx)
		case <-cases[1]:
			jobs[1].run(ctx)
		case <-cases[2]:
			jobs[2].run(ctx)
		case <-cases[3]:
			jobs[3].run(ctx)
		case <-cases[4]:
			jobs[4].run(ctx)
		}
	}
}

// Sweep finalizes idle archives and ends expired sessions.
func (r *Runner) Sweep() {
	if r.Store != nil {
		r.Store.Sweep(r.now())
	}
}

// Compact rewrites small archives.
func (r *Runner) Compact() {
	if r.Store == nil {
		return
	}

	if err := r.Store.Compact(); err != nil {
		r.logger().Error("compaction failed", "err", err)
	}
}

// Collect garbage collects the store.
func (r *Runner) Collect(ctx context.Context) {
	if r.Collector == nil {
		return
	}

	report, err := r.Collector.Collect(ctx, pack.CollectOptions{})
	if err != nil {
		r.logger().Error("garbage collection failed", "err", err)
		return
	}

	if r.OnCollect != nil {
		r.OnCollect(report)
	}
}

// BuildPresence builds the presence filters that are missing.
func (r *Runner) BuildPresence(ctx context.Context) {
	if r.Presence == nil {
		return
	}

	if _, err := r.Presence.BuildPendingPresence(ctx); err != nil {
		r.logger().Error("building presence filters failed", "err", err)
	}
}

// Retire tombstones every retired commit past its window.
func (r *Runner) Retire(ctx context.Context) {
	if r.Retirer == nil {
		return
	}

	n, err := r.Retirer.Retire(ctx, r.now())
	if err != nil {
		r.logger().Error("retirement failed", "retired", n, "err", err)
		return
	}

	if n > 0 {
		r.logger().Info("retired commits", "count", n)
	}
}
