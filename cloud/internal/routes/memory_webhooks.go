// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

// Plan 3 of dashboard-memory-section-roadmap.md (T3.4).
// Customer-facing memory webhook registrations. Webhook DELIVERY is NOT
// implemented at v0.1 — the dashboard discloses this via a persistent
// <Alert intent="warning" title="Demo only"> per Plan 3 ADR D2 (EC-14).
//
// All routes registered in main.go under RequireScope("memory:admin") —
// webhooks are a privileged operation (they affect downstream consumers
// of the customer's data).

const (
	webhookIDPrefix    = "mwh-"
	webhookLabelMax    = 64
	webhookListLimit   = 100
)

var webhookIDPattern = regexp.MustCompile(`^mwh-[0-9a-f]{16}$`)
var webhookURLPattern = regexp.MustCompile(`(?i)^https?://`)

// MemoryWebhooksHandler is the receiver for /v1/memory/webhooks/* routes.
type MemoryWebhooksHandler struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewMemoryWebhooksHandler creates the handler. logger may be nil.
func NewMemoryWebhooksHandler(db *sql.DB, logger *slog.Logger) *MemoryWebhooksHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryWebhooksHandler{db: db, logger: logger}
}

// MemoryWebhookView is the wire shape for List + Create response.
type MemoryWebhookView struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`
	Label     string   `json:"label"`
	Events    []string `json:"events"`
	CreatedAt string   `json:"created_at"`
}

// Create persists a new webhook registration.
// POST /v1/memory/webhooks
func (h *MemoryWebhooksHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Could not read request body.")
		return
	}

	var req struct {
		URL    string   `json:"url"`
		Label  string   `json:"label"`
		Events []string `json:"events"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body.")
		return
	}

	if !webhookURLPattern.MatchString(req.URL) {
		writeError(w, http.StatusBadRequest, "INVALID_URL", "Webhook URL must start with http:// or https://.")
		return
	}
	trimmed := strings.TrimSpace(req.Label)
	if trimmed == "" {
		writeError(w, http.StatusBadRequest, "EMPTY_LABEL", "Webhook label is required.")
		return
	}
	if len(req.Label) > webhookLabelMax {
		writeError(w, http.StatusBadRequest, "LABEL_TOO_LONG",
			fmt.Sprintf("Webhook label must be %d characters or fewer.", webhookLabelMax))
		return
	}
	if len(req.Events) == 0 {
		writeError(w, http.StatusBadRequest, "EMPTY_EVENTS", "At least one event is required.")
		return
	}

	id := webhookIDPrefix + randomHex16()
	eventsJSON, err := json.Marshal(req.Events)
	if err != nil {
		h.logger.Error("webhooks create: events marshal failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not create webhook. Try again.")
		return
	}

	const q = `INSERT INTO memory_webhooks (id, subject, tenant_id, url, label, events)
		VALUES ($1, $2, $3, $4, $5, $6)`
	if _, err := h.db.ExecContext(r.Context(), q,
		id, user.Subject, user.TenantID, req.URL, trimmed, eventsJSON,
	); err != nil {
		h.logger.Warn("webhooks create: insert failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not create webhook. Try again.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     id,
		"url":    req.URL,
		"label":  trimmed,
		"events": req.Events,
	})
}

// List returns the authenticated user's active webhooks, capped at 100.
// GET /v1/memory/webhooks/list
func (h *MemoryWebhooksHandler) List(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	const q = `SELECT id, url, label, events, created_at
		FROM memory_webhooks
		WHERE subject = $1 AND tenant_id = $2 AND revoked_at IS NULL
		ORDER BY created_at DESC
		LIMIT 100`

	rows, err := h.db.QueryContext(r.Context(), q, user.Subject, user.TenantID)
	if err != nil {
		h.logger.Warn("webhooks list: query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not list webhooks. Try again.")
		return
	}
	defer func() { _ = rows.Close() }()

	out := make([]MemoryWebhookView, 0, 16)
	for rows.Next() {
		var (
			v         MemoryWebhookView
			eventsRaw []byte
		)
		if err := rows.Scan(&v.ID, &v.URL, &v.Label, &eventsRaw, &v.CreatedAt); err != nil {
			h.logger.Warn("webhooks list: scan failed", "error", err)
			continue
		}
		if err := json.Unmarshal(eventsRaw, &v.Events); err != nil {
			h.logger.Warn("webhooks list: events unmarshal failed", "error", err, "id", v.ID)
			v.Events = []string{}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"webhooks": out})
}

// Delete soft-deletes the webhook. Cross-tenant attempts → 404 (D5).
// DELETE /v1/memory/webhooks/{id}
func (h *MemoryWebhooksHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	id := chi.URLParam(r, "id")
	if !webhookIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Webhook id must match mwh-{16hex}.")
		return
	}

	const q = `UPDATE memory_webhooks
		SET revoked_at = NOW()
		WHERE id = $1 AND subject = $2 AND tenant_id = $3 AND revoked_at IS NULL`

	res, err := h.db.ExecContext(r.Context(), q, id, user.Subject, user.TenantID)
	if err != nil {
		h.logger.Warn("webhooks delete: update failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not delete webhook. Try again.")
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		// D5 invariant: cross-tenant + non-existent + already-revoked all
		// collapse to 404. Never 403.
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Webhook not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// randomHex16 returns 16 hex chars from 8 random bytes. Mirrors the
// pkg/id convention used for build-/rel-/etc. ids without importing
// pkg/id (avoids a cross-package coupling that doesn't earn its weight).
func randomHex16() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}
