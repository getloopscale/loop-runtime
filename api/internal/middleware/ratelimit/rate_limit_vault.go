// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package ratelimit

// rate_limit_vault.go — T4.3 of plan 2026-06-02-openbao-adoption v1.3.
//
// Per-cell rate limiter for the /internal/v1/secret-fetch broker endpoint.
// Each cell gets its own token bucket so a single noisy cell can't
// exhaust the broker's capacity for other cells. Defense vs ESO storm
// (cell with thousands of ExternalSecrets all refreshing simultaneously).
//
// Default: 50 req/s sustained, burst 100. Tunable via VaultRateLimitConfig.
// Lazy bucket creation (sync.Map per cell), bounded by MaxCells to prevent
// memory blowup from spurious X-Cell-ID values (DoS amplifier).
//
// On exhaustion: returns 429 Too Many Requests with Retry-After header.
// Audit hook optional (T4.4 wiring point — record "vault.broker.rate_limited").

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth"

	"golang.org/x/time/rate"
)

// VaultRateLimitConfig tunes the per-cell limiter.
type VaultRateLimitConfig struct {
	// PerSecond is sustained req/s allowed per cell. Default 50.
	PerSecond float64
	// Burst is the bucket capacity per cell. Default 100.
	Burst int
	// MaxCells caps the number of distinct cells tracked (DoS guard).
	// When exceeded, requests from new cells get 429 immediately.
	// Default 100 (well above expected 10-20 cell count).
	MaxCells int
	// OnRateLimited is called BEFORE the 429 response. Use for audit/metrics.
	// Receives the cellID + the actor (from cross-cluster middleware identity
	// if present in ctx). Optional.
	OnRateLimited func(cellID, actor string)
}

type cellBucket struct {
	limiter *rate.Limiter
}

// VaultRateLimiter is the per-cell limiter middleware factory.
type VaultRateLimiter struct {
	perSecond float64
	burst     int
	maxCells  int
	onLimited func(cellID, actor string)

	buckets sync.Map // map[string]*cellBucket
	count   atomic.Int32
}

// NewVaultRateLimiter constructs the middleware with safe defaults.
func NewVaultRateLimiter(cfg VaultRateLimitConfig) *VaultRateLimiter {
	if cfg.PerSecond <= 0 {
		cfg.PerSecond = 50
	}
	if cfg.Burst <= 0 {
		cfg.Burst = 100
	}
	if cfg.MaxCells <= 0 {
		cfg.MaxCells = 100
	}
	return &VaultRateLimiter{
		perSecond: cfg.PerSecond,
		burst:     cfg.Burst,
		maxCells:  cfg.MaxCells,
		onLimited: cfg.OnRateLimited,
	}
}

// Allow reports whether a request from cellID is permitted right now.
// Public for tests + direct callers (e.g., gRPC handlers later).
func (r *VaultRateLimiter) Allow(cellID string) bool {
	if cellID == "" {
		return false
	}
	if v, ok := r.buckets.Load(cellID); ok {
		return v.(*cellBucket).limiter.Allow()
	}
	// Cap distinct cells (DoS guard)
	if r.count.Load() >= int32(r.maxCells) {
		return false
	}
	b := &cellBucket{
		limiter: rate.NewLimiter(rate.Limit(r.perSecond), r.burst),
	}
	actual, loaded := r.buckets.LoadOrStore(cellID, b)
	if !loaded {
		r.count.Add(1)
	}
	return actual.(*cellBucket).limiter.Allow()
}

// Reset clears all buckets (test helper; production code should not call this).
func (r *VaultRateLimiter) Reset() {
	r.buckets.Range(func(k, _ interface{}) bool {
		r.buckets.Delete(k)
		return true
	})
	r.count.Store(0)
}

// CellCount returns the number of tracked cells (test introspection).
func (r *VaultRateLimiter) CellCount() int {
	return int(r.count.Load())
}

// Middleware returns the http handler wrapper. Reads X-Cell-ID from the
// request header. If empty or limit exceeded → 429.
func (r *VaultRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		cellID := req.Header.Get("X-Cell-ID")
		if !r.Allow(cellID) {
			actor := ""
			if id, ok := auth.CrossClusterFromContext(req.Context()); ok && id != nil {
				actor = id.Username
			}
			if r.onLimited != nil {
				r.onLimited(cellID, actor)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "rate_limited",
				"code":  "RATE_LIMITED",
			})
			return
		}
		next.ServeHTTP(w, req)
	})
}
