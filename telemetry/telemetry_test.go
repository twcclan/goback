package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func clearEnv(t *testing.T) {
	for _, key := range []string{
		"OTEL_TRACES_EXPORTER", "OTEL_METRICS_EXPORTER",
		"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	} {
		t.Setenv(key, "")
	}
}

func TestNothingConfiguredExportsNothing(t *testing.T) {
	clearEnv(t)

	require.False(t, configured("TRACES"))
	require.False(t, configured("METRICS"))
}

func TestAnEndpointAloneExportsBothSignals(t *testing.T) {
	clearEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")

	require.True(t, configured("TRACES"))
	require.True(t, configured("METRICS"))
}

func TestASignalsOwnEndpointExportsOnlyIt(t *testing.T) {
	clearEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "http://collector:4318/v1/metrics")

	require.False(t, configured("TRACES"))
	require.True(t, configured("METRICS"))
}

func TestSetupInstallsTheExportersTheEnvironmentNames(t *testing.T) {
	clearEnv(t)
	t.Setenv("OTEL_TRACES_EXPORTER", "console")
	t.Setenv("OTEL_METRICS_EXPORTER", "console")

	stop, err := Setup(context.Background(), "goback-test")
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, stop(context.Background())) })

	require.IsType(t, &sdktrace.TracerProvider{}, otel.GetTracerProvider())
	require.IsType(t, &sdkmetric.MeterProvider{}, otel.GetMeterProvider())
}
