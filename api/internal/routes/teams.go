// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/domain"
	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// ErrMemberNotFound is an alias for domain.ErrMemberNotFound (backward compatibility).
var ErrMemberNotFound = domain.ErrMemberNotFound

// validRoles defines the allowed team member roles.
var validRoles = map[string]bool{
	"owner":  true,
	"admin":  true,
	"member": true,
}

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	TeamMemberInfo = domain.TeamMemberInfo
	TeamUserInfo   = domain.TeamUserInfo
	TeamStore      = domain.TeamStore
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

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	members, err := h.store.ListMembers(ctx, uc.TenantID)
	if err != nil {
		slog.Error("failed to list team members",
			"tenant_id", uc.TenantID,
			"error", err.Error(),
		)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"members": members,
	})
}

// addMemberRequest is the JSON request body for POST /api/v1/teams/members.
type addMemberRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Add handles POST /api/v1/teams/members.
// Adds a user to the team. Only owner or admin can add members.
func (h *TeamHandler) Add(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	// Authorization: only owner or admin can add members
	callerMember, err := h.store.GetMember(ctx, uc.TenantID, uc.UserID)
	if err != nil {
		slog.Error("failed to get caller member", "error", err.Error())
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if callerMember == nil || (callerMember.Role != "owner" && callerMember.Role != "admin") {
		writeLoopZeroError(w, loopzeroerrors.ErrForbidden.Wrap("only owners and admins can add team members"))
		return
	}

	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Validate required fields
	if req.Username == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("username is required"))
		return
	}
	if req.Role == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("role is required"))
		return
	}

	// Validate role — SECURITY: "owner" cannot be assigned via API (privilege escalation).
	// Ownership is set only during bootstrap. R-01 fix.
	if !validRoles[req.Role] || req.Role == "owner" {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap("role must be one of: admin, member"))
		return
	}

	// Look up user by username
	user, err := h.store.GetUserByUsername(ctx, req.Username)
	if err != nil {
		slog.Error("failed to look up user", "username", req.Username, "error", err.Error())
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if user == nil {
		writeLoopZeroError(w, &loopzeroerrors.LoopZeroError{
			Code:       "USER_NOT_FOUND",
			Message:    "user '" + req.Username + "' not found — they must run `loopzero login` first",
			HTTPStatus: http.StatusNotFound,
		})
		return
	}

	if err := h.store.AddMember(ctx, uc.TenantID, user.ID, req.Role); err != nil {
		slog.Error("failed to add team member",
			"tenant_id", uc.TenantID,
			"username", req.Username,
			"error", err.Error(),
		)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"username": req.Username,
		"role":     req.Role,
	})
}

// Remove handles DELETE /api/v1/teams/members/{username}.
// Removes a user from the team. Owner or admin can remove (admin cannot remove another admin).
func (h *TeamHandler) Remove(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	username := chi.URLParam(r, "username")
	if username == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("username is required"))
		return
	}

	// Authorization: only owner or admin can remove members
	callerMember, err := h.store.GetMember(ctx, uc.TenantID, uc.UserID)
	if err != nil {
		slog.Error("failed to get caller member", "error", err.Error())
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if callerMember == nil || (callerMember.Role != "owner" && callerMember.Role != "admin") {
		writeLoopZeroError(w, loopzeroerrors.ErrForbidden.Wrap("only owners and admins can remove team members"))
		return
	}

	// Look up target user (AUDIT-7: provider-agnostic — no "github-" assumption)
	targetUser, err := h.store.GetUserByUsername(ctx, username)
	if err != nil {
		slog.Error("failed to look up target user", "error", err.Error())
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if targetUser == nil {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"TEAM_MEMBER_NOT_FOUND",
			fmt.Sprintf("User %q is not a team member.", username),
			http.StatusNotFound,
		))
		return
	}
	targetUserID := targetUser.ID

	targetMember, err := h.store.GetMember(ctx, uc.TenantID, targetUserID)
	if err != nil {
		slog.Error("failed to get target member", "error", err.Error())
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// AUDIT-6: fail fast with 404 when user exists but is not a team member.
	if targetMember == nil {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"TEAM_MEMBER_NOT_FOUND",
			fmt.Sprintf("User %q is not a team member.", username),
			http.StatusNotFound,
		))
		return
	}

	// Owner protection: cannot remove the owner
	if targetMember.Role == "owner" {
		writeLoopZeroError(w, loopzeroerrors.ErrForbidden.Wrap("the team owner cannot be removed"))
		return
	}

	// Admin cannot remove another admin (only owner can)
	if callerMember.Role == "admin" && targetMember.Role == "admin" {
		writeLoopZeroError(w, loopzeroerrors.ErrForbidden.Wrap("admins cannot remove other admins — only the owner can"))
		return
	}

	if err := h.store.RemoveMember(ctx, uc.TenantID, targetUserID); err != nil {
		slog.Error("failed to remove team member",
			"tenant_id", uc.TenantID,
			"username", username,
			"error", err.Error(),
		)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"removed": username,
	})
}

// updateRoleRequest is the JSON request body for PATCH /api/v1/teams/members/{username}.
type updateRoleRequest struct {
	Role string `json:"role"`
}

// UpdateRole handles PATCH /api/v1/teams/members/{username}.
// Changes a member's role. Only the owner can change roles.
func (h *TeamHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	username := chi.URLParam(r, "username")
	if username == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("username is required"))
		return
	}

	// Authorization: only owner can change roles
	callerMember, err := h.store.GetMember(ctx, uc.TenantID, uc.UserID)
	if err != nil {
		slog.Error("failed to get caller member", "error", err.Error())
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if callerMember == nil || callerMember.Role != "owner" {
		writeLoopZeroError(w, loopzeroerrors.ErrForbidden.Wrap("only the team owner can change member roles"))
		return
	}

	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Validate role — SECURITY: "owner" cannot be assigned via API (privilege escalation). R-01 fix.
	if !validRoles[req.Role] || req.Role == "owner" {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap("role must be one of: admin, member"))
		return
	}

	// Look up target user (AUDIT-7: provider-agnostic — no "github-" assumption)
	targetUser, err := h.store.GetUserByUsername(ctx, username)
	if err != nil {
		slog.Error("failed to look up target user", "error", err.Error())
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if targetUser == nil {
		writeLoopZeroError(w, &loopzeroerrors.LoopZeroError{
			Code:       "MEMBER_NOT_FOUND",
			Message:    "member '" + username + "' not found in this team",
			HTTPStatus: http.StatusNotFound,
		})
		return
	}
	targetUserID := targetUser.ID

	// Owner protection: cannot change the owner's role
	targetMember, err := h.store.GetMember(ctx, uc.TenantID, targetUserID)
	if err != nil {
		slog.Error("failed to get target member", "error", err.Error())
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if targetMember != nil && targetMember.Role == "owner" {
		writeLoopZeroError(w, loopzeroerrors.ErrForbidden.Wrap("the team owner's role cannot be changed"))
		return
	}

	if err := h.store.UpdateMemberRole(ctx, uc.TenantID, targetUserID, req.Role); err != nil {
		if errors.Is(err, ErrMemberNotFound) {
			writeLoopZeroError(w, &loopzeroerrors.LoopZeroError{
				Code:       "MEMBER_NOT_FOUND",
				Message:    "member '" + username + "' not found in this team",
				HTTPStatus: http.StatusNotFound,
			})
			return
		}
		slog.Error("failed to update member role",
			"tenant_id", uc.TenantID,
			"username", username,
			"error", err.Error(),
		)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"username": username,
		"role":     req.Role,
	})
}
