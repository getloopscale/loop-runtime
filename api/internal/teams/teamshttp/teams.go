// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package teamshttp serves the membership endpoints under /api/v1/teams: who belongs
// to a team, and the invite and removal paths.
//
// Fourteenth family to leave the flat `routes` package (B-080, slice-34).
//
// `TeamMemberInfo` and `TeamUserInfo` turned out to be aliases re-exported from
// `internal/domain`, like three types in the previous slice; consumers now name
// the originals. Fourth slice running where the compiler surfaced an alias the
// fence could not see.
package teamshttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"
	"github.com/getloopscale/loop-runtime/api/internal/teams"

	"github.com/go-chi/chi/v5"

	auditctx "github.com/getloopscale/loop-runtime/api/internal/middleware/audit"
	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// ports.ErrMemberNotFound is an alias for ports.ErrMemberNotFound (backward compatibility).

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	TeamMemberInfo = ports.TeamMemberInfo
	TeamUserInfo   = ports.TeamUserInfo
	TeamStore      = teams.TeamStore
)

// TeamHandler provides HTTP handlers for team member operations.
type TeamHandler struct {
	store TeamStore
}

// NewTeamHandler creates a new TeamHandler.
func NewTeamHandler(store TeamStore) *TeamHandler {
	return &TeamHandler{store: store}
}

// List handles GET /api/v1/teams/members.
// Returns all members of the authenticated tenant's team.
// Any team member can list.
func (h *TeamHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	members, err := h.store.ListMembers(ctx, uc.TenantID)
	if err != nil {
		slog.Error("failed to list team members",
			"tenant_id", uc.TenantID,
			"error", err.Error(),
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	httpio.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"members": members,
	})
}

// addMemberRequest is the JSON request body for POST /api/v1/teams/members.
type addMemberRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Add handles POST /api/v1/teams/members.
// Adds a user to the team. Only owner or admin can add members — enforced at
// registration by auth.RequireRole("admin").
func (h *TeamHandler) Add(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Validate required fields
	if req.Username == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("username is required"))
		return
	}

	// B-059: declare the resource id for the audit row. member.removed and
	// member.role_changed already carry the username (it is in their URL);
	// member.added carried nothing, so the three could not be joined.
	auditctx.SetResourceID(ctx, req.Username)
	if req.Role == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("role is required"))
		return
	}

	// Validate role — SECURITY: "owner" cannot be assigned via API (privilege escalation).
	// Ownership is set only during bootstrap. R-01 fix.
	if !auth.IsKnownRole(req.Role) || req.Role == "owner" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap("role must be one of: admin, member"))
		return
	}

	// Look up user by username
	user, err := h.store.GetUserByUsername(ctx, req.Username)
	if err != nil {
		slog.Error("failed to look up user", "username", req.Username, "error", err.Error())
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if user == nil {
		httpio.WriteLoopZeroError(w, &loopzeroerrors.LoopZeroError{
			Code:    "USER_NOT_FOUND",
			Message: "user '" + req.Username + "' not found — they must run `loopzero login` first",
		})
		return
	}

	if err := h.store.AddMember(ctx, uc.TenantID, user.ID, req.Role); err != nil {
		slog.Error("failed to add team member",
			"tenant_id", uc.TenantID,
			"username", req.Username,
			"error", err.Error(),
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	httpio.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"username": req.Username,
		"role":     req.Role,
	})
}

// Remove handles DELETE /api/v1/teams/members/{username}.
// Removes a user from the team. Owner or admin can remove (admin cannot remove another admin).
func (h *TeamHandler) Remove(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	username := chi.URLParam(r, "username")
	if username == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("username is required"))
		return
	}

	// Look up target user (AUDIT-7: provider-agnostic — no "github-" assumption)
	targetUser, err := h.store.GetUserByUsername(ctx, username)
	if err != nil {
		slog.Error("failed to look up target user", "error", err.Error())
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if targetUser == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"TEAM_MEMBER_NOT_FOUND",
			fmt.Sprintf("User %q is not a team member.", username)))
		return
	}
	targetUserID := targetUser.ID

	targetMember, err := h.store.GetMember(ctx, uc.TenantID, targetUserID)
	if err != nil {
		slog.Error("failed to get target member", "error", err.Error())
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// AUDIT-6: fail fast with 404 when user exists but is not a team member.
	if targetMember == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"TEAM_MEMBER_NOT_FOUND",
			fmt.Sprintf("User %q is not a team member.", username)))
		return
	}

	// Owner protection: cannot remove the owner
	if targetMember.Role == "owner" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrForbidden.Wrap("the team owner cannot be removed"))
		return
	}

	// Admin cannot remove another admin (only owner can)
	if uc.Role == "admin" && targetMember.Role == "admin" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrForbidden.Wrap("admins cannot remove other admins — only the owner can"))
		return
	}

	if err := h.store.RemoveMember(ctx, uc.TenantID, targetUserID); err != nil {
		slog.Error("failed to remove team member",
			"tenant_id", uc.TenantID,
			"username", username,
			"error", err.Error(),
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	httpio.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"removed": username,
	})
}

// updateRoleRequest is the JSON request body for PATCH /api/v1/teams/members/{username}.
type updateRoleRequest struct {
	Role string `json:"role"`
}

// UpdateRole handles PATCH /api/v1/teams/members/{username}.
// Changes a member's role. Only the owner can change roles — enforced at
// registration by auth.RequireRole("owner"), the one role gate.
func (h *TeamHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	username := chi.URLParam(r, "username")
	if username == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("username is required"))
		return
	}

	role, perr := parseRoleRequest(r)
	if perr != nil {
		httpio.WriteLoopZeroError(w, perr)
		return
	}

	targetUserID, terr := h.resolveAndValidateTarget(ctx, uc.TenantID, username)
	if terr != nil {
		httpio.WriteLoopZeroError(w, terr)
		return
	}

	if err := h.store.UpdateMemberRole(ctx, uc.TenantID, targetUserID, role); err != nil {
		if errors.Is(err, ports.ErrMemberNotFound) {
			httpio.WriteLoopZeroError(w, &loopzeroerrors.LoopZeroError{
				Code:    "MEMBER_NOT_FOUND",
				Message: "member '" + username + "' not found in this team",
			})
			return
		}
		slog.Error("failed to update member role",
			"tenant_id", uc.TenantID,
			"username", username,
			"error", err.Error(),
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	httpio.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"username": username,
		"role":     role,
	})
}

// parseRoleRequest decodes the JSON body and validates the role enum.
// SECURITY: "owner" cannot be assigned via API (privilege escalation, R-01).
func parseRoleRequest(r *http.Request) (string, *loopzeroerrors.LoopZeroError) {
	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", loopzeroerrors.ErrInvalidRequest
	}
	if !auth.IsKnownRole(req.Role) || req.Role == "owner" {
		return "", loopzeroerrors.ErrInvalidRequest.Wrap("role must be one of: admin, member")
	}
	return req.Role, nil
}

// resolveAndValidateTarget looks up the target user by username (AUDIT-7:
// provider-agnostic) and enforces the owner-protection invariant (the
// owner's role cannot be changed).
func (h *TeamHandler) resolveAndValidateTarget(ctx context.Context, tenantID, username string) (string, *loopzeroerrors.LoopZeroError) {
	targetUser, err := h.store.GetUserByUsername(ctx, username)
	if err != nil {
		slog.Error("failed to look up target user", "error", err.Error())
		return "", loopzeroerrors.ErrInternalError
	}
	if targetUser == nil {
		return "", &loopzeroerrors.LoopZeroError{
			Code:    "MEMBER_NOT_FOUND",
			Message: "member '" + username + "' not found in this team",
		}
	}
	targetMember, err := h.store.GetMember(ctx, tenantID, targetUser.ID)
	if err != nil {
		slog.Error("failed to get target member", "error", err.Error())
		return "", loopzeroerrors.ErrInternalError
	}
	if targetMember != nil && targetMember.Role == "owner" {
		return "", loopzeroerrors.ErrForbidden.Wrap("the team owner's role cannot be changed")
	}
	return targetUser.ID, nil
}
