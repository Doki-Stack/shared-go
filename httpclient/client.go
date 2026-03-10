package httpclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/doki-stack/shared-go/breaker"
	"github.com/doki-stack/shared-go/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultTimeout        = 30 * time.Second
	defaultRetries        = 3
	defaultMaxResponseSize = 10 * 1024 * 1024 // 10MB
)

// Client is an HTTP client with retries, circuit breaker, tracing, and org_id propagation.
type Client struct {
	http     *http.Client
	retries  int
	backoff  BackoffStrategy
	breaker  *breaker.CircuitBreaker
	tracing  bool
	orgID    bool
	maxSize  int64
	tracer   trace.Tracer
}

// RequestOption modifies an outgoing HTTP request.
type RequestOption func(*http.Request)

// New creates a Client with the given options.
func New(opts ...Option) *Client {
	cfg := &clientConfig{
		timeout:          defaultTimeout,
		retries:          defaultRetries,
		backoff:          ExponentialBackoff,
		breaker:         nil,
		tracing:         true,
		orgIDPropagation: true,
		maxResponseSize: defaultMaxResponseSize,
		transport:       nil,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	transport := cfg.transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	return &Client{
		http: &http.Client{
			Timeout:   cfg.timeout,
			Transport: transport,
		},
		retries: cfg.retries,
		backoff: cfg.backoff,
		breaker: cfg.breaker,
		tracing: cfg.tracing,
		orgID:   cfg.orgIDPropagation,
		maxSize: cfg.maxResponseSize,
		tracer:  otel.Tracer("github.com/doki-stack/shared-go/httpclient"),
	}
}

// Do executes the request with retries, tracing, org_id propagation, and response size limiting.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.WithContext(ctx)

	// Apply org_id propagation
	if c.orgID {
		if orgID := middleware.OrgIDFromContext(ctx); orgID != "" {
			req.Header.Set("X-Org-Id", orgID)
		}
	}

	// Apply tracing
	if c.tracing {
		spanName := "httpclient." + req.Method + " " + req.URL.Host
		ctx, span := c.tracer.Start(ctx, spanName)
		defer span.End()

		// Inject W3C trace context
		otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

		span.SetAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.url", req.URL.String()),
		)

		resp, err := c.do(ctx, req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
		return resp, nil
	}

	return c.do(ctx, req)
}

func (c *Client) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if c.breaker != nil {
		result, err := c.breaker.Execute(func() (interface{}, error) {
			return c.doWithRetry(ctx, req)
		})
		if err != nil {
			return nil, err
		}
		return result.(*http.Response), nil
	}
	return c.doWithRetry(ctx, req)
}

// Get performs a GET request.
func (c *Client) Get(ctx context.Context, url string, opts ...RequestOption) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(req)
	}
	return c.Do(ctx, req)
}

// Post performs a POST request.
func (c *Client) Post(ctx context.Context, url string, body io.Reader, opts ...RequestOption) (*http.Response, error) {
	return c.requestWithBody(ctx, http.MethodPost, url, body, opts...)
}

// Put performs a PUT request.
func (c *Client) Put(ctx context.Context, url string, body io.Reader, opts ...RequestOption) (*http.Response, error) {
	return c.requestWithBody(ctx, http.MethodPut, url, body, opts...)
}

// Delete performs a DELETE request.
func (c *Client) Delete(ctx context.Context, url string, opts ...RequestOption) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(req)
	}
	return c.Do(ctx, req)
}

// requestWithBody creates a request with body and sets GetBody for retry support.
func (c *Client) requestWithBody(ctx context.Context, method, url string, body io.Reader, opts ...RequestOption) (*http.Response, error) {
	var bodyBytes []byte
	var err error
	if body != nil {
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return nil, err
		}
	}

	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	if len(bodyBytes) > 0 {
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}

	for _, opt := range opts {
		opt(req)
	}
	return c.Do(ctx, req)
}
