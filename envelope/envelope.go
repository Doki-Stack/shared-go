package envelope

import (
	"context"
	"encoding/json"
)

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
func (e *Envelope) MarshalJSON() ([]byte, error) {
	type envelope Envelope
	return json.Marshal((*envelope)(e))
}

// Option configures an Envelope.
type Option func(*Envelope)

// WithTraceID sets the trace ID on the envelope.
func WithTraceID(traceID string) Option {
	return func(e *Envelope) {
		e.TraceID = traceID
	}
}

// WithOrgID sets the org ID on the envelope.
func WithOrgID(orgID string) Option {
	return func(e *Envelope) {
		e.OrgID = orgID
	}
}

// WithRetryable sets whether the error is retryable.
func WithRetryable(retryable bool) Option {
	return func(e *Envelope) {
		e.Retryable = retryable
	}
}

// WithContext extracts trace_id and org_id from context and applies them.
// Expects context keys: "trace_id" (string), "org_id" (string).
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
