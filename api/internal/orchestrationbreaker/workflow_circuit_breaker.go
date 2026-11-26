// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package orchestrationbreaker guards the two clients every deploy depends on —
// the Argo Workflows submitter the builds feature uses and the ArgoCD sync
// client the release feature uses — with one circuit-breaker policy, so an
// outage of either surfaces as ErrPlatformDegraded instead of a pile of
// timeouts. It is shared kernel: both features consume the decorated clients
// through deploycontracts, and the breaker policy is one.
package orchestrationbreaker

import (
	"context"
	"log/slog"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/pkg/circuitbreaker"
)

// CircuitBreakerWorkflowSubmitter wraps a WorkflowSubmitter with circuit breaker
// protection for the Submit operation. When the Argo Workflow API is unavailable,
// the circuit opens and rejects new submissions immediately instead of retrying
// against a dead endpoint for ~7s per call.
//
// REVIEW-MOD-5: Without this, 10 concurrent deploys against a down Argo controller
// would each waste 7s in retries, blocking the API for a total of ~70 seconds.
// With the circuit breaker, after 3 failures (~21s), all subsequent submissions
// fail in <1ms until recovery is confirmed.
//
// Cancel and GetStatus are NOT protected — they are read/control operations that
// should always attempt to reach the cluster (GetStatus is used by the reconcile
// loop and reaper, Cancel is best-effort).
type CircuitBreakerWorkflowSubmitter struct {
	inner   deploycontracts.WorkflowSubmitter
	logger  *slog.Logger
	breaker *circuitbreaker.Breaker
}

// NewCircuitBreakerWorkflowSubmitter creates a circuit breaker wrapper around
// a WorkflowSubmitter. Uses the same CircuitBreakerConfig as the ArgoCD circuit
// breaker for consistency.
func NewCircuitBreakerWorkflowSubmitter(
	inner deploycontracts.WorkflowSubmitter,
	cfg CircuitBreakerConfig,
	logger *slog.Logger,
) *CircuitBreakerWorkflowSubmitter {
	cb := &CircuitBreakerWorkflowSubmitter{inner: inner, logger: logger}
	// Every Submit error counts, caller cancellation included — the behaviour
	// this decorator had before it ran on pkg/circuitbreaker.
	countAll := func(error) bool { return false }
	cb.breaker = newBreaker("argo-workflows-submit", cfg, countAll, cb.onStateChange)
	return cb
}

// Submit delegates to the inner WorkflowSubmitter with circuit breaker protection.
// Returns ErrPlatformDegraded immediately when the circuit is open.
func (cb *CircuitBreakerWorkflowSubmitter) Submit(ctx context.Context, spec deploycontracts.WorkflowSpec) (string, error) {
	var id string
	err := runGuarded(ctx, cb.breaker, func(ctx context.Context) error {
		var submitErr error
		id, submitErr = cb.inner.Submit(ctx, spec)
		return submitErr
	})
	return id, err
}

// Cancel delegates directly — not circuit-breaker protected (best-effort operation).
func (cb *CircuitBreakerWorkflowSubmitter) Cancel(ctx context.Context, namespace, workflowID string) error {
	return cb.inner.Cancel(ctx, namespace, workflowID)
}

// GetStatus delegates directly — not circuit-breaker protected (read operation
// used by reconcile loop and reaper).
func (cb *CircuitBreakerWorkflowSubmitter) GetStatus(ctx context.Context, namespace, workflowID string) (*deploycontracts.WorkflowStatus, error) {
	return cb.inner.GetStatus(ctx, namespace, workflowID)
}

// onStateChange logs each transition of the submit circuit.
func (cb *CircuitBreakerWorkflowSubmitter) onStateChange(_ string, _, to circuitbreaker.State) {
	switch to {
	case circuitbreaker.Closed:
		cb.logger.Info("workflow circuit breaker: closed — build cluster recovered")
	case circuitbreaker.Open:
		cb.logger.Error("workflow circuit breaker: opened — build cluster unavailable")
	case circuitbreaker.HalfOpen:
		cb.logger.Info("workflow circuit breaker: transitioning to half-open, allowing recovery probe")
	}
}
