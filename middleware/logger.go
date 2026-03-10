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

// Logger logs each request with method, path, status, duration, org_id, request_id.
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
				zap.String("event_type", "http_response"),
			}

			logWithCtx := log.WithContext(ctx)
			switch {
			case wrapped.status >= 500:
				logWithCtx.Error("request completed", fields...)
			case wrapped.status >= 400:
				logWithCtx.Warn("request completed", fields...)
			default:
				logWithCtx.Info("request completed", fields...)
			}
		})
	}
}
