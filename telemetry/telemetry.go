// Package telemetry installs the process-wide OpenTelemetry providers.
package telemetry

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const serviceName = "goback"

func serviceResource() *resource.Resource {
	return resource.NewSchemaless(attribute.String("service.name", serviceName))
}

// Metrics installs a meter provider backed by a private Prometheus registry
// and returns the handler that serves it.
func Metrics() (http.Handler, error) {
	registry := prometheus.NewRegistry()

	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return nil, err
	}

	otel.SetMeterProvider(sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(serviceResource()),
	))

	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{}), nil
}

// Traces installs a tracer provider that batches spans to an OTLP/HTTP
// endpoint, sampling the given ratio of new traces. The returned function
// flushes and stops it.
func Traces(ctx context.Context, endpoint string, ratio float64) (func(context.Context) error, error) {
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
		sdktrace.WithResource(serviceResource()),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return provider.Shutdown, nil
}
