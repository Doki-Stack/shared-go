package ratelimit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/doki-stack/shared-go/envelope"
	"github.com/doki-stack/shared-go/middleware"
	"github.com/stretchr/testify/require"
)

func TestLimiter_AllowBurstThenDeny(t *testing.T) {
	l := NewLimiter(10, 2)
	require.True(t, l.Allow())
	require.True(t, l.Allow())
	require.False(t, l.Allow())
}

func TestKeyedLimiter_IndependentKeys(t *testing.T) {
	kl := NewKeyedLimiter(1, 1, WithTTL(time.Minute))
	defer kl.Stop()
	require.True(t, kl.Allow("key1"))
	require.False(t, kl.Allow("key1"))
	require.True(t, kl.Allow("key2"))
}

func TestMiddleware_Returns429WhenLimited(t *testing.T) {
	l := NewLimiter(0.001, 1)
	handler := Middleware(l)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	require.Equal(t, http.StatusOK, rec1.Code)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
	require.NotEmpty(t, rec2.Header().Get("Retry-After"))
	var env envelope.Envelope
	require.NoError(t, json.NewDecoder(rec2.Body).Decode(&env))
	require.Equal(t, envelope.RateLimited, env.ErrorCode)
}

func TestDefaultKeyFunc(t *testing.T) {
	ctx := middleware.ContextWithOrgID(context.Background(), "org-123")
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ctx)
	req.Header.Set("X-User-Id", "user-456")
	require.Equal(t, "org-123:user-456", DefaultKeyFunc(req))
}

func TestDefaultKeyFunc_NoOrg(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	require.Equal(t, "", DefaultKeyFunc(req))
}

func TestKeyedLimiter_TTLCleanup(t *testing.T) {
	kl := NewKeyedLimiter(1, 1, WithTTL(50*time.Millisecond), WithCleanupInterval(20*time.Millisecond))
	defer kl.Stop()
	kl.Allow("expiring-key")
	time.Sleep(100 * time.Millisecond)
	// After TTL, key should be cleaned up; new request for same key gets fresh limiter
	require.True(t, kl.Allow("expiring-key"))
}

func TestKeyedLimiter_ReserveCancel(t *testing.T) {
	kl := NewKeyedLimiter(1, 1, WithTTL(time.Minute))
	defer kl.Stop()
	require.True(t, kl.Allow("r"))
	res := kl.Reserve("r")
	require.True(t, res.Delay() > 0)
	res.Cancel()
	// After refill, Allow should succeed
	time.Sleep(time.Second + 100*time.Millisecond)
	require.True(t, kl.Allow("r"))
}

func TestLimiter_Wait(t *testing.T) {
	l := NewLimiter(10, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Wait with token available should succeed immediately
	require.NoError(t, l.Wait(ctx))
}

func TestKeyedMiddleware_EmptyKeyPassThrough(t *testing.T) {
	kl := NewKeyedLimiter(1, 1, WithTTL(time.Minute))
	defer kl.Stop()
	handler := KeyedMiddleware(kl, DefaultKeyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestKeyedLimiter_ConcurrentAccess(t *testing.T) {
	kl := NewKeyedLimiter(100, 10, WithTTL(time.Minute))
	defer kl.Stop()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				kl.Allow("concurrent-key")
			}
		}()
	}
	wg.Wait()
}
