package circuitbreaker

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCircuitBreaker_AllowRequest_Closed(t *testing.T) {
	cb := NewWithDefaults()

	// New resources should be allowed
	assert.True(t, cb.AllowRequest("ns", "name", "Secret"))
	assert.Equal(t, StateClosed, cb.GetState("ns", "name", "Secret"))
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	config := Config{
		FailureThreshold:         3,
		ResetTimeout:             1 * time.Minute,
		HalfOpenSuccessThreshold: 1,
	}
	cb := New(config)

	testErr := errors.New("test error")

	// First two failures keep circuit closed
	state, justOpened := cb.RecordFailure("ns", "name", "Secret", testErr)
	assert.Equal(t, StateClosed, state)
	assert.False(t, justOpened)

	state, justOpened = cb.RecordFailure("ns", "name", "Secret", testErr)
	assert.Equal(t, StateClosed, state)
	assert.False(t, justOpened)

	// Third failure opens the circuit
	state, justOpened = cb.RecordFailure("ns", "name", "Secret", testErr)
	assert.Equal(t, StateOpen, state)
	assert.True(t, justOpened)

	// Request should now be blocked
	assert.False(t, cb.AllowRequest("ns", "name", "Secret"))
	assert.Equal(t, StateOpen, cb.GetState("ns", "name", "Secret"))
}

func TestCircuitBreaker_ResetOnSuccess(t *testing.T) {
	config := Config{
		FailureThreshold:         3,
		ResetTimeout:             1 * time.Minute,
		HalfOpenSuccessThreshold: 1,
	}
	cb := New(config)

	testErr := errors.New("test error")

	// Record some failures
	cb.RecordFailure("ns", "name", "Secret", testErr)
	cb.RecordFailure("ns", "name", "Secret", testErr)

	// Success resets failure count
	cb.RecordSuccess("ns", "name", "Secret")
	assert.Equal(t, 0, cb.GetFailureCount("ns", "name", "Secret"))

	// Need 3 more failures to open
	cb.RecordFailure("ns", "name", "Secret", testErr)
	cb.RecordFailure("ns", "name", "Secret", testErr)
	assert.Equal(t, StateClosed, cb.GetState("ns", "name", "Secret"))
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	config := Config{
		FailureThreshold:         2,
		ResetTimeout:             100 * time.Millisecond,
		HalfOpenSuccessThreshold: 2,
	}
	cb := New(config)

	testErr := errors.New("test error")

	// Open the circuit
	cb.RecordFailure("ns", "name", "Secret", testErr)
	cb.RecordFailure("ns", "name", "Secret", testErr)
	assert.Equal(t, StateOpen, cb.GetState("ns", "name", "Secret"))

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	// Should now be half-open
	assert.Equal(t, StateHalfOpen, cb.GetState("ns", "name", "Secret"))
	assert.True(t, cb.AllowRequest("ns", "name", "Secret"))

	// One success in half-open
	cb.RecordSuccess("ns", "name", "Secret")
	assert.Equal(t, StateHalfOpen, cb.GetState("ns", "name", "Secret"))

	// Second success closes the circuit
	cb.RecordSuccess("ns", "name", "Secret")
	assert.Equal(t, StateClosed, cb.GetState("ns", "name", "Secret"))
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	config := Config{
		FailureThreshold:         2,
		ResetTimeout:             100 * time.Millisecond,
		HalfOpenSuccessThreshold: 2,
	}
	cb := New(config)

	testErr := errors.New("test error")

	// Open the circuit
	cb.RecordFailure("ns", "name", "Secret", testErr)
	cb.RecordFailure("ns", "name", "Secret", testErr)

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	// Call AllowRequest to trigger transition to half-open
	assert.True(t, cb.AllowRequest("ns", "name", "Secret"))
	assert.Equal(t, StateHalfOpen, cb.GetState("ns", "name", "Secret"))

	// Failure in half-open immediately opens
	state, justOpened := cb.RecordFailure("ns", "name", "Secret", testErr)
	assert.Equal(t, StateOpen, state)
	assert.True(t, justOpened)
	assert.False(t, cb.AllowRequest("ns", "name", "Secret"))
}

func TestCircuitBreaker_IndependentResources(t *testing.T) {
	cb := NewWithDefaults()

	testErr := errors.New("test error")

	// Failures for resource1
	for i := 0; i < 5; i++ {
		cb.RecordFailure("ns", "resource1", "Secret", testErr)
	}

	// resource1 should be open
	assert.Equal(t, StateOpen, cb.GetState("ns", "resource1", "Secret"))

	// resource2 should still be closed
	assert.Equal(t, StateClosed, cb.GetState("ns", "resource2", "Secret"))
	assert.True(t, cb.AllowRequest("ns", "resource2", "Secret"))
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewWithDefaults()

	testErr := errors.New("test error")

	// Open the circuit
	for i := 0; i < 5; i++ {
		cb.RecordFailure("ns", "name", "Secret", testErr)
	}
	assert.Equal(t, StateOpen, cb.GetState("ns", "name", "Secret"))

	// Reset
	cb.Reset("ns", "name", "Secret")

	// Should be closed again
	assert.Equal(t, StateClosed, cb.GetState("ns", "name", "Secret"))
	assert.True(t, cb.AllowRequest("ns", "name", "Secret"))
}

func TestCircuitBreaker_OpenCircuits(t *testing.T) {
	cb := NewWithDefaults()

	testErr := errors.New("test error")

	// Open some circuits
	for i := 0; i < 5; i++ {
		cb.RecordFailure("ns1", "res1", "Secret", testErr)
		cb.RecordFailure("ns2", "res2", "ConfigMap", testErr)
	}

	open := cb.OpenCircuits()
	assert.Len(t, open, 2)
}

func TestCircuitBreaker_Stats(t *testing.T) {
	config := Config{
		FailureThreshold:         2,
		ResetTimeout:             100 * time.Millisecond,
		HalfOpenSuccessThreshold: 1,
	}
	cb := New(config)

	testErr := errors.New("test error")

	// Create some closed circuits
	cb.AllowRequest("ns", "closed1", "Secret")
	cb.AllowRequest("ns", "closed2", "Secret")

	// Create an open circuit
	cb.RecordFailure("ns", "open1", "Secret", testErr)
	cb.RecordFailure("ns", "open1", "Secret", testErr)

	stats := cb.GetStats()
	assert.Equal(t, 3, stats.Total)
	assert.Equal(t, 2, stats.Closed)
	assert.Equal(t, 1, stats.Open)
	assert.Equal(t, 0, stats.HalfOpen)

	// Wait for timeout and check half-open
	time.Sleep(150 * time.Millisecond)

	stats = cb.GetStats()
	assert.Equal(t, 3, stats.Total)
	assert.Equal(t, 2, stats.Closed)
	assert.Equal(t, 0, stats.Open)
	assert.Equal(t, 1, stats.HalfOpen)
}

func TestCircuitBreaker_GetLastError(t *testing.T) {
	cb := NewWithDefaults()

	err1 := errors.New("first error")
	err2 := errors.New("second error")

	cb.RecordFailure("ns", "name", "Secret", err1)
	assert.Equal(t, err1, cb.GetLastError("ns", "name", "Secret"))

	cb.RecordFailure("ns", "name", "Secret", err2)
	assert.Equal(t, err2, cb.GetLastError("ns", "name", "Secret"))

	cb.RecordSuccess("ns", "name", "Secret")
	assert.Nil(t, cb.GetLastError("ns", "name", "Secret"))
}

func TestState_String(t *testing.T) {
	assert.Equal(t, "closed", StateClosed.String())
	assert.Equal(t, "open", StateOpen.String())
	assert.Equal(t, "half-open", StateHalfOpen.String())
	assert.Equal(t, "unknown", State(99).String())
}
