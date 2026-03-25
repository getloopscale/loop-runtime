// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
	"time"
)

// CircuitBreakerState represents the state stored in PostgreSQL.
type CircuitBreakerState struct {
	Key          string
	State        string // "closed" or "open"
	FailureCount int
	SuccessCount int
	LastFailure  *time.Time
	LastSuccess  *time.Time
	OpenedAt     *time.Time
	UpdatedAt    time.Time
}

// CircuitBreakerStore manages shared circuit breaker state in PostgreSQL.
// All API replicas read/write to the same table, ensuring consistent behavior.
type CircuitBreakerStore struct {
	db DBTX
}

// NewCircuitBreakerStore creates a new CircuitBreakerStore.
func NewCircuitBreakerStore(db DBTX) *CircuitBreakerStore {
	return &CircuitBreakerStore{db: db}
}

// Get returns the current state of a circuit breaker.
func (s *CircuitBreakerStore) Get(ctx context.Context, key string) (*CircuitBreakerState, error) {
	var state CircuitBreakerState
	err := s.db.QueryRow(ctx,
		`SELECT key, state, failure_count, success_count, last_failure, last_success, opened_at, updated_at
		 FROM circuit_breakers WHERE key = $1`, key,
	).Scan(&state.Key, &state.State, &state.FailureCount, &state.SuccessCount,
		&state.LastFailure, &state.LastSuccess, &state.OpenedAt, &state.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("circuit breaker store: get %s: %w", key, err)
	}
	return &state, nil
}

// RecordFailure increments the failure counter and opens the circuit if threshold is reached.
// Returns the updated state.
func (s *CircuitBreakerStore) RecordFailure(ctx context.Context, key string, openThreshold int) (*CircuitBreakerState, error) {
	var state CircuitBreakerState
	err := s.db.QueryRow(ctx,
		`UPDATE circuit_breakers
		 SET failure_count = failure_count + 1,
		     success_count = 0,
		     last_failure = now(),
		     state = CASE
		       WHEN failure_count + 1 >= $2 THEN 'open'
		       ELSE state
		     END,
		     opened_at = CASE
		       WHEN failure_count + 1 >= $2 AND state = 'closed' THEN now()
		       ELSE opened_at
		     END,
		     updated_at = now()
		 WHERE key = $1
		 RETURNING key, state, failure_count, success_count, last_failure, last_success, opened_at, updated_at`,
		key, openThreshold,
	).Scan(&state.Key, &state.State, &state.FailureCount, &state.SuccessCount,
		&state.LastFailure, &state.LastSuccess, &state.OpenedAt, &state.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("circuit breaker store: record failure %s: %w", key, err)
	}
	return &state, nil
}

// RecordSuccess increments the success counter and closes the circuit if threshold is reached.
// Returns the updated state.
func (s *CircuitBreakerStore) RecordSuccess(ctx context.Context, key string, closeThreshold int) (*CircuitBreakerState, error) {
	var state CircuitBreakerState
	err := s.db.QueryRow(ctx,
		`UPDATE circuit_breakers
		 SET success_count = success_count + 1,
		     failure_count = 0,
		     last_success = now(),
		     state = CASE
		       WHEN state = 'open' AND success_count + 1 >= $2 THEN 'closed'
		       ELSE state
		     END,
		     opened_at = CASE
		       WHEN state = 'open' AND success_count + 1 >= $2 THEN NULL
		       ELSE opened_at
		     END,
		     updated_at = now()
		 WHERE key = $1
		 RETURNING key, state, failure_count, success_count, last_failure, last_success, opened_at, updated_at`,
		key, closeThreshold,
	).Scan(&state.Key, &state.State, &state.FailureCount, &state.SuccessCount,
		&state.LastFailure, &state.LastSuccess, &state.OpenedAt, &state.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("circuit breaker store: record success %s: %w", key, err)
	}
	return &state, nil
}
