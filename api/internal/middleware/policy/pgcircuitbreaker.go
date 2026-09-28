// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package policy

import (
	"encoding/json"
	"net/http"

	"github.com/getloopscale/loopzero/api/internal/metrics"
)

// PoolGate decides whether a mutation may proceed given the pool's state.
//
// The interface is declared HERE, by the consumer, not in the package that
// implements it (`rules/architecture.md` § DIP): the middleware does not import
// `buildplane/admission`, and the breaker there satisfies it without knowing it exists.
type PoolGate interface {
	// Allow returns false when the gate is tripped.
	Allow() bool
}

// PGCircuitBreakerConfig controls the PG pool circuit breaker.
type PGCircuitBreakerConfig struct {
	// Gate decides. Until 2026-08-19 this middleware computed an instantaneous
	// percentage right here, while the gate WITH hysteresis
	// (`buildplane/admission.PGPoolBreaker`) was built, started, stopped at
	// shutdown — and never consulted (B-054).
	//
	// The effect was the worst of both: the instrument that solved the flapping
	// was off, and the one deciding flapped. Near the cut-off the pool rises and
	// falls with every returned connection, and the client saw 503 alternating
	// with 200.
	//
	// Nil disables the gate — which keeps valid the path where the admission
	// stack is not assembled.
	Gate PoolGate
}

// PGCircuitBreaker rejects mutation HTTP requests when the PostgreSQL
// connection pool is above the configured saturation threshold. Read
// requests (GET, HEAD, OPTIONS) are always allowed.
type PGCircuitBreaker struct {
	cfg PGCircuitBreakerConfig
}

// NewPGCircuitBreaker creates a circuit breaker with the given config.
func NewPGCircuitBreaker(cfg PGCircuitBreakerConfig) *PGCircuitBreaker {
	return &PGCircuitBreaker{cfg: cfg}
}

// Middleware returns an HTTP middleware that applies the circuit breaker.
func (cb *PGCircuitBreaker) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Reads always pass.
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			if cb.cfg.Gate != nil && !cb.cfg.Gate.Allow() {
				metrics.PGCircuitBreakerTripped.WithLabelValues(r.Method).Inc()
				w.Header().Set("Retry-After", "5")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":       "service temporarily unavailable — database pool at capacity",
					"retry_after": 5,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
