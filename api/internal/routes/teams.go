// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// ErrMemberNotFound is returned when a team member does not exist.
var ErrMemberNotFound = errors.New("member not found")

// validRoles defines the allowed team member roles.
var validRoles = map[string]bool{
	"owner":  true,
	"admin":  true,
	"member": true,
}

// TeamMemberInfo represents a team member in API responses.
// Uses route-level types to avoid importing db (DIP).
type TeamMemberInfo struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
}

// TeamUserInfo represents a platform user (for add-member lookup).
type TeamUserInfo struct {
	ID          string
	Email       string
	DisplayName string
}

// TeamStore abstracts team member persistence (DIP).
type TeamStore interface {
	ListMembers(ctx context.Context, tenantID string) ([]TeamMemberInfo, error)
	AddMember(ctx context.Context, tenantID, userID, role string) error
	RemoveMember(ctx context.Context, tenantID, userID string) error
	UpdateMemberRole(ctx context.Context, tenantID, userID, newRole string) error
	GetMember(ctx context.Context, tenantID, userID string) (*TeamMemberInfo, error)
	GetUserByUsername(ctx context.Context, username string) (*TeamUserInfo, error)
}

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

	// Validate role
	if !validRoles[req.Role] {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap("role must be one of: owner, admin, member"))
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

	// Look up the target member
	targetUserID := "github-" + username
	targetMember, err := h.store.GetMember(ctx, uc.TenantID, targetUserID)
	if err != nil {
		slog.Error("failed to get target member", "error", err.Error())
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Owner protection: cannot remove the owner
	if targetMember != nil && targetMember.Role == "owner" {
		writeLoopZeroError(w, loopzeroerrors.ErrForbidden.Wrap("the team owner cannot be removed"))
		return
	}

	// Admin cannot remove another admin (only owner can)
	if callerMember.Role == "admin" && targetMember != nil && targetMember.Role == "admin" {
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

	// Validate role
	if !validRoles[req.Role] {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap("role must be one of: owner, admin, member"))
		return
	}

	// Owner protection: cannot change the owner's role
	targetUserID := "github-" + username
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
