// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"encoding/json"
	"net/http"
)

// §5.1: Role hierarchy for authorization enforcement.
//   owner > admin > member

var roleLevel = map[string]int{
	"owner":  3,
	"admin":  2,
	"member": 1,
}

// HasMinRole checks if the user's role meets or exceeds the minimum required.
func HasMinRole(userRole, minRole string) bool {
	return roleLevel[userRole] >= roleLevel[minRole]
}

// RequireRole returns middleware that enforces a minimum role level.
// Must be used after Auth middleware (needs UserContext with Role).
func RequireRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uc := GetUserContext(r.Context())
			if uc == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"code":    "UNAUTHORIZED",
					"message": "Not authenticated.",
				})
				return
			}

			if !HasMinRole(uc.Role, minRole) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{
					"code":    "INSUFFICIENT_ROLE",
					"message": "Requires " + minRole + " role. Your role: " + uc.Role,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
