// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"encoding/json"
	"net/http"
)

// Role hierarchy: owner > admin > member.
// §5.1: Explicit permission matrix.
//   owner: all operations (deploy, delete, manage members, billing)
//   admin: deploy, manage secrets, rollback (no billing, no member management)
//   member: deploy, view logs, view status (no delete, no secrets, no rollback)

var roleLevel = map[string]int{
	"owner":  3,
	"admin":  2,
	"member": 1,
}

// HasMinRole checks if the user's role meets or exceeds the minimum required.
func HasMinRole(userRole, minRole string) bool {
	return roleLevel[userRole] >= roleLevel[minRole]
}

// RequireRole returns middleware that enforces a minimum role.
// Must be used after RequireJWT (needs UserContext with Role).
func RequireRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUser(r.Context())
			if user == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"code":    "AUTH_REQUIRED",
					"message": "Not authenticated. Run: loopzero login",
				})
				return
			}

			if !HasMinRole(user.Role, minRole) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{
					"code":    "INSUFFICIENT_ROLE",
					"message": "Requires " + minRole + " role. Your role: " + user.Role,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
