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
// Stores under both OrgIDKey (typed) and "org_id" (string) for logger/envelope compatibility.
func ContextWithOrgID(ctx context.Context, orgID string) context.Context {
	ctx = context.WithValue(ctx, OrgIDKey, orgID)
	ctx = context.WithValue(ctx, "org_id", orgID)
	return ctx
}

// ContextWithRequestID returns a copy of ctx with request_id set.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}
