package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// KeyedOption configures KeyedLimiter.
type KeyedOption func(*keyedConfig)

type keyedConfig struct {
	ttl             time.Duration
	cleanupInterval time.Duration
}

// WithTTL sets the expiry for unused keys (default: 10 minutes).
func WithTTL(d time.Duration) KeyedOption {
	return func(c *keyedConfig) {
		c.ttl = d
	}
}

// WithCleanupInterval sets the interval for cleanup goroutine (default: 1 minute).
func WithCleanupInterval(d time.Duration) KeyedOption {
	return func(c *keyedConfig) {
		c.cleanupInterval = d
	}
}

type keyedEntry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// KeyedLimiter provides per-key rate limiting with automatic cleanup of unused keys.
type KeyedLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*keyedEntry
	rate     float64
	burst    int
	ttl      time.Duration
	stopCh   chan struct{}
}

// NewKeyedLimiter creates a KeyedLimiter with rate r and burst per key.
func NewKeyedLimiter(r float64, burst int, opts ...KeyedOption) *KeyedLimiter {
	cfg := &keyedConfig{
		ttl:             10 * time.Minute,
		cleanupInterval: 1 * time.Minute,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	kl := &KeyedLimiter{
		limiters: make(map[string]*keyedEntry),
		rate:     r,
		burst:    burst,
		ttl:      cfg.ttl,
		stopCh:   make(chan struct{}),
	}
	go kl.cleanup(cfg.cleanupInterval)
	return kl
}

// Stop stops the cleanup goroutine. Call when shutting down.
func (kl *KeyedLimiter) Stop() {
	close(kl.stopCh)
}

func (kl *KeyedLimiter) getOrCreate(key string) *rate.Limiter {
	kl.mu.Lock()
	defer kl.mu.Unlock()
	if e, ok := kl.limiters[key]; ok {
		e.lastUsed = time.Now()
		return e.limiter
	}
	e := &keyedEntry{
		limiter:  rate.NewLimiter(rate.Limit(kl.rate), kl.burst),
		lastUsed: time.Now(),
	}
	kl.limiters[key] = e
	return e.limiter
}

// Allow reports whether one token is available for the key.
func (kl *KeyedLimiter) Allow(key string) bool {
	limiter := kl.getOrCreate(key)
	return limiter.Allow()
}

// Wait blocks until one token is available for the key or ctx is done.
func (kl *KeyedLimiter) Wait(ctx context.Context, key string) error {
	limiter := kl.getOrCreate(key)
	return limiter.Wait(ctx)
}

// Reserve returns a reservation for one token. Use for accurate Retry-After.
// When rejecting a request, call reservation.Cancel() to avoid consuming the token.
func (kl *KeyedLimiter) Reserve(key string) *rate.Reservation {
	limiter := kl.getOrCreate(key)
	return limiter.Reserve()
}

// cleanup periodically removes expired entries.
func (kl *KeyedLimiter) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-kl.stopCh:
			return
		case <-ticker.C:
			kl.mu.Lock()
			now := time.Now()
			for k, e := range kl.limiters {
				if now.Sub(e.lastUsed) > kl.ttl {
					delete(kl.limiters, k)
				}
			}
			kl.mu.Unlock()
		}
	}
}
