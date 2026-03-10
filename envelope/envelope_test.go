package envelope

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNew(t *testing.T) {
	env := New(BadRequest, "invalid input")
	if env.ErrorCode != BadRequest {
		t.Errorf("ErrorCode = %q, want %q", env.ErrorCode, BadRequest)
	}
	if env.Message != "invalid input" {
		t.Errorf("Message = %q, want invalid input", env.Message)
	}
	if env.Retryable {
		t.Error("Retryable should be false by default")
	}
}

func TestNewWithOptions(t *testing.T) {
	env := New(NotFound, "not found",
		WithTraceID("abc123"),
		WithOrgID("org-1"),
		WithRetryable(true))
	if env.TraceID != "abc123" {
		t.Errorf("TraceID = %q, want abc123", env.TraceID)
	}
	if env.OrgID != "org-1" {
		t.Errorf("OrgID = %q, want org-1", env.OrgID)
	}
	if !env.Retryable {
		t.Error("Retryable should be true")
	}
}

func TestWithContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), "trace_id", "trace-xyz")
	ctx = context.WithValue(ctx, "org_id", "org-123")
	env := New(InternalError, "oops", WithContext(ctx))
	if env.TraceID != "trace-xyz" {
		t.Errorf("TraceID = %q, want trace-xyz", env.TraceID)
	}
	if env.OrgID != "org-123" {
		t.Errorf("OrgID = %q, want org-123", env.OrgID)
	}
}

func TestWithContextNil(t *testing.T) {
	env := New(InternalError, "oops", WithContext(nil))
	if env.TraceID != "" || env.OrgID != "" {
		t.Error("WithContext(nil) should not set fields")
	}
}

func TestErrorInterface(t *testing.T) {
	env := New(InternalError, "oops")
	if got := env.Error(); got != "oops" {
		t.Errorf("Error() = %q, want oops", got)
	}
}

func TestErrorInterfaceNil(t *testing.T) {
	var env *Envelope
	if got := env.Error(); got != "" {
		t.Errorf("nil Envelope Error() = %q, want empty", got)
	}
}

func TestJSONOutput(t *testing.T) {
	env := New(BadRequest, "bad")
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded Envelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ErrorCode != BadRequest {
		t.Errorf("ErrorCode = %q, want %q", decoded.ErrorCode, BadRequest)
	}
	if decoded.Message != "bad" {
		t.Errorf("Message = %q, want bad", decoded.Message)
	}
}

func TestWriteJSON(t *testing.T) {
	env := New(BadRequest, "bad")
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusBadRequest, env)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Code = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
	var decoded Envelope
	if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.ErrorCode != BadRequest {
		t.Errorf("ErrorCode = %q, want %q", decoded.ErrorCode, BadRequest)
	}
}

func TestFromHTTPStatus(t *testing.T) {
	tests := []struct {
		status int
		code   string
	}{
		{http.StatusBadRequest, BadRequest},
		{http.StatusUnauthorized, Unauthorized},
		{http.StatusForbidden, Forbidden},
		{http.StatusNotFound, NotFound},
		{http.StatusConflict, Conflict},
		{http.StatusTooManyRequests, RateLimited},
		{http.StatusInternalServerError, InternalError},
		{http.StatusGatewayTimeout, ExecutionTimeout},
		{http.StatusServiceUnavailable, PolicyUnavailable},
	}
	for _, tt := range tests {
		env := FromHTTPStatus(tt.status, "test")
		if env.ErrorCode != tt.code {
			t.Errorf("status %d: ErrorCode = %q, want %q", tt.status, env.ErrorCode, tt.code)
		}
	}
}

func TestFromHTTPStatusRetryable(t *testing.T) {
	retryableStatuses := []int{
		http.StatusTooManyRequests,
		http.StatusGatewayTimeout,
		http.StatusServiceUnavailable,
	}
	for _, status := range retryableStatuses {
		env := FromHTTPStatus(status, "test")
		if !env.Retryable {
			t.Errorf("status %d: Retryable should be true", status)
		}
	}
}

func TestFromHTTPStatusUnknown(t *testing.T) {
	env := FromHTTPStatus(418, "I'm a teapot")
	if env.ErrorCode != InternalError {
		t.Errorf("ErrorCode = %q, want %q", env.ErrorCode, InternalError)
	}
}
