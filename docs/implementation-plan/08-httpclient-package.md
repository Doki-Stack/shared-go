# shared-go Implementation Plan — HTTP Client Package

## 1. Overview

The `httpclient` package provides a robust HTTP client with retries, circuit breaker integration, W3C trace context propagation, and X-Org-Id header propagation. It is used by mcp-policy (Ollama embedding calls), api-server (agent-orchestrator calls), ee-governance (Policy MCP calls), ee-dashboards, ee-multi-tenancy, ee-notifications, and mcp-registry (external MCP proxy with 30s timeouts and 1MB response size caps).

**Package name:** `httpclient`

**Critical:** org_id propagation ensures downstream services receive the tenant key. W3C trace context propagation enables distributed tracing across all HTTP calls.

## 2. Files

| File | Purpose |
|------|---------|
| `client.go` | Client type, constructor, Do/Get/Post/Put/Delete methods |
| `options.go` | Option functions for New() |
| `retry.go` | Retry logic, backoff strategy, Retry-After handling |
| `client_test.go` | Unit tests |

## 3. Types (client.go)

```go
package httpclient

import (
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// Client is an HTTP client with retries, circuit breaker, tracing, and org_id propagation.
type Client struct {
	http     *http.Client
	retries  int
	backoff  BackoffStrategy
	breaker  *breaker.CircuitBreaker // nil if not configured
	tracer   trace.Tracer
	orgID    bool   // propagate org_id from context
	maxSize  int64  // max response body size (0 = no limit)
}

// BackoffStrategy returns the delay before retry attempt N (0-indexed).
type BackoffStrategy func(attempt int) time.Duration

// RequestOption modifies an outgoing HTTP request.
type RequestOption func(*http.Request)
```

## 4. Constructor and Options (options.go)

```go
// Option configures the Client.
type Option func(*clientConfig)

type clientConfig struct {
	timeout            time.Duration
	retries            int
	backoff            BackoffStrategy
	breaker            *breaker.CircuitBreaker
	tracing            bool
	orgIDPropagation   bool
	maxResponseSize    int64
	transport          http.RoundTripper
}

// WithTimeout sets the per-request timeout (default: 30s).
func WithTimeout(d time.Duration) Option

// WithRetries sets the max retry attempts (default: 3).
func WithRetries(n int) Option

// WithBackoff sets a custom backoff strategy (default: exponential with jitter).
func WithBackoff(fn BackoffStrategy) Option

// WithCircuitBreaker sets the circuit breaker for fail-fast when downstream is unhealthy.
func WithCircuitBreaker(cb *breaker.CircuitBreaker) Option

// WithTracing enables OpenTelemetry span per request (default: true).
func WithTracing(enabled bool) Option

// WithOrgIDPropagation enables X-Org-Id header propagation from context (default: true).
func WithOrgIDPropagation(enabled bool) Option

// WithMaxResponseSize sets the max response body size in bytes (default: 10MB; mcp-registry uses 1MB).
func WithMaxResponseSize(n int64) Option

// WithTransport sets a custom http.RoundTripper.
func WithTransport(rt http.RoundTripper) Option

// New creates a Client with the given options.
func New(opts ...Option) *Client
```

## 5. Methods (client.go)

```go
// Do executes the request with retries, tracing, org_id propagation, and response size limiting.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error)

// Get performs a GET request.
func (c *Client) Get(ctx context.Context, url string, opts ...RequestOption) (*http.Response, error)

// Post performs a POST request.
func (c *Client) Post(ctx context.Context, url string, body io.Reader, opts ...RequestOption) (*http.Response, error)

// Put performs a PUT request.
func (c *Client) Put(ctx context.Context, url string, body io.Reader, opts ...RequestOption) (*http.Response, error)

// Delete performs a DELETE request.
func (c *Client) Delete(ctx context.Context, url string, opts ...RequestOption) (*http.Response, error)
```

## 6. Retry Logic (retry.go)

### 6.1 Retry Conditions

| Condition | Retry? |
|-----------|--------|
| 429 (Too Many Requests) | Yes |
| 502 (Bad Gateway) | Yes |
| 503 (Service Unavailable) | Yes |
| 504 (Gateway Timeout) | Yes |
| Connection errors (dial, TLS) | Yes |
| Context deadline exceeded | Yes (if retries remain) |
| 4xx (except 429) | No |
| 5xx (except 502/503/504) | No |

### 6.2 Default Backoff

Exponential with jitter:
```
delay = 100ms * 2^attempt + random(0, 100ms)
```

Example: attempt 0 → ~100–200ms, attempt 1 → ~200–300ms, attempt 2 → ~400–500ms.

### 6.3 Retry-After Header

When response includes `Retry-After` header:
- If numeric (seconds): use that duration, capped at 60s
- If HTTP-date: parse and use until that time
- Override backoff when Retry-After is present

### 6.4 Logging

Log each retry attempt with: attempt number, status code, URL, error (if any). Use `logger.FromContext(ctx)` when available.

## 7. Tracing

- Create child span for each request: `httpclient.{method} {host}` (e.g., `httpclient.GET ollama.ai`)
- Inject W3C `traceparent` header into outgoing request
- Record span attributes:
  - `http.method`
  - `http.url`
  - `http.status_code`
  - `http.retry_count` (number of retries performed)
- Record error on span if request fails
- Span ends when response body is fully consumed or closed

## 8. org_id Propagation

- Extract org_id from request context via `middleware.OrgIDFromContext(ctx)`
- If org_id is non-empty and `WithOrgIDPropagation(true)`, set `X-Org-Id` header on outgoing request
- Ensures downstream services receive org_id for multi-tenant routing

## 9. Response Size Cap

- When `maxResponseSize > 0`, wrap response body with `io.LimitReader(resp.Body, maxSize)`
- If response exceeds max size, return error (e.g., `ErrResponseTooLarge`)
- Caller must close response body; the wrapped reader propagates Close

## 10. Consumer Service Patterns

| Service | Timeout | Retries | Max Size | Circuit Breaker | Use Case |
|---------|---------|---------|----------|-----------------|----------|
| mcp-policy | 30s | 3 | 10MB | — | Ollama embedding calls |
| api-server | 30s | 3 | 10MB | — | agent-orchestrator calls |
| ee-governance | 30s | 3 | 10MB | — | Policy MCP calls |
| ee-dashboards | 30s | 3 | 10MB | — | ee-multi-tenancy, ee-compliance, OpenCost, Prometheus |
| ee-multi-tenancy | 30s | 3 | 10MB | — | Auth0, Vault, OpenCost |
| ee-notifications | 30s | 3 | 10MB | — | Slack, Teams, PagerDuty webhooks |
| mcp-registry | 30s | 3 | 1MB | ✓ | External MCP proxy |

## 11. Dependencies

| Dependency | Purpose |
|------------|---------|
| `net/http` | HTTP client |
| `go.opentelemetry.io/otel` | Tracing |
| `go.opentelemetry.io/otel/trace` | Span creation |
| `github.com/doki-stack/shared-go/breaker` | Circuit breaker |
| `github.com/doki-stack/shared-go/middleware` | OrgIDFromContext |
| `github.com/doki-stack/shared-go/logger` | Retry logging |

## 12. Test Plan (client_test.go)

| Test Case | Description |
|-----------|-------------|
| **Successful request** | 200 response returned, no retries |
| **Retry on 503** | Server returns 503, client retries up to max, eventually returns last error |
| **No retry on 400** | 400 response → no retry, return immediately |
| **Backoff timing** | With mock clock or short sleeps, verify backoff increases per attempt |
| **Circuit breaker open** | When breaker is open, Do returns error immediately without calling server |
| **Tracing** | Span created with attributes: http.method, http.url, http.status_code, http.retry_count |
| **org_id propagation** | Context with org_id → X-Org-Id header set on request |
| **Response size limit** | Response body > maxSize → ErrResponseTooLarge or similar |
| **Retry-After header** | 503 with Retry-After: 2 → next retry after ~2s |
| **Get/Post/Put/Delete** | Convenience methods construct correct request and call Do |

### Example Test Snippets

```go
func TestClient_SuccessNoRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := httpclient.New()
	resp, err := client.Get(context.Background(), server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_RetriesOn503(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := httpclient.New(httpclient.WithRetries(5), httpclient.WithBackoff(func(int) time.Duration { return 1 * time.Millisecond }))
	resp, err := client.Get(context.Background(), server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 3, attempts)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestClient_NoRetryOn400(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := httpclient.New(httpclient.WithRetries(3))
	resp, err := client.Get(context.Background(), server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 1, attempts)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestClient_OrgIDPropagation(t *testing.T) {
	var capturedOrgID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOrgID = r.Header.Get("X-Org-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := httpclient.New()
	ctx := middleware.ContextWithOrgID(context.Background(), "org-123")
	resp, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, "org-123", capturedOrgID)
}

func TestClient_ResponseSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 1025)) // 1KB + 1 byte
	}))
	defer server.Close()

	client := httpclient.New(httpclient.WithMaxResponseSize(1024))
	resp, err := client.Get(context.Background(), server.URL)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Error(t, err) // ErrResponseTooLarge or similar
}
```
