package breaker

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	cb := New("test", WithReadyToTrip(func(c gobreaker.Counts) bool {
		return c.ConsecutiveFailures >= 3
	}))
	for i := 0; i < 3; i++ {
		_, err := cb.Execute(func() (interface{}, error) {
			return nil, errors.New("fail")
		})
		require.Error(t, err)
	}
	require.Equal(t, StateOpen, cb.State())
	_, err := cb.Execute(func() (interface{}, error) {
		return "ok", nil
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, gobreaker.ErrOpenState))
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	cb := New("test",
		WithReadyToTrip(func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= 1 }),
		WithTimeout(10*time.Millisecond),
	)
	_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })
	require.Equal(t, StateOpen, cb.State())
	time.Sleep(15 * time.Millisecond)
	v, err := cb.Execute(func() (interface{}, error) { return "ok", nil })
	require.NoError(t, err)
	require.Equal(t, "ok", v)
	require.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_OnStateChange(t *testing.T) {
	var transitions []string
	cb := New("test",
		WithReadyToTrip(func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= 1 }),
		WithTimeout(10*time.Millisecond),
		WithOnStateChange(func(name string, from, to State) {
			transitions = append(transitions, fmt.Sprintf("%s:%s->%s", name, from, to))
		}),
	)
	_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })
	time.Sleep(15 * time.Millisecond)
	_, _ = cb.Execute(func() (interface{}, error) { return "ok", nil })
	require.Contains(t, transitions, "test:closed->open")
	require.Contains(t, transitions, "test:open->half-open")
	require.Contains(t, transitions, "test:half-open->closed")
}

func TestCircuitBreaker_ExecuteWhenClosed(t *testing.T) {
	cb := New("test")
	v, err := cb.Execute(func() (interface{}, error) {
		return "hello", nil
	})
	require.NoError(t, err)
	require.Equal(t, "hello", v)
}

func TestCircuitBreaker_ExecuteWhenClosedReturnsError(t *testing.T) {
	cb := New("test", WithReadyToTrip(func(c gobreaker.Counts) bool {
		return c.ConsecutiveFailures >= 2
	}))
	_, err := cb.Execute(func() (interface{}, error) {
		return nil, errors.New("transient")
	})
	require.Error(t, err)
	require.Equal(t, "transient", err.Error())
	require.Equal(t, StateClosed, cb.State())
}

func TestCircuitBreaker_Name(t *testing.T) {
	cb := New("my-breaker")
	require.Equal(t, "my-breaker", cb.Name())
}

func TestCircuitBreaker_Counts(t *testing.T) {
	cb := New("test", WithReadyToTrip(func(c gobreaker.Counts) bool {
		return c.ConsecutiveFailures >= 2
	}))
	_, _ = cb.Execute(func() (interface{}, error) { return nil, errors.New("fail") })
	counts := cb.Counts()
	require.Equal(t, uint32(1), counts.ConsecutiveFailures)
}
