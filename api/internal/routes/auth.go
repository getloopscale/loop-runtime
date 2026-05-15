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

// MembershipLookup is the narrow contract AuthHandler.Whoami needs to surface
// ALL of a user's tenant memberships, not just the "active" one selected by
// the middleware (plan T2.5 D8 — closes F-S2-02 silent team swap). Implemented
// by db.PostgresMembershipStore.GetMemberships. A nil lookup is legal — the
// handler degrades gracefully to single-team output.
type MembershipLookup interface {
	GetMemberships(ctx context.Context, userID string) ([]middleware.TenantMembership, error)
}

// AuthHandler provides HTTP handlers for authentication-related routes.
type AuthHandler struct {
	userLookup       UsernameLookup
	membershipLookup MembershipLookup
}

// NewAuthHandler creates a new AuthHandler. `userLookup` may be nil — when
// absent, the response will not carry a `username` field (callers fall back
// to email/user_id). `membershipLookup` may also be nil — without it, the
// response surfaces only the active team (legacy behavior).
func NewAuthHandler(userLookup UsernameLookup) *AuthHandler {
	return &AuthHandler{userLookup: userLookup}
}

// WithMembershipLookup attaches a membership store so /whoami can list ALL
// of a user's tenant memberships (plan T2.5 D8). Returns the receiver for
// chaining.
func (h *AuthHandler) WithMembershipLookup(m MembershipLookup) *AuthHandler {
	h.membershipLookup = m
	return h
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

	// Plan T2.5 D8 — surface ALL memberships + active_team so multi-tenant
	// users see the silent-swap behavior explicitly (closes F-S2-02).
	type teamSummary struct {
		Name   string `json:"name"`
		Role   string `json:"role"`
		Active bool   `json:"active"`
	}
	payload := map[string]any{
		"user_id":     uc.UserID,
		"email":       email,
		"team":        uc.TenantID, // deprecated alias for active_team — kept one release
		"role":        uc.Role,
		"active_team": uc.TenantID,
	}
	if username != "" {
		payload["username"] = username
	}
	if h.membershipLookup != nil {
		memberships, err := h.membershipLookup.GetMemberships(r.Context(), uc.UserID)
		if err != nil {
			slog.Warn("whoami: membership lookup failed (non-fatal)",
				"user_id", uc.UserID, "error", err.Error())
		} else {
			teams := make([]teamSummary, 0, len(memberships))
			for _, m := range memberships {
				teams = append(teams, teamSummary{
					Name:   m.TenantID,
					Role:   m.Role,
					Active: m.TenantID == uc.TenantID,
				})
			}
			payload["teams"] = teams
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}
