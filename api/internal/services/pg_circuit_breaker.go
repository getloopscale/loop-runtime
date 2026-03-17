// Package services implements business logic orchestration.
package services

import (
	"context"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// PGCircuitBreakerSyncClient wraps a SyncClient with PostgreSQL-backed circuit breaker.
//
// Unlike the in-memory CircuitBreakerSyncClient, this shares state across all API
// replicas via the circuit_breakers table. When one replica detects ArgoCD is down,
// ALL replicas stop sending traffic immediately.
type PGCircuitBreakerSyncClient struct {
	inner          SyncClient
	store          *db.CircuitBreakerStore
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
func NewPGCircuitBreakerSyncClient(inner SyncClient, store *db.CircuitBreakerStore, cfg PGCircuitBreakerConfig) *PGCircuitBreakerSyncClient {
	return &PGCircuitBreakerSyncClient{
		inner:          inner,
		store:          store,
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
func (cb *PGCircuitBreakerSyncClient) recordResult(ctx context.Context, err error) {
	if err != nil {
		_, _ = cb.store.RecordFailure(ctx, cb.key, cb.openThreshold)
	} else {
		_, _ = cb.store.RecordSuccess(ctx, cb.key, cb.closeThreshold)
	}
}
