// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package orchestrationbreaker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/pkg/circuitbreaker"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// CircuitBreakerConfig configures the in-memory circuit breaker.
type CircuitBreakerConfig struct {
	OpenThreshold  int           // consecutive failures to open circuit
	CloseThreshold int           // consecutive successes to close circuit (during half-open)
	RetryInterval  time.Duration // time to wait before allowing a recovery probe
}

// newBreaker builds the shared pkg/circuitbreaker state machine both
// decorators in this package run on. isExcluded names the errors that are
// surfaced to the caller without counting as a dependency failure.
func newBreaker(name string, cfg CircuitBreakerConfig, isExcluded func(error) bool,
	onStateChange func(name string, from, to circuitbreaker.State)) *circuitbreaker.Breaker {
	return circuitbreaker.New(circuitbreaker.Config{
		Name:           name,
		OpenThreshold:  cfg.OpenThreshold,
		CloseThreshold: cfg.CloseThreshold,
		RetryInterval:  cfg.RetryInterval,
		IsExcluded:     isExcluded,
	}, onStateChange)
}

// runGuarded runs fn under the breaker. An open circuit is reported as the
// product error ErrPlatformDegraded, which is what callers match on.
func runGuarded(ctx context.Context, b *circuitbreaker.Breaker, fn func(context.Context) error) error {
	_, err := circuitbreaker.Execute(ctx, b, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	if errors.Is(err, circuitbreaker.ErrCircuitOpen) {
		return loopzeroerrors.ErrPlatformDegraded
	}
	return err
}

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
	inner   deploycontracts.SyncClient
	logger  *slog.Logger
	breaker *circuitbreaker.Breaker
}

// Compile-time assertion that *CircuitBreakerSyncClient satisfies the full
// SyncClient interface declared in build_orchestrator.go. Sprint B T1.1
// (ADR D1) — moves the L22-class regression detector from runtime
// (duck-typed assertions in release_convergence_loop.go that fail silently)
// to build time. If a future refactor accidentally drops one of the 6 methods,
// the package fails to compile here rather than silently no-op in production.
var _ deploycontracts.SyncClient = (*CircuitBreakerSyncClient)(nil)

// NewCircuitBreakerSyncClient creates an in-memory circuit breaker wrapping a SyncClient.
func NewCircuitBreakerSyncClient(inner deploycontracts.SyncClient, cfg CircuitBreakerConfig, logger *slog.Logger) *CircuitBreakerSyncClient {
	cb := &CircuitBreakerSyncClient{inner: inner, logger: logger}
	cb.breaker = newBreaker("argocd-sync", cfg, isSyncFailureExcluded, cb.onStateChange)
	return cb
}

// TriggerSync delegates to the inner SyncClient with circuit breaker protection.
func (cb *CircuitBreakerSyncClient) TriggerSync(ctx context.Context, appName string) error {
	return cb.guard(ctx, func(ctx context.Context) error {
		return cb.inner.TriggerSync(ctx, appName)
	})
}

// WaitForSync delegates to the inner SyncClient with circuit breaker protection.
func (cb *CircuitBreakerSyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration, deployedApps []string) error {
	return cb.guard(ctx, func(ctx context.Context) error {
		return cb.inner.WaitForSync(ctx, appName, timeout, deployedApps)
	})
}

// GetAppStatus delegates to the inner SyncClient. Read-only, so it bypasses the
// breaker: a status poll does not mutate ArgoCD and must keep working while the
// circuit is open.
func (cb *CircuitBreakerSyncClient) GetAppStatus(ctx context.Context, appName string, deployedApps []string) (bool, bool, error) {
	return cb.inner.GetAppStatus(ctx, appName, deployedApps)
}

// TerminateOperation delegates to the inner SyncClient. It bypasses the breaker
// because it is a recovery path: freeing a stuck operation is exactly what must
// still run while syncs are failing (MEET-2026-03-29-019).
func (cb *CircuitBreakerSyncClient) TerminateOperation(ctx context.Context, appName string) error {
	return cb.inner.TerminateOperation(ctx, appName)
}

// ForceReconcile delegates to the inner SyncClient. It bypasses the breaker for
// the same reason as TerminateOperation: it is the convergence loop's L22
// recovery for a controller whose workqueue stalled, and it must run even when
// the breaker is half-open.
func (cb *CircuitBreakerSyncClient) ForceReconcile(ctx context.Context, appName string) error {
	return cb.inner.ForceReconcile(ctx, appName)
}

// GetSyncRevision delegates to the inner SyncClient. Read-only, so it bypasses
// the breaker.
func (cb *CircuitBreakerSyncClient) GetSyncRevision(ctx context.Context, appName string) (string, error) {
	return cb.inner.GetSyncRevision(ctx, appName)
}

// guard runs fn under the breaker and reports the two excluded outcomes:
// neither says anything about ArgoCD's availability, so neither is counted.
func (cb *CircuitBreakerSyncClient) guard(ctx context.Context, fn func(context.Context) error) error {
	err := runGuarded(ctx, cb.breaker, fn)
	switch {
	case errors.Is(err, loopzeroerrors.ErrDeployServiceAuthFailed):
		metrics.ArgoCDAuthFailures.Inc()
		cb.logger.Error("argocd auth failure — not counted toward circuit breaker",
			"suggestion", "check ARGOCD_TOKEN validity and regenerate if expired")
	case errors.Is(err, loopzeroerrors.ErrSyncOperationInProgress):
		cb.logger.Warn("argocd sync operation in progress — not counted toward circuit breaker")
	}
	return err
}

// isSyncFailureExcluded: an auth failure is a credential problem (the token
// provider re-mints), and an operation in progress is ArgoCD working — neither
// means ArgoCD is down. Every other error, caller cancellation included,
// counts toward the threshold.
func isSyncFailureExcluded(err error) bool {
	return errors.Is(err, loopzeroerrors.ErrDeployServiceAuthFailed) ||
		errors.Is(err, loopzeroerrors.ErrSyncOperationInProgress)
}

// onStateChange publishes the state to the ArgoCDCircuitState gauge
// (0 closed, 1 open, 2 half-open) and logs the transition.
func (cb *CircuitBreakerSyncClient) onStateChange(_ string, from, to circuitbreaker.State) {
	switch to {
	case circuitbreaker.Closed:
		metrics.ArgoCDCircuitState.Set(0)
		cb.logger.Info("circuit breaker: closed — recovery confirmed")
	case circuitbreaker.Open:
		metrics.ArgoCDCircuitState.Set(1)
		cb.logger.Error("circuit breaker: opened — ArgoCD calls are failing", "from", from.String())
	case circuitbreaker.HalfOpen:
		metrics.ArgoCDCircuitState.Set(2)
		cb.logger.Info("circuit breaker: transitioning to half-open, allowing recovery probe")
	}
}
