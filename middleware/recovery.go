package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/doki-stack/shared-go/envelope"
	"github.com/doki-stack/shared-go/logger"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Recovery recovers from panics, logs the stack trace, and returns 500.
func Recovery(log *logger.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					stack := string(debug.Stack())
					log.Error("panic recovered",
						zap.String("panic", fmt.Sprint(err)),
						zap.String("stack", stack))

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
