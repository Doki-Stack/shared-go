package otel

import (
	"context"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit_ReturnsShutdownWithoutError(t *testing.T) {
	// Use empty endpoint to get noop tracer (no OTLP connection required)
	ctx := context.Background()
	shutdown, err := Init(ctx, "test-service", WithExporterEndpoint(""))
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = shutdown(shutdownCtx)
	require.NoError(t, err)
}

func TestInit_WithOptions(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, "test-svc",
		WithExporterEndpoint(""),
		WithServiceVersion("1.0.0"),
		WithEnvironment("production"),
		WithPrometheusPort(0), // Use 0 to avoid port conflict in tests
	)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, shutdown(shutdownCtx))
}

func TestInit_EnvVarEndpoint(t *testing.T) {
	os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	defer os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	ctx := context.Background()
	// Use empty override to force noop (avoid connecting to real endpoint)
	shutdown, err := Init(ctx, "test", WithExporterEndpoint(""))
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, shutdown(shutdownCtx))
}

func TestTraceIDFromContext_WithSpan(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, "test", WithExporterEndpoint(""))
	require.NoError(t, err)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()

	ctx, span := StartSpan(ctx, "test-span")
	defer span.End()

	traceID := TraceIDFromContext(ctx)
	assert.NotEmpty(t, traceID)
}

func TestTraceIDFromContext_NoSpan(t *testing.T) {
	ctx := context.Background()
	traceID := TraceIDFromContext(ctx)
	assert.Empty(t, traceID)
}

func TestSpanFromContext(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, "test", WithExporterEndpoint(""))
	require.NoError(t, err)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()

	ctx, span := StartSpan(ctx, "test-span")
	defer span.End()

	got := SpanFromContext(ctx)
	assert.Equal(t, span, got)
	assert.True(t, got.SpanContext().IsValid())
}

func TestStartSpan(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, "test", WithExporterEndpoint(""))
	require.NoError(t, err)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()

	ctx, span := StartSpan(ctx, "my-span")
	defer span.End()

	assert.True(t, span.SpanContext().IsValid())
	// Verify context has the span
	ctxSpan := trace.SpanFromContext(ctx)
	assert.Equal(t, span.SpanContext().SpanID(), ctxSpan.SpanContext().SpanID())
}
