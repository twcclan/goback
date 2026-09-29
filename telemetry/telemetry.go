// Package telemetry installs the process-wide OpenTelemetry providers.
package telemetry

import (
	"context"
	"errors"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Setup installs tracing and metrics as the standard OTEL_* environment
// variables describe them, naming the process service unless
// OTEL_SERVICE_NAME does. A signal is exported only once its exporter or
// an OTLP endpoint is configured; setting OTEL_EXPORTER_OTLP_ENDPOINT
// alone exports both, and OTEL_TRACES_SAMPLER picks what is sampled.
// The returned function flushes and stops what was installed.
func Setup(ctx context.Context, service string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", service)),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
	)
	if err != nil {
		return nil, err
	}

	var stops []func(context.Context) error

	stop := func(ctx context.Context) error {
		var errs []error
		for _, s := range stops {
			errs = append(errs, s(ctx))
		}

		return errors.Join(errs...)
	}

	if configured("TRACES") {
		exporter, err := autoexport.NewSpanExporter(ctx)
		if err != nil {
			return nil, err
		}

		provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
		otel.SetTracerProvider(provider)
		stops = append(stops, provider.Shutdown)
	}

	if configured("METRICS") {
		reader, err := autoexport.NewMetricReader(ctx)
		if err != nil {
			return nil, errors.Join(err, stop(ctx))
		}

		provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithResource(res))
		otel.SetMeterProvider(provider)
		stops = append(stops, provider.Shutdown)

		if err := runtime.Start(); err != nil {
			return nil, errors.Join(err, stop(ctx))
		}
	}

	return stop, nil
}

// configured reports whether the environment asks for a signal to be
// exported, so that a process nobody configured does not try to reach a
// collector that is not there.
func configured(signal string) bool {
	for _, key := range []string{
		"OTEL_" + signal + "_EXPORTER",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_" + signal + "_ENDPOINT",
	} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}

	return false
}
