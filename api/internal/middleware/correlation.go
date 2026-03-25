// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"net/http"

	"github.com/loopzero/loopzero/api/internal/correlation"
)

// CorrelationID is an HTTP middleware that ensures every request has a Correlation-ID.
// If the client provides one, it is preserved. Otherwise, a new UUID v4 is generated.
// The Correlation-ID is always echoed back in the response headers.
func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("Correlation-ID")
		if id == "" {
			id = correlation.GenerateID()
			r.Header.Set("Correlation-ID", id)
		}
		w.Header().Set("Correlation-ID", id)
		next.ServeHTTP(w, r)
	})
}
