// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"net/http"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

// MeHandler returns user info for the authenticated user.
type MeHandler struct{}

// NewMeHandler creates a handler for /v1/auth/me.
func NewMeHandler() *MeHandler {
	return &MeHandler{}
}

// Get returns the authenticated user's info.
// GET /v1/auth/me — requires auth.RequireJWT middleware.
func (h *MeHandler) Get(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"email":   user.Email,
		"name":    user.Name,
		"team":    user.TenantID,
		"user_id": user.Subject,
	})
}

func extractBearerToken(r *http.Request) string {
	a := r.Header.Get("Authorization")
	if len(a) > 7 && a[:7] == "Bearer " {
		return a[7:]
	}
	return ""
}
