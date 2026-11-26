// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package observability

import (
	"net/http"

	"github.com/getloopscale/loop-runtime/pkg/correlation"
)

// CorrelationID is an HTTP middleware that ensures every request has a Correlation-ID.
// If the client provides one, it is preserved. Otherwise, a new UUID v4 is generated.
// The ID is echoed in response headers AND injected into the request context.
//
// T5.6 of deep-review-remediation-plan (closes find_obs_002 HIGH):
// previously the ID lived only in headers; only 5/32 routes propagated it
// to slog. Context propagation makes the ID universally available from
// the middleware boundary onward — services + DB stores extract via
// correlation.IDFromContext(ctx) without re-reading the *http.Request.
func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("Correlation-ID")
		if id == "" {
			id = correlation.GenerateID()
			r.Header.Set("Correlation-ID", id)
		}
		w.Header().Set("Correlation-ID", id)
		ctx := correlation.WithID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
