# shared-go Implementation Plan — Health Package

## 1. Overview

The `health` package provides standardized liveness and readiness HTTP handlers for Kubernetes probes. It supports a composable `Check` interface for dependency verification, enabling the **fail-closed** pattern mandated by ADR-005 — if a critical dependency (Qdrant, embeddings) is unavailable, the readiness probe fails, Kubernetes removes the pod from service, and no requests are served.

**Package name:** `health`

**Critical:** mcp-policy MUST include Qdrant and embedding model checks. If either is down, readiness fails → pod removed → fail closed. This is a non-negotiable architectural requirement.

## 2. Files

| File | Purpose |
|------|---------|
| `handler.go` | Handler factory, liveness/readiness endpoints, Status type |
| `checks.go` | Check interface, common check constructors (Postgres, Qdrant, Dragonfly, RabbitMQ, Vault) |
| `health_test.go` | Unit tests |

## 3. Types (handler.go)

```go
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
)

// Status is the JSON response for health endpoints.
type Status struct {
	Status string            `json:"status"` // "healthy" or "unhealthy"
	Checks map[string]string `json:"checks"` // check name → "ok" or error message
}
```

## 4. Check Interface (checks.go)

```go
// Check verifies the health of a single dependency.
type Check interface {
	Name() string
	Check(ctx context.Context) error
}

// CheckFunc adapts a name + function pair into a Check.
type CheckFunc struct {
	name string
	fn   func(ctx context.Context) error
}

func (c *CheckFunc) Name() string                        { return c.name }
func (c *CheckFunc) Check(ctx context.Context) error     { return c.fn(ctx) }

// NewCheck creates a Check from a name and function.
func NewCheck(name string, fn func(ctx context.Context) error) Check
```

## 5. Handler Factory (handler.go)

```go
// Handler returns an http.Handler that mounts /healthz (liveness) and /readyz (readiness)
// on a chi sub-router. Readiness runs all checks.
func Handler(checks ...Check) http.Handler

// LivenessHandler returns an http.Handler for /healthz that always returns 200.
// Proves the process is alive; no dependency checks.
func LivenessHandler() http.Handler

// ReadinessHandler returns an http.Handler for /readyz that runs all checks.
// Returns 200 if all pass, 503 if any fail.
func ReadinessHandler(checks ...Check) http.Handler
```

### Handler Behavior

**LivenessHandler (`/healthz`)**:
- Always returns HTTP 200
- Response: `{"status": "healthy"}`
- No dependency checks — only proves process is running

**ReadinessHandler (`/readyz`)**:
- Runs all checks concurrently with a 5-second per-check timeout
- Returns HTTP 200 if ALL checks pass
- Returns HTTP 503 if ANY check fails
- Response includes individual check results

**Handler (combined)**:
- Mounts LivenessHandler at `/healthz`
- Mounts ReadinessHandler at `/readyz`
- Returns a chi sub-router that can be mounted on any router

## 6. Common Checks (checks.go)

```go
// PostgresCheck verifies the database is reachable with a SELECT 1 query.
func PostgresCheck(db *sql.DB) Check

// QdrantCheck verifies Qdrant is reachable via its REST API.
func QdrantCheck(url string) Check

// DragonflyCheck verifies the Dragonfly (Redis-compatible) instance is reachable via PING.
func DragonflyCheck(addr string) Check

// RabbitMQCheck verifies RabbitMQ is reachable by dialing an AMQP connection.
func RabbitMQCheck(url string) Check

// VaultCheck verifies HashiCorp Vault is reachable via its health endpoint.
func VaultCheck(addr string) Check

// HTTPCheck verifies any HTTP endpoint is reachable with a GET request.
func HTTPCheck(name, url string) Check
```

### Check Implementation Details

| Check | Method | Timeout | Success Condition |
|-------|--------|---------|-------------------|
| PostgresCheck | `db.PingContext(ctx)` | 5s | Ping returns nil |
| QdrantCheck | `GET {url}/collections` | 5s | HTTP 200 |
| DragonflyCheck | Redis `PING` command | 5s | Response is `PONG` |
| RabbitMQCheck | `amqp.DialConfig(url)` | 5s | Connection established (then closed) |
| VaultCheck | `GET {addr}/v1/sys/health` | 5s | HTTP 200 or 429 (Vault returns 429 when sealed but alive) |
| HTTPCheck | `GET {url}` | 5s | HTTP 2xx |

Each check:
- Wraps the context with `context.WithTimeout(ctx, 5*time.Second)`
- Returns `nil` on success, descriptive `error` on failure
- Error messages are human-readable (e.g., `"connection refused"`, `"timeout after 5s"`)

## 7. Concurrent Check Execution

Readiness runs all checks concurrently using goroutines and `sync.WaitGroup`:

```go
func runChecks(ctx context.Context, checks []Check) Status {
	status := Status{
		Status: "healthy",
		Checks: make(map[string]string, len(checks)),
	}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, check := range checks {
		wg.Add(1)
		go func(c Check) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			err := c.Check(checkCtx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				status.Status = "unhealthy"
				status.Checks[c.Name()] = err.Error()
			} else {
				status.Checks[c.Name()] = "ok"
			}
		}(check)
	}

	wg.Wait()
	return status
}
```

## 8. Response Format

### Healthy (HTTP 200)

```json
{
  "status": "healthy",
  "checks": {
    "postgres": "ok",
    "qdrant": "ok",
    "dragonfly": "ok"
  }
}
```

### Unhealthy (HTTP 503)

```json
{
  "status": "unhealthy",
  "checks": {
    "postgres": "ok",
    "qdrant": "connection refused",
    "dragonfly": "ok"
  }
}
```

### Liveness (HTTP 200, always)

```json
{
  "status": "healthy"
}
```

## 9. Fail-Closed Pattern (ADR-005)

The fail-closed pattern is the most critical use of this package. For `mcp-policy`:

```go
r := chi.NewRouter()

healthHandler := health.Handler(
	health.PostgresCheck(db),
	health.QdrantCheck("http://qdrant.doki-data:6333"),
	health.HTTPCheck("ollama-embeddings", "http://ollama.ai:11434/api/tags"),
)
r.Mount("/", healthHandler)
```

**Flow when Qdrant is down:**
1. Readiness probe (`/readyz`) runs all checks
2. QdrantCheck returns error: `"connection refused"`
3. Readiness returns HTTP 503
4. Kubernetes marks pod as not ready
5. Service removes pod from endpoint list
6. No traffic reaches mcp-policy
7. Upstream callers receive 503 from the service mesh / load balancer
8. System fails closed — no policy evaluation without context

This is required behavior per ADR-005: "if Policy MCP, Qdrant, or embeddings are unavailable, the system blocks."

## 10. Consumer Service Patterns

| Service | Checks | Fail-Closed? |
|---------|--------|--------------|
| api-server | Postgres, Dragonfly, RabbitMQ | No (graceful degradation) |
| mcp-policy | Postgres, Qdrant, Ollama | **Yes** (ADR-005) |
| mcp-memory | Postgres, Qdrant, Dragonfly | Yes (memory unavailable) |
| mcp-registry | Postgres, Vault | Yes (registry unavailable) |
| ee-multi-tenancy | Postgres, Vault, Dragonfly | No |
| ee-notifications | Postgres, RabbitMQ | No |
| ee-compliance | Postgres | No |
| ee-governance | Postgres | No |
| ee-dashboards | Postgres, Dragonfly | No |

## 11. Dependencies

| Dependency | Purpose |
|------------|---------|
| `net/http` | HTTP handler |
| `encoding/json` | JSON response |
| `database/sql` | PostgresCheck |
| `github.com/go-chi/chi/v5` | Sub-router for mounting |
| `context` | Timeout per check |
| `sync` | Concurrent check execution |

**Note:** DragonflyCheck and RabbitMQCheck require additional client libraries (`github.com/redis/go-redis/v9` and `github.com/rabbitmq/amqp091-go`). These are optional dependencies — services that don't use these checks don't import them. Consider using build tags or keeping these as example implementations that consumers copy into their own codebases.

### Optional Dependency Strategy

To avoid pulling Redis and AMQP client libraries into shared-go for services that don't need them:

1. **Core checks** (handler.go, checks.go): PostgresCheck (uses `database/sql`), QdrantCheck (uses `net/http`), VaultCheck (uses `net/http`), HTTPCheck (uses `net/http`)
2. **Optional checks**: DragonflyCheck and RabbitMQCheck are provided as `NewCheck` examples in documentation. Consumers implement them in their own service code using the `Check` interface.

This keeps shared-go's dependency tree minimal while providing the patterns.

## 12. Test Plan (health_test.go)

| Test Case | Description |
|-----------|-------------|
| **All checks pass** | Three passing checks → HTTP 200, `"healthy"`, all `"ok"` |
| **One check fails** | Two pass, one fails → HTTP 503, `"unhealthy"`, failed check shows error |
| **Liveness always 200** | LivenessHandler returns 200 regardless of any state |
| **Check timeout** | Slow check exceeds 5s → treated as failure |
| **Custom check function** | NewCheck with custom fn → Check interface works |
| **Concurrent execution** | Checks run in parallel, not sequentially |
| **No checks** | ReadinessHandler with zero checks → 200 (vacuously healthy) |
| **Fail-closed scenario** | QdrantCheck fails → readiness 503 → simulates ADR-005 |

### Example Test Snippets

```go
func TestReadiness_AllHealthy(t *testing.T) {
	handler := health.ReadinessHandler(
		health.NewCheck("check1", func(ctx context.Context) error { return nil }),
		health.NewCheck("check2", func(ctx context.Context) error { return nil }),
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var status health.Status
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&status))
	require.Equal(t, "healthy", status.Status)
	require.Equal(t, "ok", status.Checks["check1"])
	require.Equal(t, "ok", status.Checks["check2"])
}

func TestReadiness_OneUnhealthy(t *testing.T) {
	handler := health.ReadinessHandler(
		health.NewCheck("db", func(ctx context.Context) error { return nil }),
		health.NewCheck("qdrant", func(ctx context.Context) error {
			return fmt.Errorf("connection refused")
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var status health.Status
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&status))
	require.Equal(t, "unhealthy", status.Status)
	require.Equal(t, "ok", status.Checks["db"])
	require.Equal(t, "connection refused", status.Checks["qdrant"])
}

func TestLiveness_AlwaysHealthy(t *testing.T) {
	handler := health.LivenessHandler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestReadiness_CheckTimeout(t *testing.T) {
	handler := health.ReadinessHandler(
		health.NewCheck("slow", func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
				return nil
			}
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
```
