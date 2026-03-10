package ratelimit

import (
	"context"

	"golang.org/x/time/rate"
)

// Limiter wraps rate.Limiter for simple token-bucket rate limiting.
type Limiter struct {
	limiter *rate.Limiter
}

// NewLimiter creates a Limiter with rate r (tokens per second) and burst size.
// Example: NewLimiter(10, 20) allows 10 req/s with bursts up to 20.
func NewLimiter(r float64, burst int) *Limiter {
	return &Limiter{
		limiter: rate.NewLimiter(rate.Limit(r), burst),
	}
}

// Allow reports whether one token is available without blocking.
// Returns true if allowed, false if rate limited.
func (l *Limiter) Allow() bool {
	return l.limiter.Allow()
}

// Wait blocks until one token is available or ctx is done.
// Returns nil if allowed, ctx.Err() if context cancelled.
func (l *Limiter) Wait(ctx context.Context) error {
	return l.limiter.Wait(ctx)
}
