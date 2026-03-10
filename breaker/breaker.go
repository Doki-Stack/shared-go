package breaker

import (
	"time"

	"github.com/sony/gobreaker/v2"
)

// CircuitBreaker wraps gobreaker.CircuitBreaker with a name and configurable options.
type CircuitBreaker struct {
	cb   *gobreaker.CircuitBreaker[any]
	name string
}

// State is the circuit breaker state (alias for gobreaker.State).
type State = gobreaker.State

const (
	StateClosed   = gobreaker.StateClosed
	StateHalfOpen = gobreaker.StateHalfOpen
	StateOpen     = gobreaker.StateOpen
)

// Option configures the circuit breaker.
type Option func(*breakerConfig)

type breakerConfig struct {
	maxRequests   uint32
	interval      time.Duration
	timeout       time.Duration
	readyToTrip   func(gobreaker.Counts) bool
	onStateChange func(name string, from, to State)
}

// WithMaxRequests sets the max number of requests allowed in half-open state (default: 1).
func WithMaxRequests(n uint32) Option {
	return func(c *breakerConfig) {
		c.maxRequests = n
	}
}

// WithInterval sets the cyclic period of the closed state for clearing internal counts (default: 0 = never clear).
func WithInterval(d time.Duration) Option {
	return func(c *breakerConfig) {
		c.interval = d
	}
}

// WithTimeout sets the period of the open state after which the state becomes half-open (default: 60s).
func WithTimeout(d time.Duration) Option {
	return func(c *breakerConfig) {
		c.timeout = d
	}
}

// WithReadyToTrip sets the custom function that determines if the breaker should trip (default: 5 consecutive failures).
func WithReadyToTrip(fn func(gobreaker.Counts) bool) Option {
	return func(c *breakerConfig) {
		c.readyToTrip = fn
	}
}

// WithOnStateChange sets the callback invoked when the breaker state changes.
func WithOnStateChange(fn func(name string, from, to State)) Option {
	return func(c *breakerConfig) {
		c.onStateChange = fn
	}
}

// New creates a CircuitBreaker with the given name and options.
func New(name string, opts ...Option) *CircuitBreaker {
	cfg := &breakerConfig{
		maxRequests: 1,
		interval:     0,
		timeout:      60 * time.Second,
		readyToTrip: func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= 5
		},
		onStateChange: nil,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	settings := gobreaker.Settings{
		Name:        name,
		MaxRequests: cfg.maxRequests,
		Interval:    cfg.interval,
		Timeout:     cfg.timeout,
		ReadyToTrip: cfg.readyToTrip,
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			if cfg.onStateChange != nil {
				cfg.onStateChange(name, from, to)
			}
		},
	}

	cb := gobreaker.NewCircuitBreaker[any](settings)
	return &CircuitBreaker{cb: cb, name: name}
}

// Execute runs the given function through the circuit breaker.
// Returns gobreaker.ErrOpenState when the circuit is open (fail-fast).
func (cb *CircuitBreaker) Execute(fn func() (interface{}, error)) (interface{}, error) {
	return cb.cb.Execute(fn)
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() State {
	return cb.cb.State()
}

// Name returns the circuit breaker name.
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// Counts returns the current failure/success counts.
func (cb *CircuitBreaker) Counts() gobreaker.Counts {
	return cb.cb.Counts()
}
