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

// ActiveTeamStore is the contract AuthHandler.Switch needs to persist
// explicit user-driven active-team selection. Plan T2.5 D8 full mode.
// Implementations write to users.active_tenant_id column (migration 000018).
type ActiveTeamStore interface {
	GetActiveTenant(ctx context.Context, userID string) (string, error)
	SetActiveTenant(ctx context.Context, userID, tenantID string) error
	// ClearActiveTenant nulls the active_tenant_id (used by EC-4 self-heal
	// when active references a deleted tenant).
	ClearActiveTenant(ctx context.Context, userID string) error
}

// AuthHandler provides HTTP handlers for authentication-related routes.
type AuthHandler struct {
	userLookup       UsernameLookup
	membershipLookup MembershipLookup
	activeStore      ActiveTeamStore
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

// WithActiveTeamStore attaches an ActiveTeamStore so /whoami and /team/switch
// can persist explicit active-team selection (plan T2.5 D8 full).
func (h *AuthHandler) WithActiveTeamStore(s ActiveTeamStore) *AuthHandler {
	h.activeStore = s
	return h
}

// SwitchTeamRequest is the JSON body for POST /api/v1/auth/team/switch.
type SwitchTeamRequest struct {
	Name string `json:"name"`
}

// SwitchTeam handles POST /api/v1/auth/team/switch.
// Updates users.active_tenant_id to the specified team after validating
// the user is a member. Returns 403 if not a member, 400 if invalid input.
// Plan T2.5 D8 full.
func (h *AuthHandler) SwitchTeam(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}
	if h.activeStore == nil || h.membershipLookup == nil {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"NOT_AVAILABLE",
			"Team switching is not configured on this server.",
			501,
		))
		return
	}

	var req SwitchTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"INVALID_REQUEST", "Invalid JSON body", 400))
		return
	}
	if req.Name == "" {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"MISSING_FIELD", "Team name is required", 400))
		return
	}

	// Validate membership before persisting.
	memberships, err := h.membershipLookup.GetMemberships(r.Context(), uc.UserID)
	if err != nil {
		slog.Error("switch team: membership lookup failed",
			"user_id", uc.UserID, "error", err)
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"INTERNAL", "Unable to verify team membership.", 500))
		return
	}
	isMember := false
	for _, m := range memberships {
		if m.TenantID == req.Name {
			isMember = true
			break
		}
	}
	if !isMember {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"NOT_MEMBER",
			"You are not a member of team \""+req.Name+"\". Run `loopzero whoami` to see your teams.",
			403,
		))
		return
	}

	if err := h.activeStore.SetActiveTenant(r.Context(), uc.UserID, req.Name); err != nil {
		slog.Error("switch team: SetActiveTenant failed",
			"user_id", uc.UserID, "tenant_id", req.Name, "error", err)
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"INTERNAL", "Unable to persist team selection.", 500))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"active_team": req.Name,
		"message":     "Active team switched to \"" + req.Name + "\".",
	})
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
	// Persistent active-team selection: when activeStore is wired, read
	// users.active_tenant_id and use it as active_team (overrides JWT
	// middleware heuristic). EC-4 self-heal: if persisted value references
	// a tenant the user is NOT a member of (e.g., tenant deleted via
	// admin), clear the stale reference and fall back to JWT context.
	activeTeam := uc.TenantID
	if h.activeStore != nil {
		if persisted, err := h.activeStore.GetActiveTenant(r.Context(), uc.UserID); err == nil && persisted != "" {
			activeTeam = persisted
		}
	}

	type teamSummary struct {
		Name   string `json:"name"`
		Role   string `json:"role"`
		Active bool   `json:"active"`
	}

	payload := map[string]any{
		"user_id":     uc.UserID,
		"email":       email,
		"role":        uc.Role,
		"active_team": activeTeam,
		"team":        activeTeam, // deprecated alias — kept one release for backwards-compat
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
			// EC-4 self-heal: validate persisted active_team is still a valid membership.
			if h.activeStore != nil && activeTeam != uc.TenantID {
				valid := false
				for _, m := range memberships {
					if m.TenantID == activeTeam {
						valid = true
						break
					}
				}
				if !valid {
					slog.Warn("whoami: active_tenant_id references non-member tenant — self-healing",
						"user_id", uc.UserID, "stale", activeTeam)
					_ = h.activeStore.ClearActiveTenant(r.Context(), uc.UserID)
					activeTeam = uc.TenantID
					payload["active_team"] = activeTeam
					payload["team"] = activeTeam
				}
			}
			teams := make([]teamSummary, 0, len(memberships))
			for _, m := range memberships {
				teams = append(teams, teamSummary{
					Name:   m.TenantID,
					Role:   m.Role,
					Active: m.TenantID == activeTeam,
				})
			}
			payload["teams"] = teams
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}
