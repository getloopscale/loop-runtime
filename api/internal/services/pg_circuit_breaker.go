// Package services implements business logic orchestration.
package services

import (
	"context"
	"log/slog"
	"sync"
	"time"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
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
	circuitClosed  circuitState = iota // normal operation
	circuitOpen                        // rejecting requests
	circuitHalfOpen                    // allowing a single probe
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
// - Each replica detects ArgoCD failure independently after openThreshold failures
// - The convergence delay is at most openThreshold × sync attempts (~15s)
// - Circuit breaker state does not need to survive API restarts — starting
//   CLOSED and detecting failure naturally is the correct behavior
type CircuitBreakerSyncClient struct {
	inner          SyncClient
	logger         *slog.Logger
	openThreshold  int
	closeThreshold int
	retryInterval  time.Duration

	mu             sync.RWMutex
	state          circuitState
	failureCount   int
	successCount   int
	lastFailure    time.Time
	openedAt       time.Time
}

// NewCircuitBreakerSyncClient creates an in-memory circuit breaker wrapping a SyncClient.
func NewCircuitBreakerSyncClient(inner SyncClient, cfg CircuitBreakerConfig, logger *slog.Logger) *CircuitBreakerSyncClient {
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
func (cb *CircuitBreakerSyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration) error {
	if err := cb.preCheck(); err != nil {
		return err
	}

	err := cb.inner.WaitForSync(ctx, appName, timeout)
	cb.recordResult(err)
	return err
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
		cb.logger.Info("circuit breaker: transitioning to half-open, allowing recovery probe")
	}
	cb.mu.Unlock()

	return nil
}

// recordResult updates the circuit breaker state based on the sync result.
func (cb *CircuitBreakerSyncClient) recordResult(err error) {
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
			cb.logger.Warn("circuit breaker: recovery probe failed, re-opening circuit",
				"failure_count", cb.failureCount)
			return
		}

		if cb.failureCount >= cb.openThreshold && cb.state == circuitClosed {
			cb.state = circuitOpen
			cb.openedAt = time.Now()
			cb.logger.Error("circuit breaker: opened — too many consecutive failures",
				"failure_count", cb.failureCount, "threshold", cb.openThreshold)
		}
	} else {
		cb.failureCount = 0
		cb.successCount++

		if cb.state == circuitHalfOpen && cb.successCount >= cb.closeThreshold {
			cb.state = circuitClosed
			cb.logger.Info("circuit breaker: closed — recovery confirmed",
				"success_count", cb.successCount, "threshold", cb.closeThreshold)
			cb.successCount = 0
		}
	}
}
