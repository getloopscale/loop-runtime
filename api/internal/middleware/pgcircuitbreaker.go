// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/loopzero/loopzero/api/internal/metrics"
)

// PGCircuitBreakerConfig controls the PG pool circuit breaker.
type PGCircuitBreakerConfig struct {
	// MaxConns is the maximum number of connections in the pool.
	MaxConns int32

	// ThresholdPct is the percentage (0-100) of pool utilization above
	// which mutation requests are rejected. Reads continue.
	ThresholdPct int

	// PoolStatsFunc returns (in-use connections, total connections).
	// In production this reads from pgxpool.Stat().
	PoolStatsFunc func() (inUse int32, total int32)
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

			// Check pool utilization.
			inUse, total := cb.cfg.PoolStatsFunc()
			if total == 0 {
				next.ServeHTTP(w, r)
				return
			}

			pct := int(float64(inUse) / float64(total) * 100)
			if pct >= cb.cfg.ThresholdPct {
				metrics.PGCircuitBreakerTripped.WithLabelValues(r.Method).Inc()
				w.Header().Set("Retry-After", "5")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":      "service temporarily unavailable — database pool at capacity",
					"retry_after": 5,
					"pool_pct":    pct,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
