# shared-go Implementation Plan — Breaker Package

## 1. Overview

The `breaker` package provides a circuit breaker wrapper around `github.com/sony/gobreaker/v2`. It enables fail-fast behavior when downstream services (e.g., Qdrant, MCP proxy) are unhealthy, supporting ADR-005 fail-closed policy enforcement.

**Package name:** `breaker`

**Critical:** When the Policy MCP's Qdrant dependency is unavailable, the system MUST fail closed (return 503) rather than proceed without policy context.

## 2. Files

| File | Purpose |
|------|---------|
| `breaker.go` | CircuitBreaker type, constructor, methods |
| `breaker_test.go` | Unit tests |

## 3. Types

```go
package breaker

import "github.com/sony/gobreaker/v2"

// CircuitBreaker wraps gobreaker.CircuitBreaker with a name and configurable options.
type CircuitBreaker struct {
	cb   *gobreaker.CircuitBreaker
	name string
}

// State is the circuit breaker state (alias for gobreaker.State).
type State = gobreaker.State

const (
	StateClosed   = gobreaker.StateClosed
	StateHalfOpen = gobreaker.StateHalfOpen
	StateOpen     = gobreaker.StateOpen
)
```

## 4. Constructor and Options

```go
// Option configures the circuit breaker.
type Option func(*breakerConfig)

type breakerConfig struct {
	maxRequests   uint32
	interval      time.Duration
	timeout       time.Duration
	readyToTrip   func(gobreaker.Counts) bool
	onStateChange func(name string, from, to State)
}

// WithMaxRequests sets the max number of requests allowed in half-open state (default: 1).
func WithMaxRequests(n uint32) Option {
	return func(c *breakerConfig) {
		c.maxRequests = n
	}
}

// WithInterval sets the cyclic period of the closed state for clearing internal counts (default: 0 = never clear).
func WithInterval(d time.Duration) Option {
	return func(c *breakerConfig) {
		c.interval = d
	}
}

// WithTimeout sets the period of the open state after which the state becomes half-open (default: 60s).
func WithTimeout(d time.Duration) Option {
	return func(c *breakerConfig) {
		c.timeout = d
	}
}

// WithReadyToTrip sets the custom function that determines if the breaker should trip (default: 5 consecutive failures).
func WithReadyToTrip(fn func(gobreaker.Counts) bool) Option {
	return func(c *breakerConfig) {
		c.readyToTrip = fn
	}
}

// WithOnStateChange sets the callback invoked when the breaker state changes.
func WithOnStateChange(fn func(name string, from, to State)) Option {
	return func(c *breakerConfig) {
		c.onStateChange = fn
	}
}

// New creates a CircuitBreaker with the given name and options.
func New(name string, opts ...Option) *CircuitBreaker {
	cfg := &breakerConfig{
		maxRequests: 1,
		interval:    0,
		timeout:     60 * time.Second,
		readyToTrip: func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= 5
		},
		onStateChange: nil,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	settings := gobreaker.Settings{
		Name:        name,
		MaxRequests: cfg.maxRequests,
		Interval:    cfg.interval,
		Timeout:     cfg.timeout,
		ReadyToTrip: cfg.readyToTrip,
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			if cfg.onStateChange != nil {
				cfg.onStateChange(name, from, to)
			}
		},
	}

	cb := gobreaker.NewCircuitBreaker(settings)
	return &CircuitBreaker{cb: cb, name: name}
}
```

## 5. Methods

```go
// Execute runs the given function through the circuit breaker.
// Returns gobreaker.ErrOpenState when the circuit is open (fail-fast).
func (cb *CircuitBreaker) Execute(fn func() (interface{}, error)) (interface{}, error) {
	return cb.cb.Execute(fn)
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() State {
	return cb.cb.State()
}

// Name returns the circuit breaker name.
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// Counts returns the current failure/success counts.
func (cb *CircuitBreaker) Counts() gobreaker.Counts {
	return cb.cb.Counts()
}
```

## 6. Default Behavior

| Setting | Default | Description |
|---------|---------|-------------|
| MaxRequests | 1 | Max requests allowed in half-open state (probe) |
| Interval | 0 | Closed-state cycle interval (0 = never clear counts) |
| Timeout | 60s | Open-state duration before transitioning to half-open |
| ReadyToTrip | 5 consecutive failures | Customizable; default trips after 5 failures |

**State machine:**
- **Closed** → 5 consecutive failures → **Open**
- **Open** → 60s timeout → **HalfOpen**
- **HalfOpen** → 1 successful request → **Closed**
- **HalfOpen** → 1 failed request → **Open**

When **Open**: `Execute` returns `gobreaker.ErrOpenState` immediately (fail-fast, no call to fn).

## 7. Usage Patterns by Service

### 7.1 mcp-policy (Qdrant)

```go
qdrantBreaker := breaker.New("qdrant",
	breaker.WithTimeout(30*time.Second),
	breaker.WithOnStateChange(func(name string, from, to breaker.State) {
		log.Warn("circuit breaker state change", zap.String("name", name),
			zap.String("from", from.String()), zap.String("to", to.String()))
	}),
)

result, err := qdrantBreaker.Execute(func() (interface{}, error) {
	return qdrantClient.Search(ctx, query)
})
if err != nil {
	// Circuit open OR Qdrant error → fail closed (ADR-005)
	return nil, envelope.New(envelope.PolicyUnavailable, "policy context unavailable",
		envelope.WithRetryable(true))
}
```

### 7.2 mcp-registry (MCP proxy)

```go
mcpBreaker := breaker.New("mcp-proxy")
resp, err := mcpBreaker.Execute(func() (interface{}, error) {
	return mcpClient.Call(ctx, tool, args)
})
if err != nil {
	// Isolate unhealthy MCPs
	return nil, envelope.New(envelope.InternalError, "MCP proxy unavailable",
		envelope.WithRetryable(true))
}
```

### 7.3 mcp-memory (Qdrant)

Same pattern as mcp-policy — breaker on Qdrant calls for memory operations.

## 8. Fail-Closed Pattern (ADR-005)

When Policy MCP depends on Qdrant and Qdrant is down:

1. Circuit breaker opens after 5 consecutive failures
2. Subsequent calls return `gobreaker.ErrOpenState` immediately
3. Service returns 503 with `POLICY_UNAVAILABLE` and `retryable: true`
4. System blocks rather than proceeding without policy context

```go
result, err := qdrantBreaker.Execute(func() (interface{}, error) {
	return qdrantClient.Search(ctx, query)
})
if err != nil {
	if errors.Is(err, gobreaker.ErrOpenState) {
		// Circuit open — fail closed
		return nil, envelope.New(envelope.PolicyUnavailable, "policy context unavailable",
			envelope.WithRetryable(true))
	}
	// Actual Qdrant error — also fail closed
	return nil, envelope.New(envelope.PolicyUnavailable, "policy context unavailable",
		envelope.WithRetryable(true))
}
```

## 9. Observability

- **State change logging**: Use `WithOnStateChange` to log transitions (closed→open, open→half-open, half-open→closed)
- **Health checks**: Include circuit breaker state in health check response. When any critical breaker (e.g., Qdrant) is open, health can report degraded or unhealthy.

Example health integration:

```go
func (s *Service) HealthCheck() map[string]interface{} {
	state := qdrantBreaker.State()
	return map[string]interface{}{
		"qdrant_breaker": state.String(),
		"status":         map[breaker.State]string{
			breaker.StateClosed:   "healthy",
			breaker.StateHalfOpen: "degraded",
			breaker.StateOpen:     "unhealthy",
		}[state],
	}
}
```

## 10. Full API Surface Summary

```go
// Types
type CircuitBreaker struct { ... }
type State = gobreaker.State
const StateClosed, StateHalfOpen, StateOpen State

// Constructor
func New(name string, opts ...Option) *CircuitBreaker

// Options
func WithMaxRequests(n uint32) Option
func WithInterval(d time.Duration) Option
func WithTimeout(d time.Duration) Option
func WithReadyToTrip(fn func(gobreaker.Counts) bool) Option
func WithOnStateChange(fn func(name string, from, to State)) Option

// Methods
func (cb *CircuitBreaker) Execute(fn func() (interface{}, error)) (interface{}, error)
func (cb *CircuitBreaker) State() State
func (cb *CircuitBreaker) Name() string
func (cb *CircuitBreaker) Counts() gobreaker.Counts
```

## 11. Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/sony/gobreaker/v2` | Circuit breaker implementation |
| `time` | Duration for timeout, interval |

**No internal shared-go dependencies.** The breaker package is a foundation layer. Consumers may use `envelope` and `logger` when handling Execute errors.

## 12. Test Plan (breaker_test.go)

| Test Case | Description |
|-----------|-------------|
| **State transitions** | Closed → Open after 5 consecutive failures |
| **Half-open behavior** | When open, after timeout, single probe request allowed |
| **Recovery** | Successful probe in half-open → transitions to closed |
| **Custom trip function** | WithReadyToTrip(fn) — verify custom logic used |
| **Execute when open** | Returns gobreaker.ErrOpenState, fn not called |
| **Execute when closed** | fn called, result/error returned |
| **State change callback** | WithOnStateChange — verify callback fires on transition |
| **Counts** | Verify Counts() returns correct ConsecutiveFailures, Successes |
| **Name** | Verify Name() returns constructor arg |

### Example Test Snippets

```go
func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	cb := breaker.New("test", breaker.WithReadyToTrip(func(c gobreaker.Counts) bool {
		return c.ConsecutiveFailures >= 3
	}))
	for i := 0; i < 3; i++ {
		_, err := cb.Execute(func() (interface{}, error) {
			return nil, errors.New("fail")
		})
		require.Error(t, err)
	}
	require.Equal(t, breaker.StateOpen, cb.State())
	_, err := cb.Execute(func() (interface{}, error) {
		return "ok", nil
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, gobreaker.ErrOpenState))
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	cb := breaker.New("test",
		breaker.WithReadyToTrip(func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= 1 }),
		breaker.WithTimeout(10*time.Millisecond),
	)
	_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })
	require.Equal(t, breaker.StateOpen, cb.State())
	time.Sleep(15 * time.Millisecond)
	v, err := cb.Execute(func() (interface{}, error) { return "ok", nil })
	require.NoError(t, err)
	require.Equal(t, "ok", v)
	require.Equal(t, breaker.StateClosed, cb.State())
}

func TestCircuitBreaker_OnStateChange(t *testing.T) {
	var transitions []string
	cb := breaker.New("test",
		breaker.WithReadyToTrip(func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= 1 }),
		breaker.WithTimeout(10*time.Millisecond),
		breaker.WithOnStateChange(func(name string, from, to breaker.State) {
			transitions = append(transitions, fmt.Sprintf("%s:%s->%s", name, from, to))
		}),
	)
	_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })
	time.Sleep(15 * time.Millisecond)
	_, _ = cb.Execute(func() (interface{}, error) { return "ok", nil })
	require.Contains(t, transitions, "test:closed->open")
	require.Contains(t, transitions, "test:open->half-open")
	require.Contains(t, transitions, "test:half-open->closed")
}
```
