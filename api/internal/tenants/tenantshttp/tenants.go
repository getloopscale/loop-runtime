// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package tenantshttp serves `/api/v1/tenants` — the team-facing surface, including
// the deletion request and its cancellation window.
//
// Second family to leave the flat `routes` package (B-041 slice-8), and the more
// informative of the two: unlike `version` it uses the shared transport, so it
// is the first family to declare a permitted edge to a leaf. Splitting it also
// surfaced three requirements the first family could not — see B-080.
package tenantshttp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/getloopscale/loopzero/api/internal/httpio"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	auditctx "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/ports"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	TenantDeletionStore = ports.TenantDeletionStore
	DeletionRequest     = ports.DeletionRequest
	DeletionRecord      = ports.DeletionRecord
	TombstoneRecord     = ports.TombstoneRecord
)

// TenantDeletionResponse is the 202 response for DELETE /api/v1/tenants/:id.
type TenantDeletionResponse struct {
	TenantID    string     `json:"id"`
	Status      string     `json:"status"` // "pending_deletion"
	RequestedAt time.Time  `json:"requested_at"`
	GraceUntil  *time.Time `json:"grace_until,omitempty"`
	Message     string     `json:"message"`
}

// TenantHandler handles tenant lifecycle operations.
type TenantHandler struct {
	deletionStore TenantDeletionStore
	// isAdminKey reports whether an X-Admin-Key value is the platform-admin
	// key (auth.Authenticator.ValidateAdminAPIKey in production).
	isAdminKey func(string) bool
	logger     *slog.Logger
}

// NewTenantHandler creates a new handler for tenant lifecycle operations.
func NewTenantHandler(store TenantDeletionStore, logger *slog.Logger, isAdminKey func(string) bool) *TenantHandler {
	if isAdminKey == nil {
		panic("tenants: NewTenantHandler requires the admin-key check")
	}
	return &TenantHandler{
		deletionStore: store,
		logger:        logger,
		isAdminKey:    isAdminKey,
	}
}

// Delete handles DELETE /api/v1/tenants/:id — soft-delete with grace period.
// MEET-052: API writes intent to PG, returns 202. Idempotent (second call returns same record).
func (h *TenantHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	tenantID := chi.URLParam(r, "id")
	if tenantID == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("tenant ID is required"))
		return
	}

	// B-059: declare the resource id for the audit row. `extractResourceID`
	// does not know the `tenants` collection, so tenant.deleted and
	// tenant.restored — the most destructive operations the product has —
	// recorded an empty resource_id.
	auditctx.SetResourceID(ctx, tenantID)

	// MEET-052 IDOR protection: non-admin callers can only delete their own tenant.
	// T0.2 of deep-review-remediation-plan (closes find_sec_001 CRITICAL):
	// previously used `r.Header.Get("X-Admin-Key") != ""` which checked only
	// header presence — any authenticated user could spoof. Now validates the
	// key value against AdminAPIKey via constant-time compare.
	isAdmin := h.isAdminKey(r.Header.Get("X-Admin-Key"))
	if !isAdmin && uc.TenantID != tenantID {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrForbidden)
		return
	}

	// Non-admin callers must be owners
	if !isAdmin && uc.Role != "owner" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrForbidden)
		return
	}
	ctx = actOnTenant(ctx, tenantID)

	force := r.URL.Query().Get("force") == "true"
	if force && !isAdmin {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrForbidden)
		return
	}

	// Calculate grace period: 24h for self-service, 0 for force/admin
	var graceUntil *time.Time
	if !force {
		t := time.Now().Add(24 * time.Hour)
		graceUntil = &t
	}

	reason := "user_request"
	if isAdmin {
		reason = "admin_evict"
	}

	record, err := h.deletionStore.RequestDeletion(ctx, DeletionRequest{
		TenantID:    tenantID,
		RequestedBy: uc.Email,
		Reason:      reason,
		GraceUntil:  graceUntil,
		Force:       force,
	})
	if err != nil {
		h.logger.Error("failed to request tenant deletion",
			"tenant", tenantID, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	msg := "Team scheduled for deletion"
	if record.GraceUntil != nil {
		msg = "Team scheduled for deletion. You can restore it within 24 hours using: loopzero team restore"
	}
	if force {
		msg = "Team deletion initiated (force mode, no grace period)"
	}

	resp := TenantDeletionResponse{
		TenantID:    record.TenantID,
		Status:      "pending_deletion",
		RequestedAt: record.RequestedAt,
		GraceUntil:  record.GraceUntil,
		Message:     msg,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

// Restore handles POST /api/v1/tenants/:id/restore — cancel pending deletion.
// MEET-052: Returns 200 if restored, 410 if grace expired, 404 if no pending deletion.
func (h *TenantHandler) Restore(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	tenantID := chi.URLParam(r, "id")
	if tenantID == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("tenant ID is required"))
		return
	}

	// B-059: declare the resource id for the audit row. `extractResourceID`
	// does not know the `tenants` collection, so tenant.deleted and
	// tenant.restored — the most destructive operations the product has —
	// recorded an empty resource_id.
	auditctx.SetResourceID(ctx, tenantID)

	// IDOR protection
	// T0.2 of deep-review-remediation-plan (closes find_sec_001 CRITICAL):
	// validates key value, not header presence.
	isAdmin := h.isAdminKey(r.Header.Get("X-Admin-Key"))
	if !isAdmin && uc.TenantID != tenantID {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrForbidden)
		return
	}
	ctx = actOnTenant(ctx, tenantID)

	// Check if pending deletion exists
	record, err := h.deletionStore.GetPendingDeletion(ctx, tenantID)
	if err != nil {
		h.logger.Error("failed to check pending deletion", "tenant", tenantID, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if record == nil {
		httpio.WriteErrorJSON(w, http.StatusNotFound, "NO_PENDING_DELETION",
			"No pending deletion found for this team")
		return
	}

	// Check grace period
	if record.GraceUntil != nil && time.Now().After(*record.GraceUntil) {
		httpio.WriteErrorJSON(w, http.StatusGone, "GRACE_PERIOD_EXPIRED",
			"The grace period has expired. Team deletion is in progress and cannot be cancelled.")
		return
	}

	// Cancel the deletion
	if err := h.deletionStore.CancelDeletion(ctx, tenantID); err != nil {
		h.logger.Error("failed to cancel deletion", "tenant", tenantID, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":      tenantID,
		"status":  "active",
		"message": "Team deletion cancelled. Your team is active again.",
	})
}

// actOnTenant scopes the deletion store's work to the tenant the caller was
// just authorized to act on. For an owner that is the caller's own tenant; an
// operator holding the admin key acts on another one. The store runs on the
// request pool, whose tenant tables are fail-closed under Row Level Security
// and scoped to the caller — so without this the admin's write was refused and
// the pending deletion it meant to restore was never found.
func actOnTenant(ctx context.Context, tenantID string) context.Context {
	return dbtx.WithTenantScope(ctx, tenantID)
}
