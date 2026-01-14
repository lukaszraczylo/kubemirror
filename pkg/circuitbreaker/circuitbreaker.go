// Package circuitbreaker provides circuit breaker functionality for reconciliation failures.
// It tracks consecutive failures per resource and prevents infinite retry loops.
package circuitbreaker

import (
	"sync"
	"time"
)

// State represents the circuit breaker state
type State int

const (
	// StateClosed means the circuit is operating normally
	StateClosed State = iota
	// StateOpen means the circuit is open (failures exceeded threshold)
	StateOpen
	// StateHalfOpen means the circuit is testing if the resource can recover
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Config contains circuit breaker configuration
type Config struct {
	// FailureThreshold is the number of consecutive failures before opening the circuit
	FailureThreshold int
	// ResetTimeout is how long to wait before attempting to close the circuit
	ResetTimeout time.Duration
	// HalfOpenSuccessThreshold is the number of consecutive successes in half-open state to close the circuit
	HalfOpenSuccessThreshold int
}

// DefaultConfig returns sensible default configuration
func DefaultConfig() Config {
	return Config{
		FailureThreshold:         5,
		ResetTimeout:             5 * time.Minute,
		HalfOpenSuccessThreshold: 2,
	}
}

// resourceState tracks the state of a single resource
type resourceState struct {
	lastFailure          time.Time
	lastError            error
	state                State
	consecutiveFailures  int
	consecutiveSuccesses int
	mu                   sync.RWMutex
}

// CircuitBreaker tracks failures per resource and provides circuit breaker functionality
type CircuitBreaker struct {
	states sync.Map
	config Config
}

// New creates a new CircuitBreaker with the given configuration
func New(config Config) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
	}
}

// NewWithDefaults creates a new CircuitBreaker with default configuration
func NewWithDefaults() *CircuitBreaker {
	return New(DefaultConfig())
}

// resourceKey generates a unique key for a resource
func resourceKey(namespace, name, kind string) string {
	return namespace + "/" + name + "/" + kind
}

// getOrCreateState returns the state for a resource, creating if necessary
func (cb *CircuitBreaker) getOrCreateState(key string) *resourceState {
	state, _ := cb.states.LoadOrStore(key, &resourceState{
		state: StateClosed,
	})
	return state.(*resourceState)
}

// AllowRequest checks if a request should be allowed for this resource.
// Returns true if the request should proceed, false if it should be skipped.
// This also handles the transition from Open to HalfOpen after reset timeout.
func (cb *CircuitBreaker) AllowRequest(namespace, name, kind string) bool {
	key := resourceKey(namespace, name, kind)
	state := cb.getOrCreateState(key)

	state.mu.Lock()
	defer state.mu.Unlock()

	switch state.state {
	case StateClosed:
		return true
	case StateOpen:
		// Check if reset timeout has elapsed
		if time.Since(state.lastFailure) >= cb.config.ResetTimeout {
			// Transition to half-open
			state.state = StateHalfOpen
			state.consecutiveSuccesses = 0
			return true
		}
		return false
	case StateHalfOpen:
		// Allow requests in half-open state to test recovery
		return true
	default:
		return true
	}
}

// RecordSuccess records a successful operation for the resource.
// Returns the new state after recording.
func (cb *CircuitBreaker) RecordSuccess(namespace, name, kind string) State {
	key := resourceKey(namespace, name, kind)
	state := cb.getOrCreateState(key)

	state.mu.Lock()
	defer state.mu.Unlock()

	state.consecutiveFailures = 0
	state.lastError = nil

	switch state.state {
	case StateHalfOpen:
		state.consecutiveSuccesses++
		if state.consecutiveSuccesses >= cb.config.HalfOpenSuccessThreshold {
			state.state = StateClosed
			state.consecutiveSuccesses = 0
		}
	case StateOpen:
		// If we got a success while open (after timeout), go to half-open
		if time.Since(state.lastFailure) >= cb.config.ResetTimeout {
			state.state = StateHalfOpen
			state.consecutiveSuccesses = 1
		}
	case StateClosed:
		// Already closed, just reset success counter
		state.consecutiveSuccesses = 0
	}

	return state.state
}

// RecordFailure records a failed operation for the resource.
// Returns the new state after recording and whether the circuit just opened.
func (cb *CircuitBreaker) RecordFailure(namespace, name, kind string, err error) (State, bool) {
	key := resourceKey(namespace, name, kind)
	state := cb.getOrCreateState(key)

	state.mu.Lock()
	defer state.mu.Unlock()

	state.consecutiveFailures++
	state.consecutiveSuccesses = 0
	state.lastFailure = time.Now()
	state.lastError = err

	justOpened := false

	switch state.state {
	case StateClosed:
		if state.consecutiveFailures >= cb.config.FailureThreshold {
			state.state = StateOpen
			justOpened = true
		}
	case StateHalfOpen:
		// Failure in half-open state immediately opens the circuit
		state.state = StateOpen
		justOpened = true
	case StateOpen:
		// Already open, just update failure count
	}

	return state.state, justOpened
}

// GetState returns the current state for a resource
func (cb *CircuitBreaker) GetState(namespace, name, kind string) State {
	key := resourceKey(namespace, name, kind)
	state := cb.getOrCreateState(key)

	state.mu.RLock()
	defer state.mu.RUnlock()

	// Check if open circuit should transition to half-open
	if state.state == StateOpen && time.Since(state.lastFailure) >= cb.config.ResetTimeout {
		return StateHalfOpen
	}

	return state.state
}

// GetFailureCount returns the consecutive failure count for a resource
func (cb *CircuitBreaker) GetFailureCount(namespace, name, kind string) int {
	key := resourceKey(namespace, name, kind)
	state := cb.getOrCreateState(key)

	state.mu.RLock()
	defer state.mu.RUnlock()

	return state.consecutiveFailures
}

// GetLastError returns the last error recorded for a resource
func (cb *CircuitBreaker) GetLastError(namespace, name, kind string) error {
	key := resourceKey(namespace, name, kind)
	state := cb.getOrCreateState(key)

	state.mu.RLock()
	defer state.mu.RUnlock()

	return state.lastError
}

// Reset resets the circuit breaker state for a resource
func (cb *CircuitBreaker) Reset(namespace, name, kind string) {
	key := resourceKey(namespace, name, kind)
	cb.states.Delete(key)
}

// OpenCircuits returns a list of resources with open circuits
func (cb *CircuitBreaker) OpenCircuits() []string {
	var open []string
	cb.states.Range(func(key, value any) bool {
		state := value.(*resourceState)
		state.mu.RLock()
		isOpen := state.state == StateOpen
		state.mu.RUnlock()
		if isOpen {
			open = append(open, key.(string))
		}
		return true
	})
	return open
}

// Stats contains aggregate statistics
type Stats struct {
	Total    int
	Closed   int
	Open     int
	HalfOpen int
}

// GetStats returns aggregate statistics about circuit states
func (cb *CircuitBreaker) GetStats() Stats {
	stats := Stats{}
	cb.states.Range(func(key, value any) bool {
		state := value.(*resourceState)
		state.mu.RLock()
		s := state.state
		// Check for timeout transition
		if s == StateOpen && time.Since(state.lastFailure) >= cb.config.ResetTimeout {
			s = StateHalfOpen
		}
		state.mu.RUnlock()

		stats.Total++
		switch s {
		case StateClosed:
			stats.Closed++
		case StateOpen:
			stats.Open++
		case StateHalfOpen:
			stats.HalfOpen++
		}
		return true
	})
	return stats
}
