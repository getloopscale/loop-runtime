// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"encoding/json"
	"net/http"
)

// OperationCheck defines what metric to check for a given operation.
type OperationCheck struct {
	Metric       string // e.g. "projects", "builds_per_month"
	CountFn      func(tenantID string) int // Returns current count for this tenant
}

// Middleware returns an HTTP middleware that checks plan limits.
// The tenantExtractor function extracts the tenant ID from the request.
// The upgradeURL is included in 402 responses.
func Middleware(
	limiter *PlanLimiter,
	check OperationCheck,
	tenantExtractor func(r *http.Request) string,
	upgradeURL string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := tenantExtractor(r)
			if tenantID == "" {
				next.ServeHTTP(w, r)
				return
			}

			currentCount := check.CountFn(tenantID)
			result := limiter.CheckLimit(tenantID, check.Metric, currentCount)

			if !result.Allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPaymentRequired)
				json.NewEncoder(w).Encode(FormatLimitError(result, upgradeURL))
				return
			}

			// Soft limit exceeded — allow but could log/mark for overage billing
			next.ServeHTTP(w, r)
		})
	}
}
