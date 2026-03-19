// Package services implements business logic orchestration.
package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// CircuitBreakerStore abstracts the circuit breaker persistence layer (DIP for testability).
type CircuitBreakerStore interface {
	Get(ctx context.Context, key string) (*db.CircuitBreakerState, error)
	RecordFailure(ctx context.Context, key string, openThreshold int) (*db.CircuitBreakerState, error)
	RecordSuccess(ctx context.Context, key string, closeThreshold int) (*db.CircuitBreakerState, error)
}

// PGCircuitBreakerSyncClient wraps a SyncClient with PostgreSQL-backed circuit breaker.
//
// State is shared across all API replicas via the circuit_breakers table.
// When one replica detects ArgoCD is down, ALL replicas stop sending traffic immediately.
type PGCircuitBreakerSyncClient struct {
	inner          SyncClient
	store          CircuitBreakerStore
	logger         *slog.Logger
	key            string
	openThreshold  int
	closeThreshold int
	retryInterval  time.Duration
}

// PGCircuitBreakerConfig configures the PG-backed circuit breaker.
type PGCircuitBreakerConfig struct {
	Key            string // e.g., "argocd"
	OpenThreshold  int    // consecutive failures to open
	CloseThreshold int    // consecutive successes to close (during recovery)
	RetryInterval  time.Duration
}

// NewPGCircuitBreakerSyncClient creates a PG-backed circuit breaker wrapping a SyncClient.
func NewPGCircuitBreakerSyncClient(inner SyncClient, store CircuitBreakerStore, cfg PGCircuitBreakerConfig, logger *slog.Logger) *PGCircuitBreakerSyncClient {
	return &PGCircuitBreakerSyncClient{
		inner:          inner,
		store:          store,
		logger:         logger,
		key:            cfg.Key,
		openThreshold:  cfg.OpenThreshold,
		closeThreshold: cfg.CloseThreshold,
		retryInterval:  cfg.RetryInterval,
	}
}

// TriggerSync delegates to the inner SyncClient with circuit breaker protection.
func (cb *PGCircuitBreakerSyncClient) TriggerSync(ctx context.Context, appName string) error {
	if err := cb.preCheck(ctx); err != nil {
		return err
	}

	err := cb.inner.TriggerSync(ctx, appName)
	cb.recordResult(ctx, err)
	return err
}

// WaitForSync delegates to the inner SyncClient with circuit breaker protection.
func (cb *PGCircuitBreakerSyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration) error {
	if err := cb.preCheck(ctx); err != nil {
		return err
	}

	err := cb.inner.WaitForSync(ctx, appName, timeout)
	cb.recordResult(ctx, err)
	return err
}

// preCheck reads the circuit breaker state from PostgreSQL.
// Returns ErrPlatformDegraded if the circuit is open and no recovery probe is due.
func (cb *PGCircuitBreakerSyncClient) preCheck(ctx context.Context) error {
	state, err := cb.store.Get(ctx, cb.key)
	if err != nil {
		// If we can't read circuit breaker state, allow the call through
		// (fail-open: better to try than to block all deploys)
		return nil
	}

	if state.State == "closed" {
		return nil
	}

	// Circuit is open — check if enough time has passed for a recovery probe
	if state.OpenedAt != nil && time.Since(*state.OpenedAt) < cb.retryInterval {
		return loopzeroerrors.ErrPlatformDegraded
	}

	// Allow recovery probe through
	return nil
}

// recordResult updates the circuit breaker state in PostgreSQL.
// Errors are logged but do not fail the caller — the sync result itself
// is more important than the circuit breaker bookkeeping.
func (cb *PGCircuitBreakerSyncClient) recordResult(ctx context.Context, err error) {
	if err != nil {
		if _, storeErr := cb.store.RecordFailure(ctx, cb.key, cb.openThreshold); storeErr != nil {
			cb.logger.Error("circuit breaker: failed to record failure",
				"key", cb.key,
				"error", storeErr,
			)
		}
	} else {
		if _, storeErr := cb.store.RecordSuccess(ctx, cb.key, cb.closeThreshold); storeErr != nil {
			cb.logger.Error("circuit breaker: failed to record success",
				"key", cb.key,
				"error", storeErr,
			)
		}
	}
}
