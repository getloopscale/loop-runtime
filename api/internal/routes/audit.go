// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/loopzero/loopzero/api/internal/middleware"
)

// AuditStore abstracts audit log persistence.
type AuditStore interface {
	ListAuditEntries(ctx context.Context, tenantID string, filter AuditFilter) ([]AuditEntryResult, int, error)
}

// AuditFilter defines query parameters for audit log listing.
type AuditFilter struct {
	Action       string
	ResourceType string
	Since        time.Time
	Limit        int
	Offset       int
}

// AuditEntryResult represents a single audit log entry in API responses.
type AuditEntryResult struct {
	Action        string         `json:"action"`
	ResourceType  string         `json:"resource_type"`
	ResourceID    string         `json:"resource_id"`
	Actor         string         `json:"actor"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"timestamp"`
}

// AuditHandler handles GET /api/v1/audit.
type AuditHandler struct {
	store AuditStore
}

// NewAuditHandler creates an audit log handler.
func NewAuditHandler(store AuditStore) *AuditHandler {
	return &AuditHandler{store: store}
}

// List returns paginated audit entries for the authenticated tenant.
// Requires owner or admin role.
// GET /api/v1/audit?action=project.created&resource_type=project&since=2026-04-01&limit=50&offset=0
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		jsonError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated.")
		return
	}

	// Require admin or owner role
	if uc.Role != "owner" && uc.Role != "admin" {
		jsonError(w, http.StatusForbidden, "INSUFFICIENT_ROLE", "Audit log requires admin or owner role.")
		return
	}

	filter := AuditFilter{
		Action:       r.URL.Query().Get("action"),
		ResourceType: r.URL.Query().Get("resource_type"),
		Limit:        50,
		Offset:       0,
	}

	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 200 {
			filter.Limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			filter.Offset = v
		}
	}
	if s := r.URL.Query().Get("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			filter.Since = t
		} else if t, err := time.Parse("2006-01-02", s); err == nil {
			filter.Since = t
		}
	}

	entries, total, err := h.store.ListAuditEntries(r.Context(), uc.TenantID, filter)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve audit log.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
		"total":   total,
		"limit":   filter.Limit,
		"offset":  filter.Offset,
	})
}

func jsonError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}
