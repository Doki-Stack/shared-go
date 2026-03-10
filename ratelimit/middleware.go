package ratelimit

import (
	"net/http"
	"strconv"

	"github.com/doki-stack/shared-go/envelope"
	"github.com/doki-stack/shared-go/middleware"
)

const retryAfterHeader = "Retry-After"

// Middleware returns middleware that rate limits using the simple Limiter.
// Responds with 429 and envelope RATE_LIMITED when limited.
func Middleware(limiter *Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				env := envelope.New(envelope.RateLimited, "rate limit exceeded")
				w.Header().Set(retryAfterHeader, "1") // Conservative: 1 second
				envelope.WriteJSON(w, http.StatusTooManyRequests, env)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// KeyedMiddleware returns middleware that rate limits per key from keyFunc.
// Uses Reserve() for accurate Retry-After header. When rate limited, calls
// reservation.Cancel() so the token is not consumed.
func KeyedMiddleware(kl *KeyedLimiter, keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			res := kl.Reserve(key)
			if res.Delay() > 0 {
				res.Cancel()
				secs := int(res.Delay().Seconds()) + 1
				if secs < 1 {
					secs = 1
				}
				env := envelope.New(envelope.RateLimited, "rate limit exceeded")
				w.Header().Set(retryAfterHeader, strconv.Itoa(secs))
				envelope.WriteJSON(w, http.StatusTooManyRequests, env)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// DefaultKeyFunc extracts org_id from context and user_id from X-User-Id header.
// Key format: {org_id}:{user_id}. Returns empty string if org_id missing (pass-through).
func DefaultKeyFunc(r *http.Request) string {
	orgID := middleware.OrgIDFromContext(r.Context())
	if orgID == "" {
		return ""
	}
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		userID = "anonymous"
	}
	return orgID + ":" + userID
}
