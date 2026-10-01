package sql

import (
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const instrumentationName = "goback.io/index/sql"

var (
	tracer = otel.Tracer(instrumentationName)
	meter  = otel.Meter(instrumentationName)

	keyLookup = attribute.Key("lookup")

	lookupDuration = mustInstrument(meter.Float64Histogram("goback.index.sql.lookup_duration",
		metric.WithDescription("duration of finding where archives hold objects"), metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(0.1, 0.25, 0.5, 1, 2, 5, 10, 25, 50, 100, 250, 500, 1000)))
)

func mustInstrument[T any](instrument T, err error) T {
	if err != nil {
		panic(err)
	}

	return instrument
}

// phases times the steps of one operation for a single log line.
type phases struct {
	start, last time.Time
	took        []any
}

func newPhases() *phases {
	now := time.Now()

	return &phases{start: now, last: now}
}

// done ends the step named and starts the next.
func (p *phases) done(step string) {
	now := time.Now()
	p.took = append(p.took, step, now.Sub(p.last).Round(time.Millisecond))
	p.last = now
}

func (p *phases) log(logger *slog.Logger, msg string, attrs ...any) {
	logger.Info(msg, append(append(attrs, p.took...), "took", time.Since(p.start).Round(time.Millisecond))...)
}
