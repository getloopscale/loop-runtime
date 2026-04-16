// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// AuthHandler provides HTTP handlers for authentication-related routes.
type AuthHandler struct{}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler() *AuthHandler {
	return &AuthHandler{}
}

// Whoami returns the authenticated user's information.
// GET /api/v1/auth/whoami
func (h *AuthHandler) Whoami(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	// GitHub /user returns email:null when account email is private (default).
	// Derive a noreply email from the UserID so clients always get a usable value.
	email := uc.Email
	if email == "" {
		if login := strings.TrimPrefix(uc.UserID, "github-"); login != uc.UserID {
			email = login + "@users.noreply.github.com"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"user_id": uc.UserID,
		"email":   email,
		"team":    uc.TenantID,
		"role":    uc.Role,
	})
}
