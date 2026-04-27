// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// UsernameLookup is the narrow contract AuthHandler.Whoami needs to enrich
// the response with a display-friendly username (AUDIT-10). Implemented by
// db.PostgresTeamStore.GetUsernameByID. A nil lookup is legal — the handler
// simply omits the username field.
type UsernameLookup interface {
	GetUsernameByID(ctx context.Context, userID string) (string, error)
}

// AuthHandler provides HTTP handlers for authentication-related routes.
type AuthHandler struct {
	userLookup UsernameLookup
}

// NewAuthHandler creates a new AuthHandler. `userLookup` may be nil — when
// absent, the response will not carry a `username` field (callers fall back
// to email/user_id).
func NewAuthHandler(userLookup UsernameLookup) *AuthHandler {
	return &AuthHandler{userLookup: userLookup}
}

// Whoami returns the authenticated user's information.
// GET /api/v1/auth/whoami
//
// AUDIT-10: response now includes `username` when available. Consumers can
// use it as display identity when `email` is empty (OAuth2 client_credentials
// tokens have no email claim).
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

	// AUDIT-10: look up username column populated by migration 000037.
	// Non-fatal on lookup error — we still return the rest of the identity.
	var username string
	if h.userLookup != nil {
		u, err := h.userLookup.GetUsernameByID(r.Context(), uc.UserID)
		if err != nil {
			slog.Warn("whoami: username lookup failed (non-fatal)",
				"user_id", uc.UserID, "error", err.Error())
		} else {
			username = u
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	payload := map[string]string{
		"user_id": uc.UserID,
		"email":   email,
		"team":    uc.TenantID,
		"role":    uc.Role,
	}
	if username != "" {
		payload["username"] = username
	}
	_ = json.NewEncoder(w).Encode(payload)
}
