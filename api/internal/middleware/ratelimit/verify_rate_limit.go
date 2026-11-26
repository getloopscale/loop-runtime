// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package ratelimit

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/api/internal/middleware/slidingwindow"
	"github.com/go-chi/chi/v5"
)

// VerifyRateLimiter wraps RateLimiter providing route-scoped rate-limit for
// POST /api/v1/domains/{domain}/verify keyed by (tenant, domain) per the
// LOCKED j11-domains rate-limit ADR D7 (1 reverify per 30 seconds per
// (tenant, domain)).
//
// Composition over duplication (ADR D1 of m3-verify-rate-limit-middleware
// plan): VerifyRateLimiter is NOT a NEW limiter type — it reuses
// RateLimiter.Allow() internally. The wrapper only changes (a) the key
// extraction strategy (tenant+domain instead of tenant:user) + (b) the
// rate/window defaults (1 req / 30s instead of 60/min).
type VerifyRateLimiter struct {
	rl *RateLimiter
}

// NewVerifyRateLimiter creates a VerifyRateLimiter with the given config.
// Canonical config per the J11D7 LOCKED ADR: rate=1, window=30*time.Second.
func NewVerifyRateLimiter(rate int, window time.Duration) *VerifyRateLimiter {
	return &VerifyRateLimiter{rl: NewRateLimiter(rate, window)}
}

// Middleware returns chi-compatible HTTP middleware. Key = "tenant:domain".
//
// Fail-open semantic per ADR D3 of the plan — when UserContext OR chi URL
// param `{domain}` are missing (incl. UserContext with empty TenantID per
// EC-4 absorption v1.1), the request is passed downstream (which returns
// 401 OR 400). Rate-limit middleware does NOT enforce auth/validation
// invariants.
func (v *VerifyRateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uc := auth.GetUserContext(r.Context())
			domain := chi.URLParam(r, "domain")
			// EC-4 absorption (v1.1): guard empty TenantID. Non-nil UserContext
			// with TenantID="" would create cross-tenant key collision
			// ":domain" — 2 unauthenticated tenants share the bucket.
			if uc == nil || uc.TenantID == "" || domain == "" {
				slog.WarnContext(r.Context(), "verify rate-limit fail-open",
					"missing_user_context", uc == nil,
					"missing_tenant_id", uc != nil && uc.TenantID == "",
					"missing_domain_param", domain == "")
				next.ServeHTTP(w, r)
				return
			}
			key := uc.TenantID + ":" + domain
			if ok, retryAfter := v.rl.counter.Allow(key); !ok {
				metrics.DomainVerifyRateLimitedTotal.WithLabelValues(uc.TenantID).Inc()

				w.Header().Set("Content-Type", "application/json")
				slidingwindow.SetRetryAfter(w, retryAfter)
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":                "RATE_LIMITED",
					"message":             "Too many verification attempts for this domain. Retry after the Retry-After delay.",
					"retry_after_seconds": slidingwindow.RetryAfterSeconds(retryAfter),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
