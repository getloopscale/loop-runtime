// Package services implements business logic orchestration.
package services

import (
	"context"
	"log/slog"
	"sync"
	"time"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
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
	inner          WorkflowSubmitter
	logger         *slog.Logger
	openThreshold  int
	closeThreshold int
	retryInterval  time.Duration

	mu           sync.RWMutex
	state        circuitState
	failureCount int
	successCount int
	openedAt     time.Time
}

// NewCircuitBreakerWorkflowSubmitter creates a circuit breaker wrapper around
// a WorkflowSubmitter. Uses the same CircuitBreakerConfig as the ArgoCD circuit
// breaker for consistency.
func NewCircuitBreakerWorkflowSubmitter(
	inner WorkflowSubmitter,
	cfg CircuitBreakerConfig,
	logger *slog.Logger,
) *CircuitBreakerWorkflowSubmitter {
	return &CircuitBreakerWorkflowSubmitter{
		inner:          inner,
		logger:         logger,
		openThreshold:  cfg.OpenThreshold,
		closeThreshold: cfg.CloseThreshold,
		retryInterval:  cfg.RetryInterval,
		state:          circuitClosed,
	}
}

// Submit delegates to the inner WorkflowSubmitter with circuit breaker protection.
// Returns ErrPlatformDegraded immediately when the circuit is open.
func (cb *CircuitBreakerWorkflowSubmitter) Submit(ctx context.Context, spec WorkflowSpec) (string, error) {
	if err := cb.submitPreCheck(); err != nil {
		return "", err
	}

	id, err := cb.inner.Submit(ctx, spec)
	cb.recordSubmitResult(err)
	return id, err
}

// Cancel delegates directly — not circuit-breaker protected (best-effort operation).
func (cb *CircuitBreakerWorkflowSubmitter) Cancel(ctx context.Context, namespace, workflowID string) error {
	return cb.inner.Cancel(ctx, namespace, workflowID)
}

// GetStatus delegates directly — not circuit-breaker protected (read operation
// used by reconcile loop and reaper).
func (cb *CircuitBreakerWorkflowSubmitter) GetStatus(ctx context.Context, namespace, workflowID string) (*WorkflowStatus, error) {
	return cb.inner.GetStatus(ctx, namespace, workflowID)
}

// submitPreCheck evaluates the circuit state for submit operations.
func (cb *CircuitBreakerWorkflowSubmitter) submitPreCheck() error {
	cb.mu.RLock()
	state := cb.state
	openedAt := cb.openedAt
	cb.mu.RUnlock()

	if state == circuitClosed {
		return nil
	}

	if time.Since(openedAt) < cb.retryInterval {
		return loopzeroerrors.ErrPlatformDegraded
	}

	cb.mu.Lock()
	if cb.state == circuitOpen {
		cb.state = circuitHalfOpen
		cb.successCount = 0
		cb.logger.Info("workflow circuit breaker: transitioning to half-open, allowing recovery probe")
	}
	cb.mu.Unlock()

	return nil
}

// recordSubmitResult updates circuit state based on submit result.
func (cb *CircuitBreakerWorkflowSubmitter) recordSubmitResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.successCount = 0
		cb.failureCount++

		if cb.state == circuitHalfOpen {
			cb.state = circuitOpen
			cb.openedAt = time.Now()
			cb.logger.Warn("workflow circuit breaker: recovery probe failed, re-opening",
				"failure_count", cb.failureCount)
			return
		}

		if cb.failureCount >= cb.openThreshold && cb.state == circuitClosed {
			cb.state = circuitOpen
			cb.openedAt = time.Now()
			cb.logger.Error("workflow circuit breaker: opened — build cluster unavailable",
				"failure_count", cb.failureCount, "threshold", cb.openThreshold)
		}
	} else {
		cb.failureCount = 0
		cb.successCount++

		if cb.state == circuitHalfOpen && cb.successCount >= cb.closeThreshold {
			cb.state = circuitClosed
			cb.logger.Info("workflow circuit breaker: closed — build cluster recovered",
				"success_count", cb.successCount, "threshold", cb.closeThreshold)
			cb.successCount = 0
		}
	}
}
