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
