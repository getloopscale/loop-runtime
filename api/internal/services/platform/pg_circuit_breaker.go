// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platform

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/services/shared"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// CircuitBreakerConfig configures the in-memory circuit breaker.
type CircuitBreakerConfig struct {
	OpenThreshold  int           // consecutive failures to open circuit
	CloseThreshold int           // consecutive successes to close circuit (during half-open)
	RetryInterval  time.Duration // time to wait before allowing a recovery probe
}

// circuitState represents the internal state of the circuit breaker.
type circuitState int

const (
	circuitClosed   circuitState = iota // normal operation
	circuitOpen                         // rejecting requests
	circuitHalfOpen                     // allowing a single probe
)

// CircuitBreakerSyncClient wraps a SyncClient with an in-memory circuit breaker.
//
// M-02 fix: State is held in-memory instead of PostgreSQL. This eliminates
// write amplification (2 DB writes per sync attempt) and removes the circular
// dependency where the circuit breaker protecting ArgoCD depends on PostgreSQL,
// which could also be degraded.
//
// Trade-off: state is per-replica, not shared across API instances. This is
// acceptable because:
//   - Each replica detects ArgoCD failure independently after openThreshold failures
//   - The convergence delay is at most openThreshold × sync attempts (~15s)
//   - Circuit breaker state does not need to survive API restarts — starting
//     CLOSED and detecting failure naturally is the correct behavior
type CircuitBreakerSyncClient struct {
	inner          shared.SyncClient
	logger         *slog.Logger
	openThreshold  int
	closeThreshold int
	retryInterval  time.Duration

	mu           sync.RWMutex
	state        circuitState
	failureCount int
	successCount int
	lastFailure  time.Time
	openedAt     time.Time
}

// Compile-time assertion that *CircuitBreakerSyncClient satisfies the full
// SyncClient interface declared in build_orchestrator.go. Sprint B T1.1
// (ADR D1) — moves the L22-class regression detector from runtime
// (duck-typed assertions in release_convergence_loop.go that fail silently)
// to build time. If a future refactor accidentally drops one of the 6 methods,
// the package fails to compile here rather than silently no-op in production.
var _ shared.SyncClient = (*CircuitBreakerSyncClient)(nil)

// NewCircuitBreakerSyncClient creates an in-memory circuit breaker wrapping a SyncClient.
func NewCircuitBreakerSyncClient(inner shared.SyncClient, cfg CircuitBreakerConfig, logger *slog.Logger) *CircuitBreakerSyncClient {
	return &CircuitBreakerSyncClient{
		inner:          inner,
		logger:         logger,
		openThreshold:  cfg.OpenThreshold,
		closeThreshold: cfg.CloseThreshold,
		retryInterval:  cfg.RetryInterval,
		state:          circuitClosed,
	}
}

// TriggerSync delegates to the inner SyncClient with circuit breaker protection.
func (cb *CircuitBreakerSyncClient) TriggerSync(ctx context.Context, appName string) error {
	if err := cb.preCheck(); err != nil {
		return err
	}

	err := cb.inner.TriggerSync(ctx, appName)
	cb.recordResult(err)
	return err
}

// WaitForSync delegates to the inner SyncClient with circuit breaker protection.
func (cb *CircuitBreakerSyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration, deployedApps []string) error {
	if err := cb.preCheck(); err != nil {
		return err
	}

	err := cb.inner.WaitForSync(ctx, appName, timeout, deployedApps)
	cb.recordResult(err)
	return err
}

// GetAppStatus delegates to the inner SyncClient.
// Read-only operation — no circuit breaker needed (does not mutate state).
// Required so that the ReleaseConvergenceLoop type assertion succeeds
// when the SyncClient is wrapped by the circuit breaker (AUDIT-2026-03-28).
func (cb *CircuitBreakerSyncClient) GetAppStatus(ctx context.Context, appName string, deployedApps []string) (bool, bool, error) {
	type statusChecker interface {
		GetAppStatus(ctx context.Context, appName string, deployedApps []string) (bool, bool, error)
	}
	if checker, ok := cb.inner.(statusChecker); ok {
		return checker.GetAppStatus(ctx, appName, deployedApps)
	}
	return false, false, nil
}

// TerminateOperation delegates to the inner SyncClient.
// Required so that the ReleaseConvergenceLoop type assertion succeeds
// when the SyncClient is wrapped by the circuit breaker (MEET-2026-03-29-019).
func (cb *CircuitBreakerSyncClient) TerminateOperation(ctx context.Context, appName string) error {
	type terminator interface {
		TerminateOperation(ctx context.Context, appName string) error
	}
	if t, ok := cb.inner.(terminator); ok {
		return t.TerminateOperation(ctx, appName)
	}
	return nil
}

// ForceReconcile delegates to the inner SyncClient.
// V9 L22 fix 2026-05-31: ReleaseConvergenceLoop performs a type assertion
// `r.sync_.(forceReconciler)` to call ForceReconcile when retriggerSync has
// exhausted (counter >= maxSyncRetriggers) AND the release has been stuck
// for >2min. Without this wrapper method the assertion FAILS silently — the
// inner *argocd.SyncClient has the method but `cb.inner` is typed as the
// SyncClient interface (only TriggerSync + WaitForSync). The L22 escalation
// path was a no-op for all wrapped sync clients, which is the actual
// production wiring (workflows.SyncClient is *CircuitBreakerSyncClient per
// setup_workflows.go). Discovered LIVE in cleanroom-fresh dogfood 2026-05-31:
// J04 Scale release rel-30b7cc1ae4556e55 hit max retriggers + sync_timeout
// without L22 ever firing.
//
// Like the other delegating methods, this re-asserts the interface on the
// inner client so it works whether the inner is a real *argocd.SyncClient
// or another wrapper. No circuit-breaker pre-check: ForceReconcile is a
// recovery path; we want it to run even when the breaker is half-open.
func (cb *CircuitBreakerSyncClient) ForceReconcile(ctx context.Context, appName string) error {
	type forceReconciler interface {
		ForceReconcile(ctx context.Context, appName string) error
	}
	if fr, ok := cb.inner.(forceReconciler); ok {
		return fr.ForceReconcile(ctx, appName)
	}
	return nil
}

// GetSyncRevision delegates to the inner SyncClient.
// Read-only operation — no circuit breaker needed.
// Required so that the commit SHA verification type assertion succeeds
// when the SyncClient is wrapped by the circuit breaker (AUDIT-2026-03-28).
func (cb *CircuitBreakerSyncClient) GetSyncRevision(ctx context.Context, appName string) (string, error) {
	type revisionChecker interface {
		GetSyncRevision(ctx context.Context, appName string) (string, error)
	}
	if checker, ok := cb.inner.(revisionChecker); ok {
		return checker.GetSyncRevision(ctx, appName)
	}
	return "", nil
}

// preCheck evaluates the circuit state. Returns ErrPlatformDegraded if the
// circuit is open and no recovery probe is due.
func (cb *CircuitBreakerSyncClient) preCheck() error {
	cb.mu.RLock()
	state := cb.state
	openedAt := cb.openedAt
	cb.mu.RUnlock()

	if state == circuitClosed {
		return nil
	}

	// Circuit is open — check if enough time has passed for a recovery probe
	if time.Since(openedAt) < cb.retryInterval {
		return loopzeroerrors.ErrPlatformDegraded
	}

	// Transition to half-open: allow one probe through
	cb.mu.Lock()
	if cb.state == circuitOpen {
		cb.state = circuitHalfOpen
		cb.successCount = 0
		metrics.ArgoCDCircuitState.Set(2) // half-open
		cb.logger.Info("circuit breaker: transitioning to half-open, allowing recovery probe")
	}
	cb.mu.Unlock()

	return nil
}

// recordResult updates the circuit breaker state based on the sync result.
// Auth failures (401/403) are NOT counted toward the open threshold — they
// indicate a credential problem, not a transient ArgoCD outage (MEET-013 Phase 1.3).
func (cb *CircuitBreakerSyncClient) recordResult(err error) {
	// Auth failures bypass the circuit breaker entirely (MEET-013 Phase 1.3)
	if err != nil && errors.Is(err, loopzeroerrors.ErrDeployServiceAuthFailed) {
		metrics.ArgoCDAuthFailures.Inc()
		cb.logger.Error("argocd auth failure — not counted toward circuit breaker",
			"suggestion", "check ARGOCD_TOKEN validity and regenerate if expired")
		return
	}
	// MEET-2026-03-29-019: 409 "operation in progress" is NOT a transient failure —
	// it means a previous sync is stuck. Counting it toward the CB threshold would
	// cause a single stuck Application to trip the circuit for the ENTIRE platform.
	if err != nil && errors.Is(err, loopzeroerrors.ErrSyncOperationInProgress) {
		cb.logger.Warn("argocd sync operation in progress — not counted toward circuit breaker")
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.successCount = 0
		cb.failureCount++
		cb.lastFailure = time.Now()

		if cb.state == circuitHalfOpen {
			// Recovery probe failed — back to open
			cb.state = circuitOpen
			cb.openedAt = time.Now()
			metrics.ArgoCDCircuitState.Set(1) // open
			cb.logger.Warn("circuit breaker: recovery probe failed, re-opening circuit",
				"failure_count", cb.failureCount)
			return
		}

		if cb.failureCount >= cb.openThreshold && cb.state == circuitClosed {
			cb.state = circuitOpen
			cb.openedAt = time.Now()
			metrics.ArgoCDCircuitState.Set(1) // open
			cb.logger.Error("circuit breaker: opened — too many consecutive failures",
				"failure_count", cb.failureCount, "threshold", cb.openThreshold)
		}
	} else {
		cb.failureCount = 0
		cb.successCount++

		if cb.state == circuitHalfOpen && cb.successCount >= cb.closeThreshold {
			cb.state = circuitClosed
			metrics.ArgoCDCircuitState.Set(0) // closed
			cb.logger.Info("circuit breaker: closed — recovery confirmed",
				"success_count", cb.successCount, "threshold", cb.closeThreshold)
			cb.successCount = 0
		}
	}
}
