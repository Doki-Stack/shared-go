# shared-go Implementation Plan — OpenTelemetry Package

## 1. Overview

The `otel` package initializes OpenTelemetry for the Doki Stack platform: traces export to Tempo via OTLP gRPC, and metrics export to Prometheus. The package provides a single `Init` function that returns a shutdown function to be called on service exit.

**Package name:** `otel`

## 2. Files

| File | Purpose |
|------|---------|
| `otel.go` | Init, options, helpers |
| `otel_test.go` | Unit tests |

## 3. Init Function

```go
package otel

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
)

// Init initializes OpenTelemetry (TracerProvider, MeterProvider) and registers them globally.
// Returns a shutdown function that must be called on service exit (defer in main).
func Init(ctx context.Context, serviceName string, opts ...Option) (shutdown func(context.Context) error, err error) {
	cfg := &config{
		exporterEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		insecure:         true,
		serviceVersion:   "dev",
		environment:      "development",
		prometheusPort:   9090,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// 1. TracerProvider
	tp, err := initTracerProvider(ctx, serviceName, cfg)
	if err != nil {
		return nil, err
	}
	otel.SetTracerProvider(tp)

	// 2. MeterProvider
	mp, err := initMeterProvider(ctx, serviceName, cfg)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, err
	}
	otel.SetMeterProvider(mp)

	// 3. Propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown = func(ctx context.Context) error {
		var errs []error
		if err := tp.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
		if err := mp.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
		if len(errs) > 0 {
			return fmt.Errorf("otel shutdown: %v", errs)
		}
		return nil
	}
	return shutdown, nil
}
```

## 4. What Init Sets Up

### 4.1 TracerProvider

- **Exporter:** OTLP gRPC → Tempo (e.g., `tempo.monitoring.svc.cluster.local:4317`)
- **Resource attributes:** `service.name`, `service.version`, `deployment.environment`
- **Processor:** BatchSpanProcessor with default batch size and timeout
- **Sampler:** From env `OTEL_TRACES_SAMPLER` / `OTEL_TRACES_SAMPLER_ARG` (see §6)

### 4.2 MeterProvider

- **Exporter:** Prometheus exporter (HTTP server on configurable port)
- **Metrics:** Runtime (memory, goroutines, GC) + HTTP server metrics (via middleware)
- **Scrape endpoint:** `:9090/metrics` by default

### 4.3 Global Registration

- `otel.SetTracerProvider(tp)`
- `otel.SetMeterProvider(mp)`
- `otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(TraceContext{}, Baggage{}))`

## 5. Options

```go
type config struct {
	exporterEndpoint string
	insecure         bool
	serviceVersion   string
	environment      string
	prometheusPort   int
}

type Option func(*config)

func WithExporterEndpoint(endpoint string) Option {
	return func(c *config) {
		c.exporterEndpoint = endpoint
	}
}

func WithInsecure(insecure bool) Option {
	return func(c *config) {
		c.insecure = insecure
	}
}

func WithServiceVersion(version string) Option {
	return func(c *config) {
		c.serviceVersion = version
	}
}

func WithEnvironment(env string) Option {
	return func(c *config) {
		c.environment = env
	}
}

func WithPrometheusPort(port int) Option {
	return func(c *config) {
		c.prometheusPort = port
	}
}
```

### 5.1 Default Values

| Option | Default | Source |
|--------|---------|--------|
| ExporterEndpoint | `OTEL_EXPORTER_OTLP_ENDPOINT` env var | Environment |
| Insecure | `true` | Dev-friendly |
| ServiceVersion | `"dev"` | Hardcoded |
| Environment | `"development"` | Hardcoded |
| PrometheusPort | `9090` | Hardcoded |

## 6. Environment Variables

Read by default (or overridden by options):

| Variable | Purpose | Default |
|----------|---------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP gRPC endpoint (Tempo) | e.g., `tempo.monitoring.svc.cluster.local:4317` |
| `OTEL_SERVICE_NAME` | Service name (overridden by `serviceName` param) | — |
| `OTEL_TRACES_SAMPLER` | Sampler type | `always_on` (dev), `parentbased_traceidratio` (prod) |
| `OTEL_TRACES_SAMPLER_ARG` | Sampler argument (e.g., ratio) | `1.0` (dev), `0.1` (prod) |

**Sampler logic:** If `WithEnvironment("production")` is set, use `parentbased_traceidratio` with `0.1`; otherwise `always_on` with `1.0`.

## 7. Shutdown Behavior

- Flushes all pending spans (via TracerProvider.Shutdown)
- Shuts down MeterProvider
- Context-aware: pass context with timeout for graceful shutdown
- Returns error if any shutdown step fails

**Usage in main:**

```go
func main() {
	ctx := context.Background()
	shutdown, err := otel.Init(ctx, "api-server")
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			log.Printf("otel shutdown: %v", err)
		}
	}()
	// ... run service
}
```

## 8. Helper Functions

```go
import "go.opentelemetry.io/otel/trace"

// SpanFromContext returns the current span from context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// TraceIDFromContext returns the trace ID as a string, or empty if no span.
func TraceIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return ""
	}
	return span.SpanContext().TraceID().String()
}

// StartSpan starts a new span and returns the updated context and span.
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	tracer := otel.Tracer("github.com/doki-stack/shared-go/otel")
	return tracer.Start(ctx, name)
}
```

**Note:** `StartSpan` uses a tracer; the tracer name can be configurable or derived from the service name passed to Init.

## 9. TracerProvider Implementation Sketch

```go
func initTracerProvider(ctx context.Context, serviceName string, cfg *config) (*trace.TracerProvider, error) {
	endpoint := cfg.exporterEndpoint
	if endpoint == "" {
		endpoint = "localhost:4317"
	}

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if cfg.insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otlp trace exporter: %w", err)
	}

	resource, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(cfg.serviceVersion),
			attribute.String("deployment.environment", cfg.environment),
		),
	)

	sampler := trace.AlwaysSample()
	if cfg.environment == "production" {
		sampler = trace.ParentBased(trace.TraceIDRatioBased(0.1))
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(resource),
		trace.WithSampler(sampler),
	)
	return tp, nil
}
```

## 10. MeterProvider Implementation Sketch

```go
func initMeterProvider(ctx context.Context, serviceName string, cfg *config) (*metric.MeterProvider, error) {
	exporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("prometheus exporter: %w", err)
	}

	resource, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(cfg.serviceVersion),
			attribute.String("deployment.environment", cfg.environment),
		),
	)

	mp := metric.NewMeterProvider(
		metric.WithResource(resource),
		metric.WithReader(metric.NewPeriodicReader(exporter)),
	)

	// Start Prometheus HTTP server for scraping
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", exporter)
		addr := fmt.Sprintf(":%d", cfg.prometheusPort)
		_ = http.ListenAndServe(addr, mux)
	}()

	return mp, nil
}
```

**Note:** Prometheus exporter from `go.opentelemetry.io/otel/exporters/prometheus` exposes a handler for the `/metrics` endpoint. The API may vary by version; common patterns: `exporter.Handler()` or serving the exporter's HTTP handler. Verify against current OTel Go SDK docs.

## 11. Dependencies

```go
require (
	go.opentelemetry.io/otel v1.32.0
	go.opentelemetry.io/otel/sdk v1.32.0
	go.opentelemetry.io/otel/sdk/trace v1.32.0
	go.opentelemetry.io/otel/sdk/metric v1.32.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.32.0
	go.opentelemetry.io/otel/exporters/prometheus v0.52.0  // or latest compatible
	go.opentelemetry.io/otel/propagation v1.32.0
	go.opentelemetry.io/otel/sdk/resource v1.32.0
	go.opentelemetry.io/otel/semconv/v1.32.0
)
```

**Version note:** Use `go mod tidy` to resolve compatible versions. `otel/exporters/prometheus` may have a different module path; check current OTel Go docs.

## 12. Full API Surface Summary

```go
// Init
func Init(ctx context.Context, serviceName string, opts ...Option) (shutdown func(context.Context) error, err error)

// Options
func WithExporterEndpoint(endpoint string) Option
func WithInsecure(insecure bool) Option
func WithServiceVersion(version string) Option
func WithEnvironment(env string) Option
func WithPrometheusPort(port int) Option

// Helpers
func SpanFromContext(ctx context.Context) trace.Span
func TraceIDFromContext(ctx context.Context) string
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span)
```

## 13. Test Plan (otel_test.go)

| Test Case | Description |
|-----------|-------------|
| **Init success** | Init with mock/in-memory endpoint; verify no error |
| **Init returns shutdown** | Call shutdown; verify it runs without panic |
| **Shutdown flushes** | Init, create span, shutdown; verify exporter receives |
| **Shutdown context timeout** | Pass cancelled context to shutdown; verify behavior |
| **WithExporterEndpoint** | Override endpoint; verify used in exporter |
| **WithInsecure** | Set false; verify TLS options (or skip if no TLS in test) |
| **WithEnvironment production** | Verify sampler is TraceIDRatioBased |
| **TraceIDFromContext** | Start span, get trace ID from context; verify non-empty |
| **TraceIDFromContext no span** | Empty context; verify returns "" |
| **StartSpan** | Start span, verify context has span, span has name |
| **Environment variable** | Set OTEL_EXPORTER_OTLP_ENDPOINT; verify read (integration) |

### Example Test Snippets

```go
func TestInit(t *testing.T) {
	ctx := context.Background()
	shutdown, err := otel.Init(ctx, "test-service", otel.WithExporterEndpoint("localhost:4317"))
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = shutdown(shutdownCtx)
	require.NoError(t, err)
}

func TestTraceIDFromContext(t *testing.T) {
	ctx := context.Background()
	_, span := otel.StartSpan(ctx, "test")
	defer span.End()
	ctx = trace.ContextWithSpan(ctx, span)

	traceID := otel.TraceIDFromContext(ctx)
	assert.NotEmpty(t, traceID)
}

func TestTraceIDFromContext_NoSpan(t *testing.T) {
	ctx := context.Background()
	traceID := otel.TraceIDFromContext(ctx)
	assert.Empty(t, traceID)
}
```

## 14. Consumer Usage Example

```go
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/doki-stack/shared-go/otel"
)

func main() {
	ctx := context.Background()
	shutdown, err := otel.Init(ctx, "api-server",
		otel.WithExporterEndpoint("tempo.monitoring.svc.cluster.local:4317"),
		otel.WithEnvironment("production"),
		otel.WithServiceVersion("1.0.0"),
		otel.WithPrometheusPort(9090))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			log.Printf("otel shutdown: %v", err)
		}
	}()

	// Use otel.Tracer() or otel.StartSpan() in handlers
	http.ListenAndServe(":8080", nil)
}
```
