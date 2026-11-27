// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package audithttp serves the audit-log endpoints under /api/v1/audit: the
// tenant-scoped read side of what the platform recorded about itself.
//
// Ninth family to leave the flat `routes` package (B-080, slice-32), extracted
// together with `health` because both are single-file families with no coupling —
// two trivial moves in one diff stay reviewable, and the one-family-per-slice
// discipline exists for reviewability rather than for its own sake.
//
// Its extraction surfaced `jsonError`, a response writer that lived here and was
// reached by two files of the parent. It moved to the `httpio` leaf as
// WriteCodeErrorJSON — deliberately NOT merged into the existing WriteErrorJSON,
// which keys its machine-readable field `error` where this one keys it `code`.
// Merging them would have been a breaking change to the `--json` contract wearing
// the costume of a deduplication.
package audithttp

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/audit"
	"github.com/getloopscale/loop-runtime/api/internal/httpio"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	AuditStore       = audit.AuditStore
	AuditFilter      = ports.AuditFilter
	AuditEntryResult = ports.AuditEntryResult
)

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
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteCodeErrorJSON(w, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated.")
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
		httpio.WriteCodeErrorJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve audit log.")
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
