// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package auth serves the identity endpoints under /api/v1/auth: whoami, which
// answers "who am I and which teams do I belong to", and switch-team, which moves
// the caller's active team.
//
// Thirteenth family to leave the flat `routes` package (B-080, slice-34),
// extracted alongside `teams`, and the two had to move together: they shared
// `auth_whoami_teams_test.go`, ten tests that read like they belong to both.
// Measured, all ten exercise handlers declared HERE — the filename said so and
// the compiler confirmed it. A shared test file is a question about ownership,
// and the answer came from where the handlers live rather than from the name.
package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getloopscale/loopzero/api/internal/httpio"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// UsernameLookup is the narrow contract AuthHandler.Whoami needs to enrich
// the response with a display-friendly username (AUDIT-10). Implemented by
// store.PostgresTeamStore.GetUsernameByID. A nil lookup is legal — the handler
// simply omits the username field.
type UsernameLookup interface {
	GetUsernameByID(ctx context.Context, userID string) (string, error)
}

// MembershipLookup is the narrow contract AuthHandler.Whoami needs to surface
// ALL of a user's tenant memberships, not just the "active" one selected by
// the middleware (plan T2.5 D8 — closes F-S2-02 silent team swap). Implemented
// by store.PostgresMembershipStore.GetMemberships. A nil lookup is legal — the
// handler degrades gracefully to single-team output.
type MembershipLookup interface {
	GetMemberships(ctx context.Context, userID string) ([]auth.TenantMembership, error)
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
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}
	if h.activeStore == nil || h.membershipLookup == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"NOT_AVAILABLE",
			"Team switching is not configured on this server."))
		return
	}

	var req SwitchTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"INVALID_REQUEST", "Invalid JSON body"))
		return
	}
	if req.Name == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"MISSING_FIELD", "Team name is required"))
		return
	}

	// Validate membership before persisting.
	memberships, err := h.membershipLookup.GetMemberships(r.Context(), uc.UserID)
	if err != nil {
		slog.Error("switch team: membership lookup failed",
			"user_id", uc.UserID, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError.Wrap("Unable to verify team membership."))
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
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"NOT_MEMBER",
			"You are not a member of team \""+req.Name+"\". Run `loopzero whoami` to see your teams."))
		return
	}

	if err := h.activeStore.SetActiveTenant(r.Context(), uc.UserID, req.Name); err != nil {
		slog.Error("switch team: SetActiveTenant failed",
			"user_id", uc.UserID, "tenant_id", req.Name, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError.Wrap("Unable to persist team selection."))
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
// Plan 2026-06-15-code-review-49-findings-remediation T5.4 (R2-F3 HIGH):
// CC 18 → ≤ 10 via 3 helper extracts (deriveEmail + lookupUsername +
// resolveActiveTeam). Behavior preserved verbatim.
func (h *AuthHandler) Whoami(w http.ResponseWriter, r *http.Request) {
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	email := deriveEmail(uc.Email, uc.UserID)
	username := h.lookupUsername(r.Context(), uc.UserID)
	activeTeam := h.resolveActiveTeam(r.Context(), uc.UserID, uc.TenantID)

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
			activeTeam = h.selfHealActiveTeam(r.Context(), uc.UserID, uc.TenantID, activeTeam, memberships, payload)
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

// deriveEmail returns the user's email, falling back to a noreply derived
// from the UserID for GitHub identities that have no public email.
func deriveEmail(email, userID string) string {
	if email != "" {
		return email
	}
	if login := strings.TrimPrefix(userID, "github-"); login != userID {
		return login + "@users.noreply.github.com"
	}
	return ""
}

// lookupUsername queries the username column populated by migration 000037.
// Non-fatal on lookup error — returns empty so the rest of the identity
// still surfaces (AUDIT-10).
func (h *AuthHandler) lookupUsername(ctx context.Context, userID string) string {
	if h.userLookup == nil {
		return ""
	}
	u, err := h.userLookup.GetUsernameByID(ctx, userID)
	if err != nil {
		slog.Warn("whoami: username lookup failed (non-fatal)",
			"user_id", userID, "error", err.Error())
		return ""
	}
	return u
}

// resolveActiveTeam returns the persisted active_tenant_id when present,
// or the JWT-derived tenant ID as fallback. The EC-4 self-heal is
// applied separately in selfHealActiveTeam (it depends on memberships).
func (h *AuthHandler) resolveActiveTeam(ctx context.Context, userID, fallback string) string {
	if h.activeStore == nil {
		return fallback
	}
	persisted, err := h.activeStore.GetActiveTenant(ctx, userID)
	if err != nil || persisted == "" {
		return fallback
	}
	return persisted
}

// selfHealActiveTeam implements the EC-4 self-heal: if the persisted
// active_team references a tenant the user is NOT a member of (e.g.,
// admin-deleted), clear the stale reference and fall back to the JWT
// tenant. Returns the (possibly corrected) active team. Mutates payload
// in place when self-heal fires.
func (h *AuthHandler) selfHealActiveTeam(
	ctx context.Context,
	userID, jwtTenantID, activeTeam string,
	memberships []auth.TenantMembership,
	payload map[string]any,
) string {
	if h.activeStore == nil || activeTeam == jwtTenantID {
		return activeTeam
	}
	for _, m := range memberships {
		if m.TenantID == activeTeam {
			return activeTeam
		}
	}
	slog.Warn("whoami: active_tenant_id references non-member tenant — self-healing",
		"user_id", userID, "stale", activeTeam)
	_ = h.activeStore.ClearActiveTenant(ctx, userID)
	payload["active_team"] = jwtTenantID
	payload["team"] = jwtTenantID
	return jwtTenantID
}
