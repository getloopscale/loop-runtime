// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package policy

import "net/http"

// SecurityHeaders adds standard security response headers to every request.
//
// Headers set:
//   - X-Frame-Options: DENY (prevent clickjacking)
//   - X-Content-Type-Options: nosniff (prevent MIME sniffing)
//   - Content-Security-Policy: default-src 'self' (restrict resource loading)
//   - Referrer-Policy: strict-origin-when-cross-origin
//   - Strict-Transport-Security: max-age=63072000; includeSubDomains (HTTPS only)
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// HSTS only when the request arrived over TLS (or via a TLS-terminating
		// proxy that sets X-Forwarded-Proto).
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}
