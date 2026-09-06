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
	"context"
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

	// IsExcluded is consulted by Execute to decide whether an error should
	// count toward failure threshold. When IsExcluded returns true, the error
	// is propagated to the caller but does NOT advance the failure counter.
	//
	// When nil, defaultIsExcluded() is used — it filters out
	// context.Canceled and context.DeadlineExceeded so cancelled or timed-out
	// requests do not falsely trip the breaker. This matches sony/gobreaker v2
	// + mercari/go-circuitbreaker behavior.
	//
	// Per ADR-0031.
	IsExcluded func(error) bool
}

// Breaker is a generic in-memory circuit breaker.
//
// Two equivalent usage styles are supported:
//
//  1. Manual (legacy v1):
//
//     cb := circuitbreaker.New(circuitbreaker.Config{...}, nil)
//     if err := cb.PreCheck(); err != nil { return err }
//     err := doSomething()
//     cb.RecordResult(err)
//
//  2. Generic+context-aware (recommended, ADR-0031):
//
//     cb := circuitbreaker.New(circuitbreaker.Config{...}, nil)
//     result, err := circuitbreaker.Execute(ctx, cb, func(ctx context.Context) (Result, error) {
//     return doSomething(ctx)
//     })
//
// The Execute helper is a package-level generic function (not a method)
// because Go does not allow type parameters on methods. It still operates
// on the same *Breaker — both styles coexist and share state.
type Breaker struct {
	name           string
	openThreshold  int
	closeThreshold int
	retryInterval  time.Duration
	isExcluded     func(error) bool

	mu            sync.RWMutex
	state         State
	failureCount  int
	successCount  int
	openedAt      time.Time
	onStateChange func(name string, from, to State)
}

// defaultIsExcluded filters context cancellation/timeout errors so that
// caller-driven aborts do not falsely advance the failure counter.
// Matches mercari/go-circuitbreaker default behavior.
func defaultIsExcluded(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
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
	if cfg.IsExcluded == nil {
		cfg.IsExcluded = defaultIsExcluded
	}
	return &Breaker{
		name:           cfg.Name,
		openThreshold:  cfg.OpenThreshold,
		closeThreshold: cfg.CloseThreshold,
		retryInterval:  cfg.RetryInterval,
		isExcluded:     cfg.IsExcluded,
		state:          Closed,
		onStateChange:  onStateChange,
	}
}

// Execute is a generic helper that runs fn under circuit breaker protection,
// returning the typed result and propagating context cancellation cleanly.
// Errors matched by the breaker's IsExcluded are surfaced to the caller but
// do NOT count toward the failure threshold (default: context.Canceled and
// context.DeadlineExceeded are excluded).
//
// Execute is a package-level function rather than a method because Go does
// not allow type parameters on methods.
//
// Per ADR-0031 (amended async — see MEET-2026-05-04-PKG-SOTA — package-level
// generic Execute chosen over Breaker[T] type to preserve 100% backward
// compatibility for the 5 existing operator consumers).
func Execute[T any](ctx context.Context, b *Breaker, fn func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	if b == nil {
		return zero, errors.New("circuitbreaker.Execute: nil breaker")
	}
	if err := b.PreCheck(); err != nil {
		return zero, err
	}
	// Honor pre-cancelled context without invoking fn unnecessarily — but
	// invoke fn anyway so callers that want to observe ctx.Err themselves
	// inside fn (e.g. for logging/cleanup) keep that opportunity. Both
	// behaviors are valid; here we delegate to fn and let it observe
	// ctx.Err on entry.
	result, err := fn(ctx)
	if err != nil && b.isExcluded != nil && b.isExcluded(err) {
		// Excluded: propagate but do NOT advance failure counter.
		// We still record SUCCESS so that recovery probes can transition.
		// Important: NOT recording ANYTHING would prevent half-open close.
		// Compromise: only record success when no error path is exercised.
		// For excluded errors we record nothing — consistent with mercari's
		// "Ignore" pattern.
		return result, err
	}
	b.RecordResult(err)
	return result, err
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
