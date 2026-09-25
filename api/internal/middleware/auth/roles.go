// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"net/http"
)

// §5.1: Role hierarchy for authorization enforcement.
//   owner > admin > member

var roleLevel = map[string]int{
	"owner":  3,
	"admin":  2,
	"member": 1,
}

// IsKnownRole reports whether role is one of the team roles. The role
// vocabulary is roleLevel; nothing else keeps a copy of it.
func IsKnownRole(role string) bool {
	_, ok := roleLevel[role]
	return ok
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
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated.")
				return
			}

			if !HasMinRole(uc.Role, minRole) {
				writeError(w, http.StatusForbidden, "INSUFFICIENT_ROLE",
					"Requires "+minRole+" role. Your role: "+uc.Role)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
