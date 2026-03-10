package otel

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// config holds options for Init.
type config struct {
	exporterEndpoint string
	insecure         bool
	serviceVersion   string
	environment      string
	prometheusPort   int
}

// Option configures Init.
type Option func(*config)

// WithExporterEndpoint sets the OTLP endpoint (e.g., http://tempo:4318).
func WithExporterEndpoint(endpoint string) Option {
	return func(c *config) {
		c.exporterEndpoint = endpoint
	}
}

// WithInsecure sets whether to use insecure (non-TLS) connection.
func WithInsecure(insecure bool) Option {
	return func(c *config) {
		c.insecure = insecure
	}
}

// WithServiceVersion sets the service version.
func WithServiceVersion(version string) Option {
	return func(c *config) {
		c.serviceVersion = version
	}
}

// WithEnvironment sets the deployment environment.
func WithEnvironment(env string) Option {
	return func(c *config) {
		c.environment = env
	}
}

// WithPrometheusPort sets the port for the Prometheus metrics endpoint.
func WithPrometheusPort(port int) Option {
	return func(c *config) {
		c.prometheusPort = port
	}
}

// Init initializes OpenTelemetry (TracerProvider, MeterProvider) and registers them globally.
// Returns a shutdown function that must be called on service exit (defer in main).
// When exporterEndpoint is empty (after applying options and env), a noop tracer is used.
func Init(ctx context.Context, serviceName string, opts ...Option) (shutdown func(context.Context) error, err error) {
	cfg := &config{
		exporterEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		insecure:         true,
		serviceVersion:   "dev",
		environment:      "development",
		prometheusPort:   9090,
	}
	if envName := os.Getenv("OTEL_SERVICE_NAME"); envName != "" && serviceName == "" {
		serviceName = envName
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

func initTracerProvider(ctx context.Context, serviceName string, cfg *config) (*sdktrace.TracerProvider, error) {
	resource, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"",
			attribute.String("service.name", serviceName),
			attribute.String("service.version", cfg.serviceVersion),
			attribute.String("deployment.environment", cfg.environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}

	sampler := sdktrace.AlwaysSample()
	if cfg.environment == "production" {
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))
	}

	// When endpoint is empty, use noop tracer (no OTLP exporter)
	if cfg.exporterEndpoint == "" {
		return sdktrace.NewTracerProvider(
			sdktrace.WithResource(resource),
			sdktrace.WithSampler(sampler),
		), nil
	}

	// WithEndpoint expects "host:port"; strip scheme from URL if present
	endpoint := cfg.exporterEndpoint
	if u, err := url.Parse(cfg.exporterEndpoint); err == nil && u.Host != "" {
		endpoint = u.Host
	} else if strings.HasPrefix(cfg.exporterEndpoint, "http://") {
		endpoint = strings.TrimPrefix(cfg.exporterEndpoint, "http://")
	} else if strings.HasPrefix(cfg.exporterEndpoint, "https://") {
		endpoint = strings.TrimPrefix(cfg.exporterEndpoint, "https://")
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
	}
	if cfg.insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otlp trace exporter: %w", err)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource),
		sdktrace.WithSampler(sampler),
	), nil
}

func initMeterProvider(ctx context.Context, serviceName string, cfg *config) (*metric.MeterProvider, error) {
	reg := promclient.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(reg))
	if err != nil {
		return nil, fmt.Errorf("prometheus exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"",
			attribute.String("service.name", serviceName),
			attribute.String("service.version", cfg.serviceVersion),
			attribute.String("deployment.environment", cfg.environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}

	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(exporter),
	)

	// Start Prometheus HTTP server for scraping
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		addr := fmt.Sprintf(":%d", cfg.prometheusPort)
		_ = http.ListenAndServe(addr, mux)
	}()

	return mp, nil
}

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
