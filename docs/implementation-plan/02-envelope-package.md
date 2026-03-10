# shared-go Implementation Plan — Envelope Package

## 1. Overview

The `envelope` package provides a standardized error envelope for HTTP API responses across the Doki Stack platform. All error responses use a consistent JSON structure with `error_code`, `message`, `trace_id`, `org_id`, and `retryable` fields.

**Package name:** `envelope` (NOT `errors` — avoids shadowing Go's stdlib `errors` package)

## 2. Files

| File | Purpose |
|------|---------|
| `envelope.go` | Core `Envelope` type, constructor, options |
| `codes.go` | Error code constants by domain |
| `http.go` | HTTP helpers for writing JSON responses |
| `envelope_test.go` | Unit tests |

## 3. Types

### 3.1 Envelope Struct

```go
package envelope

// Envelope is the standard error response structure for all Doki Stack APIs.
// Implements the error interface and json.Marshaler for custom serialization.
type Envelope struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
	TraceID   string `json:"trace_id,omitempty"`
	OrgID     string `json:"org_id,omitempty"`
	Retryable bool   `json:"retryable"`
}

// Error implements the error interface. Returns the message field.
func (e *Envelope) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// MarshalJSON implements json.Marshaler for custom serialization if needed.
// Default: use standard JSON encoding; override only if special handling required.
func (e *Envelope) MarshalJSON() ([]byte, error) {
	// Alias to avoid recursion; use type embedding or explicit struct for marshaling
	type envelope Envelope
	return json.Marshal((*envelope)(e))
}
```

**Note:** If no custom serialization is needed, omit `MarshalJSON` and rely on struct tags. Include it only if the package requires special JSON behavior (e.g., omitting empty strings differently).

## 4. Constructor

```go
// Option configures an Envelope.
type Option func(*Envelope)

func WithTraceID(traceID string) Option {
	return func(e *Envelope) {
		e.TraceID = traceID
	}
}

func WithOrgID(orgID string) Option {
	return func(e *Envelope) {
		e.OrgID = orgID
	}
}

func WithRetryable(retryable bool) Option {
	return func(e *Envelope) {
		e.Retryable = retryable
	}
}

// WithContext extracts trace_id and org_id from context and applies them.
// Expects context keys: "trace_id" (string), "org_id" (string).
// Middleware (e.g., requestid, otel) sets these keys; envelope has no OTel dependency.
func WithContext(ctx context.Context) Option {
	return func(e *Envelope) {
		if ctx == nil {
			return
		}
		if traceID := ctx.Value("trace_id"); traceID != nil {
			if s, ok := traceID.(string); ok {
				e.TraceID = s
			}
		}
		if orgID := ctx.Value("org_id"); orgID != nil {
			if s, ok := orgID.(string); ok {
				e.OrgID = s
			}
		}
	}
}

// New creates an Envelope with the given error code and message.
func New(code, message string, opts ...Option) *Envelope {
	e := &Envelope{
		ErrorCode: code,
		Message:   message,
		Retryable: false,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}
```

**Imports for envelope.go:** `context`, `encoding/json`

## 5. Error Code Constants (codes.go)

Organize by domain. Use `const` blocks for clarity.

```go
package envelope

// Policy domain
const (
	PolicyUnavailable    = "POLICY_UNAVAILABLE"
	PolicyViolation      = "POLICY_VIOLATION"
	PolicyIngestInvalid  = "POLICY_INGEST_INVALID"
)

// Scanner domain
const (
	ScannerFailed   = "SCANNER_FAILED"
	ScannerTimeout  = "SCANNER_TIMEOUT"
)

// Execution domain
const (
	ExecutionFailed   = "EXECUTION_FAILED"
	ExecutionTimeout  = "EXECUTION_TIMEOUT"
	ExecutionDenied   = "EXECUTION_DENIED"
)

// Auth domain
const (
	Unauthorized  = "UNAUTHORIZED"
	Forbidden     = "FORBIDDEN"
	TokenExpired  = "TOKEN_EXPIRED"
)

// Agent domain
const (
	AgentFailed   = "AGENT_FAILED"
	AgentTimeout  = "AGENT_TIMEOUT"
)

// General domain
const (
	InternalError = "INTERNAL_ERROR"
	NotFound      = "NOT_FOUND"
	BadRequest    = "BAD_REQUEST"
	RateLimited   = "RATE_LIMITED"
	Conflict      = "CONFLICT"
)

// EE (Enterprise Edition) domain
const (
	LicenseRequired   = "LICENSE_REQUIRED"
	LicenseExpired    = "LICENSE_EXPIRED"
	QuotaExceeded     = "QUOTA_EXCEEDED"
	CrossOrgDenied    = "CROSS_ORG_DENIED"
)

// Resource domain
const (
	TaskNotFound        = "TASK_NOT_FOUND"
	TaskAlreadyApproved = "TASK_ALREADY_APPROVED"
	PlanNotFound       = "PLAN_NOT_FOUND"
)
```

## 6. HTTP Helpers (http.go)

### 6.1 WriteJSON

```go
package envelope

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes the envelope as JSON to the response writer with the given status code.
// Sets Content-Type: application/json.
func WriteJSON(w http.ResponseWriter, status int, env *Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}
```

### 6.2 FromHTTPStatus

Maps HTTP status codes to error codes. Use when wrapping generic HTTP errors.

```go
// statusToCode maps HTTP status codes to error codes.
var statusToCode = map[int]string{
	http.StatusBadRequest:          BadRequest,
	http.StatusUnauthorized:        Unauthorized,
	http.StatusForbidden:           Forbidden,
	http.StatusNotFound:            NotFound,
	http.StatusConflict:            Conflict,
	http.StatusTooManyRequests:     RateLimited,
	http.StatusInternalServerError: InternalError,
	http.StatusGatewayTimeout:      ExecutionTimeout,
	http.StatusServiceUnavailable:   PolicyUnavailable,
}

// FromHTTPStatus creates an Envelope from an HTTP status code and message.
// Uses the mapping table above; defaults to INTERNAL_ERROR for unknown statuses.
func FromHTTPStatus(status int, message string) *Envelope {
	code, ok := statusToCode[status]
	if !ok {
		code = InternalError
	}
	retryable := status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout ||
		status == http.StatusTooManyRequests
	return New(code, message, WithRetryable(retryable))
}
```

### 6.3 Status Code to Error Code Mapping Table

| HTTP Status | Error Code |
|-------------|------------|
| 400 Bad Request | BAD_REQUEST |
| 401 Unauthorized | UNAUTHORIZED |
| 403 Forbidden | FORBIDDEN |
| 404 Not Found | NOT_FOUND |
| 409 Conflict | CONFLICT |
| 429 Too Many Requests | RATE_LIMITED |
| 500 Internal Server Error | INTERNAL_ERROR |
| 504 Gateway Timeout | EXECUTION_TIMEOUT |
| 503 Service Unavailable | POLICY_UNAVAILABLE |

**Retryable:** 429, 503, 504 are considered retryable by default.

## 7. Full API Surface Summary

```go
// Types
type Envelope struct { ... }
type Option func(*Envelope)

// Constructor
func New(code, message string, opts ...Option) *Envelope

// Options
func WithTraceID(traceID string) Option
func WithOrgID(orgID string) Option
func WithRetryable(retryable bool) Option
func WithContext(ctx context.Context) Option

// HTTP
func WriteJSON(w http.ResponseWriter, status int, env *Envelope)
func FromHTTPStatus(status int, message string) *Envelope
```

## 8. Test Plan (envelope_test.go)

| Test Case | Description |
|-----------|-------------|
| **New** | Create envelope with code and message; verify fields |
| **New with options** | Apply WithTraceID, WithOrgID, WithRetryable; verify all fields |
| **WithContext** | Create context with trace_id and org_id; verify extraction |
| **Error interface** | Call `Error()` on Envelope; verify returns Message |
| **JSON output** | Marshal Envelope; verify JSON structure and field names |
| **WriteJSON** | Use httptest.ResponseRecorder; verify status, Content-Type, body |
| **FromHTTPStatus** | Test 400, 401, 404, 500, 503; verify correct error codes |
| **FromHTTPStatus retryable** | Test 429, 503, 504; verify Retryable is true |
| **FromHTTPStatus unknown** | Pass 418; verify defaults to INTERNAL_ERROR |
| **Nil Envelope** | Call `Error()` on nil; verify returns empty string |

### Example Test Snippets

```go
func TestNew(t *testing.T) {
	env := New(BadRequest, "invalid input")
	assert.Equal(t, BadRequest, env.ErrorCode)
	assert.Equal(t, "invalid input", env.Message)
	assert.False(t, env.Retryable)
}

func TestNewWithOptions(t *testing.T) {
	env := New(NotFound, "not found",
		WithTraceID("abc123"),
		WithOrgID("org-1"),
		WithRetryable(true))
	assert.Equal(t, "abc123", env.TraceID)
	assert.Equal(t, "org-1", env.OrgID)
	assert.True(t, env.Retryable)
}

func TestErrorInterface(t *testing.T) {
	env := New(InternalError, "oops")
	assert.Equal(t, "oops", env.Error())
}

func TestWriteJSON(t *testing.T) {
	env := New(BadRequest, "bad")
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusBadRequest, env)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var decoded Envelope
	_ = json.NewDecoder(rec.Body).Decode(&decoded)
	assert.Equal(t, BadRequest, decoded.ErrorCode)
}
```

## 9. Dependencies

| Dependency | Purpose |
|------------|---------|
| `context` | WithContext option |
| `encoding/json` | MarshalJSON, WriteJSON |
| `net/http` | WriteJSON, FromHTTPStatus |

**No internal shared-go dependencies.** The envelope package is a foundation layer. Middleware sets `trace_id` and `org_id` in context; envelope reads them via `ctx.Value()`.
