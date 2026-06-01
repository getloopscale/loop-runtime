// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"context"
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

// RoleResolver resolves a user's role from the authoritative store (the
// database), keyed by the user's subject. Per S2.3, role is NEVER trusted from
// the token — it is always resolved server-side so that role changes take
// effect immediately and a stale/forged token cannot escalate privileges.
type RoleResolver interface {
	GetRole(ctx context.Context, userID string) (string, error)
}

// RequireRole returns middleware that enforces a minimum role, resolving the
// caller's role from the authoritative store (not the token). Must run after
// RequireJWT (needs UserContext.Subject). The resolved role is stamped into the
// UserContext for downstream handlers.
func RequireRole(resolver RoleResolver, minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUser(r.Context())
			if user == nil {
				writeRoleError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
				return
			}

			role, err := resolver.GetRole(r.Context(), user.Subject)
			if err != nil {
				writeRoleError(w, http.StatusBadGateway, "SERVICE_ERROR", "Could not verify permissions. Try again.")
				return
			}

			if !HasMinRole(role, minRole) {
				writeRoleError(w, http.StatusForbidden, "INSUFFICIENT_ROLE", "Requires "+minRole+" role.")
				return
			}

			user.Role = role // stamp resolved role for downstream handlers
			next.ServeHTTP(w, r)
		})
	}
}

func writeRoleError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}
