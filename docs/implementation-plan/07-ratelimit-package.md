# shared-go Implementation Plan — Ratelimit Package

## 1. Overview

The `ratelimit` package provides in-process rate limiting using the token bucket algorithm (`golang.org/x/time/rate`). It includes a simple limiter, a keyed limiter for per-entity limits (e.g., per org_id+user_id), and HTTP middleware that returns 429 with `Retry-After` when limited.

**Package name:** `ratelimit`

**Note:** This package provides **in-process** rate limiting only. For distributed rate limiting (e.g., api-server with Dragonfly/Redis), the consumer service implements its own Dragonfly-backed limiter. shared-go provides the local fallback and HTTP middleware wrapper pattern.

## 2. Files

| File | Purpose |
|------|---------|
| `limiter.go` | Simple Limiter wrapping rate.Limiter |
| `keyed.go` | KeyedLimiter for per-key limits with TTL cleanup |
| `middleware.go` | HTTP middleware for simple and keyed limiters |
| `ratelimit_test.go` | Unit tests |

## 3. Simple Limiter (limiter.go)

```go
package ratelimit

import (
	"context"
	"sync"

	"golang.org/x/time/rate"
)

// Limiter wraps rate.Limiter for simple token-bucket rate limiting.
type Limiter struct {
	mu      sync.Mutex
	limiter *rate.Limiter
}

// NewLimiter creates a Limiter with rate r (tokens per second) and burst size.
// Example: NewLimiter(10, 20) allows 10 req/s with bursts up to 20.
func NewLimiter(r float64, burst int) *Limiter {
	return &Limiter{
		limiter: rate.NewLimiter(rate.Limit(r), burst),
	}
}

// Allow reports whether one token is available without blocking.
// Returns true if allowed, false if rate limited.
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limiter.Allow()
}

// Wait blocks until one token is available or ctx is done.
// Returns nil if allowed, ctx.Err() if context cancelled.
func (l *Limiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	limiter := l.limiter
	l.mu.Unlock()
	return limiter.Wait(ctx)
}
```

**Note:** `rate.Limiter` is safe for concurrent use; the mutex here protects against external modification of the limiter reference. For read-only Allow/Wait, `rate.Limiter` is already goroutine-safe. The mutex can be omitted if the Limiter is never replaced; it's kept for consistency with potential future changes.

**Simplified (rate.Limiter is concurrent-safe):**

```go
// Limiter wraps rate.Limiter for simple token-bucket rate limiting.
type Limiter struct {
	limiter *rate.Limiter
}

func NewLimiter(r float64, burst int) *Limiter {
	return &Limiter{limiter: rate.NewLimiter(rate.Limit(r), burst)}
}

func (l *Limiter) Allow() bool {
	return l.limiter.Allow()
}

func (l *Limiter) Wait(ctx context.Context) error {
	return l.limiter.Wait(ctx)
}
```

Use the simplified version — `rate.Limiter` is documented as safe for concurrent use.

## 4. Keyed Limiter (keyed.go)

```go
package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// KeyedOption configures KeyedLimiter.
type KeyedOption func(*keyedConfig)

type keyedConfig struct {
	ttl             time.Duration
	cleanupInterval time.Duration
}

// WithTTL sets the expiry for unused keys (default: 10 minutes).
func WithTTL(d time.Duration) KeyedOption {
	return func(c *keyedConfig) {
		c.ttl = d
	}
}

// WithCleanupInterval sets the interval for cleanup goroutine (default: 1 minute).
func WithCleanupInterval(d time.Duration) KeyedOption {
	return func(c *keyedConfig) {
		c.cleanupInterval = d
	}
}

type keyedEntry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// KeyedLimiter provides per-key rate limiting with automatic cleanup of unused keys.
type KeyedLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*keyedEntry
	rate     float64
	burst    int
	ttl      time.Duration
	stopCh   chan struct{}
}

// NewKeyedLimiter creates a KeyedLimiter with rate r and burst per key.
func NewKeyedLimiter(r float64, burst int, opts ...KeyedOption) *KeyedLimiter {
	cfg := &keyedConfig{
		ttl:             10 * time.Minute,
		cleanupInterval: 1 * time.Minute,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	kl := &KeyedLimiter{
		limiters: make(map[string]*keyedEntry),
		rate:     r,
		burst:    burst,
		ttl:      cfg.ttl,
		stopCh:   make(chan struct{}),
	}
	go kl.cleanup(cfg.cleanupInterval)
	return kl
}

// Stop stops the cleanup goroutine. Call when shutting down.
func (kl *KeyedLimiter) Stop() {
	close(kl.stopCh)
}

func (kl *KeyedLimiter) getOrCreate(key string) *rate.Limiter {
	kl.mu.Lock()
	defer kl.mu.Unlock()
	if e, ok := kl.limiters[key]; ok {
		e.lastUsed = time.Now()
		return e.limiter
	}
	e := &keyedEntry{
		limiter:  rate.NewLimiter(rate.Limit(kl.rate), kl.burst),
		lastUsed: time.Now(),
	}
	kl.limiters[key] = e
	return e.limiter
}

// Allow reports whether one token is available for the key.
func (kl *KeyedLimiter) Allow(key string) bool {
	limiter := kl.getOrCreate(key)
	return limiter.Allow()
}

// Wait blocks until one token is available for the key or ctx is done.
func (kl *KeyedLimiter) Wait(ctx context.Context, key string) error {
	limiter := kl.getOrCreate(key)
	return limiter.Wait(ctx)
}

// Reserve returns a reservation for one token. Use for accurate Retry-After.
// When rejecting a request, call reservation.Cancel() to avoid consuming the token.
func (kl *KeyedLimiter) Reserve(key string) *rate.Reservation {
	limiter := kl.getOrCreate(key)
	return limiter.Reserve()
}

// cleanup periodically removes expired entries.
func (kl *KeyedLimiter) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-kl.stopCh:
			return
		case <-ticker.C:
			kl.mu.Lock()
			now := time.Now()
			for k, e := range kl.limiters {
				if now.Sub(e.lastUsed) > kl.ttl {
					delete(kl.limiters, k)
				}
			}
			kl.mu.Unlock()
		}
	}
}
```

## 5. Key Patterns by Service

| Service | Key Format | Example | Rate (typical) |
|---------|------------|---------|----------------|
| api-server | `api:{org_id}:{user_id}` | `api:a0000000-0000-0000-0000-000000000001:user-123` | 100 req/min |
| ee-notifications | `notify:{org_id}:{user_id}` | `notify:org-1:user-456` | Configurable |
| mcp-registry | `mcp:{org_id}:{mcp_id}` | `mcp:org-1:mcp-policy` | 100 req/min per MCP |

**Note:** api-server uses Dragonfly for distributed limiting; the key format above is the pattern. shared-go KeyedLimiter is for single-instance or fallback.

## 6. HTTP Middleware (middleware.go)

```go
package ratelimit

import (
	"net/http"
	"strconv"

	"github.com/doki-stack/shared-go/envelope"
	"github.com/doki-stack/shared-go/middleware"
)

const retryAfterHeader = "Retry-After"

// Middleware returns middleware that rate limits using the simple Limiter.
// Responds with 429 and envelope RATE_LIMITED when limited.
func Middleware(limiter *Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				env := envelope.New(envelope.RateLimited, "rate limit exceeded")
				w.Header().Set(retryAfterHeader, "1") // Conservative: 1 second
				envelope.WriteJSON(w, http.StatusTooManyRequests, env)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// KeyedMiddleware returns middleware that rate limits per key from keyFunc.
// Uses Reserve() for accurate Retry-After header. When rate limited, calls
// reservation.Cancel() so the token is not consumed.
func KeyedMiddleware(kl *KeyedLimiter, keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			res := kl.Reserve(key)
			if res.Delay() > 0 {
				res.Cancel()
				secs := int(res.Delay().Seconds()) + 1
				if secs < 1 {
					secs = 1
				}
				env := envelope.New(envelope.RateLimited, "rate limit exceeded")
				w.Header().Set(retryAfterHeader, strconv.Itoa(secs))
				envelope.WriteJSON(w, http.StatusTooManyRequests, env)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// DefaultKeyFunc extracts org_id from context and user_id from X-User-Id header.
// Key format: {org_id}:{user_id}. Returns empty string if org_id missing (pass-through).
func DefaultKeyFunc(r *http.Request) string {
	orgID := middleware.OrgIDFromContext(r.Context())
	if orgID == "" {
		return ""
	}
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		userID = "anonymous"
	}
	return orgID + ":" + userID
}
```

## 7. Dragonfly/Redis-Backed Limiting

This package provides **in-process** rate limiting only. For distributed rate limiting (api-server uses Dragonfly), the api-server implements its own Dragonfly-backed limiter. The shared-go package provides:

- The local fallback when Dragonfly is unavailable
- The HTTP middleware wrapper pattern (429, Retry-After, envelope)
- The key format convention (`{org_id}:{user_id}`)

Consumers needing distributed limits use this package's interfaces as a pattern and implement a Dragonfly/Redis-backed limiter that satisfies the same middleware signature.

## 8. Full API Surface Summary

```go
// Simple Limiter (limiter.go)
func NewLimiter(r float64, burst int) *Limiter
func (l *Limiter) Allow() bool
func (l *Limiter) Wait(ctx context.Context) error

// Keyed Limiter (keyed.go)
func NewKeyedLimiter(r float64, burst int, opts ...KeyedOption) *KeyedLimiter
func WithTTL(d time.Duration) KeyedOption
func WithCleanupInterval(d time.Duration) KeyedOption
func (kl *KeyedLimiter) Allow(key string) bool
func (kl *KeyedLimiter) Wait(ctx context.Context, key string) error
func (kl *KeyedLimiter) Reserve(key string) *rate.Reservation
func (kl *KeyedLimiter) Stop()

// Middleware (middleware.go)
func Middleware(limiter *Limiter) func(http.Handler) http.Handler
func KeyedMiddleware(kl *KeyedLimiter, keyFunc func(r *http.Request) string) func(http.Handler) http.Handler
func DefaultKeyFunc(r *http.Request) string
```

## 9. Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/doki-stack/shared-go/envelope` | RATE_LIMITED error, WriteJSON |
| `github.com/doki-stack/shared-go/middleware` | OrgIDFromContext for DefaultKeyFunc |
| `golang.org/x/time/rate` | Token bucket limiter |
| `net/http` | Handler, Request |
| `context` | Wait cancellation |
| `sync` | KeyedLimiter map protection |
| `time` | TTL, cleanup interval |

## 10. Test Plan (ratelimit_test.go)

| Test Case | Description |
|-----------|-------------|
| **Simple limiter** | Allow up to burst, then deny; Wait blocks until token available |
| **Keyed limiter** | Independent limits per key; key "a" and key "b" don't affect each other |
| **TTL cleanup** | Expired keys removed after TTL; verify map size decreases |
| **Middleware 429** | When limited, response is 429 with RATE_LIMITED envelope |
| **Retry-After header** | When limited, Retry-After set with seconds until next token |
| **Concurrent access** | KeyedLimiter Allow/Reserve from multiple goroutines; no race |
| **DefaultKeyFunc** | With org_id in context and X-User-Id header → `{org_id}:{user_id}` |
| **DefaultKeyFunc no org** | No org_id in context → empty string (pass-through) |
| **Reserve then Cancel** | Reserve, Delay > 0, Cancel — token not consumed, next Allow succeeds after refill |

### Example Test Snippets

```go
func TestLimiter_AllowBurstThenDeny(t *testing.T) {
	l := NewLimiter(10, 2)
	require.True(t, l.Allow())
	require.True(t, l.Allow())
	require.False(t, l.Allow())
}

func TestKeyedLimiter_IndependentKeys(t *testing.T) {
	kl := NewKeyedLimiter(1, 1, WithTTL(time.Minute))
	require.True(t, kl.Allow("key1"))
	require.False(t, kl.Allow("key1"))
	require.True(t, kl.Allow("key2"))
}

func TestMiddleware_Returns429WhenLimited(t *testing.T) {
	l := NewLimiter(0.001, 1)
	handler := Middleware(l)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	require.Equal(t, http.StatusOK, rec1.Code)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
	require.NotEmpty(t, rec2.Header().Get("Retry-After"))
	var env envelope.Envelope
	require.NoError(t, json.NewDecoder(rec2.Body).Decode(&env))
	require.Equal(t, envelope.RateLimited, env.ErrorCode)
}

func TestDefaultKeyFunc(t *testing.T) {
	ctx := middleware.ContextWithOrgID(context.Background(), "org-123")
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ctx)
	req.Header.Set("X-User-Id", "user-456")
	require.Equal(t, "org-123:user-456", DefaultKeyFunc(req))
}
</think>
Adding the `Reserve` method to `KeyedLimiter` for accurate `Retry-After` and updating the middleware section.
<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>
StrReplace