# shared-go Implementation Plan — Middleware Package

## 1. Overview

The `middleware` package provides HTTP middleware for the Doki Stack platform: org_id enforcement (multi-tenancy), request ID propagation, structured request logging, and panic recovery. All middleware uses the chi router pattern `func(next http.Handler) http.Handler` and standard `net/http` types.

**Package name:** `middleware`

**Critical:** org_id middleware is the **multi-tenancy enforcement point** (risk register S1). Requests without valid org_id MUST be rejected.

## 2. Files

| File | Purpose |
|------|---------|
| `context.go` | Context keys and helper functions |
| `orgid.go` | OrgID extraction and validation middleware |
| `requestid.go` | Request ID middleware |
| `logger.go` | Request logging middleware |
| `recovery.go` | Panic recovery middleware |
| `middleware_test.go` | Unit tests |

## 3. Context Keys and Helpers (context.go)

```go
package middleware

import "context"

type contextKey string

const (
	OrgIDKey     contextKey = "org_id"
	RequestIDKey contextKey = "request_id"
)

// OrgIDFromContext returns the org_id from context, or empty string if not set.
func OrgIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(OrgIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// RequestIDFromContext returns the request_id from context, or empty string if not set.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(RequestIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ContextWithOrgID returns a copy of ctx with org_id set.
func ContextWithOrgID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, OrgIDKey, orgID)
}

// ContextWithRequestID returns a copy of ctx with request_id set.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}
```

## 4. OrgID Middleware (orgid.go)

```go
package middleware

import (
	"net/http"

	"github.com/doki-stack/shared-go/envelope"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// OrgID extracts and validates org_id, storing it in context.
// Extraction order: (1) X-Org-Id header (primary, set by Kong from JWT), (2) chi URL param :org_id (fallback).
// Rejects with 400 if missing or invalid UUID.
func OrgID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID := r.Header.Get("X-Org-Id")
		if orgID == "" {
			orgID = chi.URLParam(r, "org_id")
		}
		if orgID == "" {
			envelope.WriteJSON(w, http.StatusBadRequest, envelope.New(envelope.BadRequest, "missing or invalid org_id"))
			return
		}
		if _, err := uuid.Parse(orgID); err != nil {
			envelope.WriteJSON(w, http.StatusBadRequest, envelope.New(envelope.BadRequest, "missing or invalid org_id"))
			return
		}
		ctx := ContextWithOrgID(r.Context(), orgID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

**Behavior summary:**
- Extract org_id from `X-Org-Id` header first (Kong sets this from JWT claims)
- Fallback to chi URL param `:org_id` for routes like `/orgs/:org_id/...`
- Validate using `google/uuid.Parse()` — must be valid UUID format
- If missing or invalid: respond 400 with `envelope.New("BAD_REQUEST", "missing or invalid org_id")`, abort
- If valid: store in context via `ContextWithOrgID`, call next

## 5. RequestID Middleware (requestid.go)

```go
package middleware

import (
	"net/http"

	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-Id"

// RequestID extracts or generates a request ID, stores it in context, and sets the response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(requestIDHeader)
		if requestID == "" {
			requestID = uuid.New().String()
		}
		ctx := ContextWithRequestID(r.Context(), requestID)
		w.Header().Set(requestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

**Behavior summary:**
- Extract from `X-Request-Id` header if present
- Generate new UUID if not present
- Store in context and set response header `X-Request-Id`

## 6. Logger Middleware (logger.go)

```go
package middleware

import (
	"net/http"
	"time"

	"github.com/doki-stack/shared-go/logger"
	"go.uber.org/zap"
)

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Logger logs each request with method, path, status, duration, org_id, request_id, trace_id.
func Logger(log *logger.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(wrapped, r)
			duration := time.Since(start)

			ctx := r.Context()
			fields := []zap.Field{
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", wrapped.status),
				zap.Duration("duration", duration),
				zap.String("org_id", OrgIDFromContext(ctx)),
				zap.String("request_id", RequestIDFromContext(ctx)),
			}

			logWithCtx := log.WithContext(ctx)
			switch {
			case wrapped.status >= 500:
				logWithCtx.Error("request completed", append(fields, zap.String("event_type", "http_response"))...)
			case wrapped.status >= 400:
				logWithCtx.Warn("request completed", append(fields, zap.String("event_type", "http_response"))...)
			default:
				logWithCtx.Info("request completed", append(fields, zap.String("event_type", "http_response"))...)
			}
		})
	}
}
```

**Behavior summary:**
- Log each request: method, path, status, duration, org_id, request_id, trace_id (via WithContext)
- Use `log.WithContext(ctx)` to auto-populate trace fields from OTel span
- Log at Info for 2xx/3xx, Warn for 4xx, Error for 5xx
- Custom ResponseWriter wrapper captures status code

## 7. Recovery Middleware (recovery.go)

```go
package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/doki-stack/shared-go/envelope"
	"github.com/doki-stack/shared-go/logger"
	"go.opentelemetry.io/otel/trace"
)

// Recovery recovers from panics, logs the stack trace, and returns 500.
// Imports for recovery.go: fmt, go.uber.org/zap
func Recovery(log *logger.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					stack := string(debug.Stack())
					log.Error("panic recovered", zap.String("panic", fmt.Sprint(err)), zap.String("stack", stack))

					env := envelope.New(envelope.InternalError, "internal server error")
					if span := trace.SpanFromContext(r.Context()); span.SpanContext().IsValid() {
						env = envelope.New(envelope.InternalError, "internal server error",
							envelope.WithTraceID(span.SpanContext().TraceID().String()))
					}
					envelope.WriteJSON(w, http.StatusInternalServerError, env)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
```

**Imports for recovery.go:** Add `fmt`, `go.uber.org/zap` to the above.

**Behavior summary:**
- Recover from panics
- Log panic value and stack trace at Error level
- Respond with 500 using `envelope.New("INTERNAL_ERROR", "internal server error")`
- Include trace_id in error response if OTel span is available

## 8. Middleware Stack Ordering

**Recommended order** (outermost to innermost):

```go
r := chi.NewRouter()
r.Use(middleware.RequestID)   // First: so all logs have request_id
r.Use(middleware.OrgID)       // Second: rejects early if invalid
r.Use(middleware.Logger(log)) // Third: wraps handler timing
r.Use(middleware.Recovery(log)) // Last: catches panics in handlers
```

**Rationale:**
- RequestID first — every subsequent middleware and handler can access it
- OrgID second — fail fast on invalid/missing org_id before any business logic
- Logger third — wraps the handler to capture full request timing
- Recovery last — outermost defer, catches panics from all inner handlers

## 9. Consumer Services

| Service | OrgID | RequestID | Logger | Recovery |
|---------|:-----:|:---------:|:------:|:--------:|
| api-server | ✓ | ✓ | ✓ | ✓ |
| mcp-policy | ✓ | ✓ | ✓ | ✓ |
| mcp-memory | ✓ | ✓ | ✓ | ✓ |
| mcp-registry | ✓ | ✓ | ✓ | ✓ |
| ee-multi-tenancy | ✓ | ✓ | ✓ | ✓ |
| ee-notifications | ✓ | ✓ | ✓ | ✓ |
| ee-governance | ✓ | ✓ | ✓ | ✓ |
| ee-dashboards | ✓ | ✓ | ✓ | ✓ |

All consumer services MUST use OrgID middleware — org_id is the tenant key everywhere.

## 10. Full API Surface Summary

```go
// Context keys and helpers (context.go)
type contextKey string
const OrgIDKey, RequestIDKey contextKey
func OrgIDFromContext(ctx context.Context) string
func RequestIDFromContext(ctx context.Context) string
func ContextWithOrgID(ctx context.Context, orgID string) context.Context
func ContextWithRequestID(ctx context.Context, requestID string) context.Context

// Middleware (chi pattern: func(next http.Handler) http.Handler)
func OrgID(next http.Handler) http.Handler
func RequestID(next http.Handler) http.Handler
func Logger(log *logger.Logger) func(next http.Handler) http.Handler
func Recovery(log *logger.Logger) func(next http.Handler) http.Handler
```

## 11. Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/doki-stack/shared-go/envelope` | Error responses (WriteJSON, New, BadRequest, InternalError) |
| `github.com/doki-stack/shared-go/logger` | Structured logging |
| `github.com/go-chi/chi/v5` | URLParam for org_id fallback |
| `github.com/google/uuid` | UUID validation and generation |
| `go.opentelemetry.io/otel/trace` | TraceID for recovery response |
| `go.uber.org/zap` | Log fields |
| `net/http` | Handler, ResponseWriter |
| `context` | Context helpers |

## 12. Test Plan (middleware_test.go)

### 12.1 OrgID Middleware

| Test Case | Description |
|-----------|-------------|
| **Valid header** | Request with `X-Org-Id: a0000000-0000-0000-0000-000000000001` → 200, org_id in context |
| **Valid URL param** | Route `/orgs/:org_id/foo`, request `/orgs/a0000000-0000-0000-0000-000000000001/foo` without header → 200, org_id in context |
| **Header overrides param** | Both present → header wins |
| **Missing** | No header, no URL param → 400, envelope BAD_REQUEST |
| **Invalid UUID** | `X-Org-Id: not-a-uuid` → 400, envelope BAD_REQUEST |
| **Empty string** | `X-Org-Id: ""` → 400 |

### 12.2 RequestID Middleware

| Test Case | Description |
|-----------|-------------|
| **With header** | `X-Request-Id: abc-123` → same value in context and response header |
| **Without header** | No header → new UUID generated, set in context and response header |
| **Response header set** | Verify `X-Request-Id` is always present in response |

### 12.3 Logger Middleware

| Test Case | Description |
|-----------|-------------|
| **200 response** | Logged at Info level |
| **404 response** | Logged at Warn level |
| **500 response** | Logged at Error level |
| **Fields present** | method, path, status, duration, org_id, request_id in log output |

### 12.4 Recovery Middleware

| Test Case | Description |
|-----------|-------------|
| **Panic caught** | Handler panics → 500 returned, no panic propagates |
| **Panic logged** | Stack trace logged at Error |
| **Trace ID in response** | When OTel span present, trace_id in envelope |

### 12.5 Context Helpers

| Test Case | Description |
|-----------|-------------|
| **Round-trip set/get** | ContextWithOrgID → OrgIDFromContext returns same value |
| **Round-trip RequestID** | ContextWithRequestID → RequestIDFromContext returns same value |
| **Nil context** | OrgIDFromContext(nil), RequestIDFromContext(nil) return "" |
| **Missing key** | Empty context → "" |

### 12.6 Example Test Snippets

```go
func TestOrgID_ValidHeader(t *testing.T) {
	orgID := "a0000000-0000-0000-0000-000000000001"
	var capturedCtx context.Context
	handler := middleware.OrgID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Org-Id", orgID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, orgID, middleware.OrgIDFromContext(capturedCtx))
}

func TestOrgID_InvalidUUID(t *testing.T) {
	handler := middleware.OrgID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Org-Id", "not-a-uuid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var env envelope.Envelope
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
	require.Equal(t, envelope.BadRequest, env.ErrorCode)
}

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	var capturedID string
	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = middleware.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.NotEmpty(t, capturedID)
	require.Equal(t, capturedID, rec.Header().Get("X-Request-Id"))
	// Verify it's a valid UUID
	_, err := uuid.Parse(capturedID)
	require.NoError(t, err)
}

func TestRecovery_PanicReturns500(t *testing.T) {
	log, _ := logger.New("test")
	handler := middleware.Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var env envelope.Envelope
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
	require.Equal(t, envelope.InternalError, env.ErrorCode)
}
```
