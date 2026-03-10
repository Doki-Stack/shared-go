package httpclient

import (
	"net/http"
	"time"

	"github.com/doki-stack/shared-go/breaker"
)

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

// BackoffStrategy returns the delay before retry attempt N (0-indexed).
type BackoffStrategy func(attempt int) time.Duration

// WithTimeout sets the per-request timeout (default: 30s).
func WithTimeout(d time.Duration) Option {
	return func(c *clientConfig) {
		c.timeout = d
	}
}

// WithRetries sets the max retry attempts (default: 3).
func WithRetries(n int) Option {
	return func(c *clientConfig) {
		c.retries = n
	}
}

// WithBackoff sets a custom backoff strategy (default: exponential with jitter).
func WithBackoff(fn BackoffStrategy) Option {
	return func(c *clientConfig) {
		c.backoff = fn
	}
}

// WithCircuitBreaker sets the circuit breaker for fail-fast when downstream is unhealthy.
func WithCircuitBreaker(cb *breaker.CircuitBreaker) Option {
	return func(c *clientConfig) {
		c.breaker = cb
	}
}

// WithTracing enables OpenTelemetry span per request (default: true).
func WithTracing(enabled bool) Option {
	return func(c *clientConfig) {
		c.tracing = enabled
	}
}

// WithOrgIDPropagation enables X-Org-Id header propagation from context (default: true).
func WithOrgIDPropagation(enabled bool) Option {
	return func(c *clientConfig) {
		c.orgIDPropagation = enabled
	}
}

// WithMaxResponseSize sets the max response body size in bytes (default: 10MB; 0 = no limit).
func WithMaxResponseSize(n int64) Option {
	return func(c *clientConfig) {
		c.maxResponseSize = n
	}
}

// WithTransport sets a custom http.RoundTripper.
func WithTransport(rt http.RoundTripper) Option {
	return func(c *clientConfig) {
		c.transport = rt
	}
}
