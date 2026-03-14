// Package services implements business logic orchestration.
package services

import (
	"context"
	"sync"
	"time"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// CircuitState represents the state of the circuit breaker.
type CircuitState int

const (
	// CircuitClosed is the normal operating state where all calls pass through.
	CircuitClosed CircuitState = iota
	// CircuitOpen is the degraded state where calls are rejected immediately.
	CircuitOpen
)

// CircuitBreakerConfig holds the configuration for the circuit breaker.
type CircuitBreakerConfig struct {
	// OpenThreshold is the number of consecutive failures before the circuit opens.
	OpenThreshold int
	// CloseThreshold is the number of consecutive successes during recovery before closing.
	CloseThreshold int
	// RetryInterval is how long to wait before allowing a recovery probe when open.
	RetryInterval time.Duration
}

// CircuitBreakerSyncClient wraps a SyncClient with circuit breaker logic.
//
// Known limitation (DA-7): This is in-process state. Each API replica has its
// own circuit breaker. For v1 (single replica) this is acceptable. Multi-replica
// needs PG-backed state (Sprint 8 tech debt).
type CircuitBreakerSyncClient struct {
	inner          SyncClient
	state          CircuitState
	failureCount   int
	successCount   int
	mu             sync.Mutex
	openThreshold  int
	closeThreshold int
	retryInterval  time.Duration
	lastAttempt    time.Time
}

// NewCircuitBreakerSyncClient creates a new CircuitBreakerSyncClient wrapping the
// given inner SyncClient with the provided configuration.
func NewCircuitBreakerSyncClient(inner SyncClient, cfg CircuitBreakerConfig) *CircuitBreakerSyncClient {
	return &CircuitBreakerSyncClient{
		inner:          inner,
		state:          CircuitClosed,
		openThreshold:  cfg.OpenThreshold,
		closeThreshold: cfg.CloseThreshold,
		retryInterval:  cfg.RetryInterval,
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

// State returns the current circuit state (for metrics).
func (cb *CircuitBreakerSyncClient) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// preCheck determines whether a call should be allowed through.
// Returns ErrPlatformDegraded if the circuit is open and no recovery probe is due.
func (cb *CircuitBreakerSyncClient) preCheck() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitClosed {
		return nil
	}

	// Circuit is OPEN — check if enough time has passed for a recovery probe
	if time.Since(cb.lastAttempt) < cb.retryInterval {
		return loopzeroerrors.ErrPlatformDegraded
	}

	// Allow recovery probe through
	cb.lastAttempt = time.Now()
	return nil
}

// recordResult updates circuit breaker counters based on the call result.
func (cb *CircuitBreakerSyncClient) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitClosed {
		if err != nil {
			cb.failureCount++
			if cb.failureCount >= cb.openThreshold {
				cb.state = CircuitOpen
				cb.lastAttempt = time.Now()
				cb.failureCount = 0
				cb.successCount = 0
			}
		} else {
			// Success resets failure count in closed state
			cb.failureCount = 0
		}
		return
	}

	// Circuit is OPEN (recovery probe)
	if err != nil {
		// Failure during recovery — reset success count, stay OPEN
		cb.successCount = 0
	} else {
		cb.successCount++
		if cb.successCount >= cb.closeThreshold {
			// Enough successes — close the circuit
			cb.state = CircuitClosed
			cb.failureCount = 0
			cb.successCount = 0
		}
	}
}
