// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package circuitbreaker provides a generic in-memory circuit breaker.
//
// MEET-052: Extracted from api/internal/services/pg_circuit_breaker.go to be
// reusable across API and operator provisioner clients (Gitea, Harbor, MinIO, ArgoCD).
//
// State is per-process, not shared across replicas. This is acceptable because
// each replica detects failure independently after openThreshold failures, and
// CB state does not need to survive restarts — starting CLOSED and detecting
// failure naturally is the correct behavior.
package circuitbreaker

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the circuit breaker is open and no recovery
// probe is due. Callers should treat this as a transient failure and retry later.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// State represents the circuit breaker state.
type State int

const (
	Closed   State = iota // normal operation
	Open                  // rejecting calls
	HalfOpen              // allowing a single recovery probe
)

// String returns the state name for logging/metrics.
func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Config configures a circuit breaker instance.
type Config struct {
	// Name identifies the circuit breaker (e.g., "gitea", "harbor") for logging.
	Name string

	// OpenThreshold is the number of consecutive failures to open the circuit.
	OpenThreshold int

	// CloseThreshold is the number of consecutive successes during half-open
	// to close the circuit (resume normal operation).
	CloseThreshold int

	// RetryInterval is the time to wait in open state before allowing a recovery probe.
	RetryInterval time.Duration
}

// Breaker is a generic in-memory circuit breaker.
//
// Usage:
//
//	cb := circuitbreaker.New(circuitbreaker.Config{...})
//	if err := cb.PreCheck(); err != nil {
//	    return err // circuit is open
//	}
//	err := doSomething()
//	cb.RecordResult(err)
type Breaker struct {
	name           string
	openThreshold  int
	closeThreshold int
	retryInterval  time.Duration

	mu           sync.RWMutex
	state        State
	failureCount int
	successCount int
	openedAt     time.Time
	onStateChange func(name string, from, to State)
}

// New creates a circuit breaker with the given configuration.
// onStateChange is an optional callback for metrics/logging (can be nil).
func New(cfg Config, onStateChange func(name string, from, to State)) *Breaker {
	if cfg.OpenThreshold <= 0 {
		cfg.OpenThreshold = 5
	}
	if cfg.CloseThreshold <= 0 {
		cfg.CloseThreshold = 1
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = 120 * time.Second
	}
	return &Breaker{
		name:           cfg.Name,
		openThreshold:  cfg.OpenThreshold,
		closeThreshold: cfg.CloseThreshold,
		retryInterval:  cfg.RetryInterval,
		state:          Closed,
		onStateChange:  onStateChange,
	}
}

// PreCheck evaluates the circuit state. Returns ErrCircuitOpen if the circuit
// is open and no recovery probe is due. Returns nil if the call may proceed.
func (b *Breaker) PreCheck() error {
	b.mu.RLock()
	state := b.state
	openedAt := b.openedAt
	b.mu.RUnlock()

	if state == Closed {
		return nil
	}

	if time.Since(openedAt) < b.retryInterval {
		return ErrCircuitOpen
	}

	// Transition to half-open: allow one probe through
	b.mu.Lock()
	if b.state == Open {
		b.transition(HalfOpen)
		b.successCount = 0
	}
	b.mu.Unlock()

	return nil
}

// RecordResult updates the circuit breaker state based on the operation result.
// Pass nil for success, non-nil for failure.
func (b *Breaker) RecordResult(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err != nil {
		b.successCount = 0
		b.failureCount++

		if b.state == HalfOpen {
			b.openedAt = time.Now()
			b.transition(Open)
			return
		}

		if b.failureCount >= b.openThreshold && b.state == Closed {
			b.openedAt = time.Now()
			b.transition(Open)
		}
	} else {
		b.failureCount = 0
		b.successCount++

		if b.state == HalfOpen && b.successCount >= b.closeThreshold {
			b.transition(Closed)
			b.successCount = 0
		}
	}
}

// State returns the current circuit breaker state.
func (b *Breaker) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// Name returns the circuit breaker name.
func (b *Breaker) Name() string {
	return b.name
}

// transition changes state and notifies the callback. Caller must hold b.mu.
func (b *Breaker) transition(to State) {
	from := b.state
	b.state = to
	if b.onStateChange != nil {
		b.onStateChange(b.name, from, to)
	}
}
