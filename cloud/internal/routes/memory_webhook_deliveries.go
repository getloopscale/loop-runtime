// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

// Memory Webhook Delivery plan, T5.6 + T5.7.
//
// Two responsibilities:
//
//   1. Publish — INSERT a memory_webhook_deliveries row for each ACTIVE
//      webhook of the authenticated user that subscribed to the firing
//      event. Called by memory handlers post-mutation (T5.6).
//
//   2. List — GET /v1/memory/webhooks/{id}/deliveries returns the last 10
//      deliveries for that webhook for the dashboard history view (T5.7).

const deliveryIDPrefix = "mwd-"
const deliveryHistoryLimit = 10

// MemoryWebhookEventPublisher is the production WebhookEventPublisher.
// It writes rows directly into memory_webhook_deliveries; the worker
// goroutine in cloud/internal/memory_webhook_worker picks them up.
type MemoryWebhookEventPublisher struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewMemoryWebhookEventPublisher constructs the publisher.
func NewMemoryWebhookEventPublisher(db *sql.DB, logger *slog.Logger) *MemoryWebhookEventPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryWebhookEventPublisher{db: db, logger: logger}
}

// Publish writes one delivery row per matching active webhook. Failures
// are logged but NOT propagated — Plan 4 D1 (post-Dashboard Memory chain
// pattern) says cross-cut emit must NOT mask the primary mutation's
// success/failure status.
func (p *MemoryWebhookEventPublisher) Publish(ctx context.Context, subject, tenantID, eventType string, payload map[string]any) {
	payloadJSON, err := json.Marshal(map[string]any{
		"event_type": eventType,
		"data":       payload,
	})
	if err != nil {
		p.logger.Warn("memory webhook publish: payload marshal failed", "error", err, "event", eventType)
		return
	}

	// Find webhooks that (a) belong to this subject + tenant, (b) are
	// not revoked, and (c) include the firing eventType in their
	// events array. The `?` containment operator on jsonb is exactly
	// the semantic we want.
	const sel = `
		SELECT id FROM memory_webhooks
		WHERE subject = $1 AND tenant_id = $2
		  AND revoked_at IS NULL
		  AND events ? $3
	`
	rows, err := p.db.QueryContext(ctx, sel, subject, tenantID, eventType)
	if err != nil {
		p.logger.Warn("memory webhook publish: query failed", "error", err, "event", eventType)
		return
	}
	defer func() { _ = rows.Close() }()

	var webhookIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		webhookIDs = append(webhookIDs, id)
	}
	_ = rows.Close()

	if len(webhookIDs) == 0 {
		return
	}

	for _, wid := range webhookIDs {
		deliveryID := deliveryIDPrefix + randomHex16ForDelivery()
		_, err := p.db.ExecContext(ctx, `
			INSERT INTO memory_webhook_deliveries
			  (id, webhook_id, event_type, payload, status, next_attempt_at)
			VALUES ($1, $2, $3, $4, 'pending', NOW())
		`, deliveryID, wid, eventType, payloadJSON)
		if err != nil {
			p.logger.Warn("memory webhook publish: insert failed", "error", err, "webhook_id", wid)
			continue
		}
	}
}

// randomHex16ForDelivery generates 16 hex chars from 8 random bytes.
// Mirrors `randomHex16` in memory_webhooks.go without exporting it from
// that file or importing it (would create import cycles in tests).
func randomHex16ForDelivery() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// =============================================================================
// GET /v1/memory/webhooks/{id}/deliveries — T5.7 dashboard history view
// =============================================================================

// DeliveryView is the wire shape for one row in the deliveries history.
type DeliveryView struct {
	ID                 string  `json:"id"`
	EventType          string  `json:"event_type"`
	Status             string  `json:"status"`
	AttemptCount       int     `json:"attempt_count"`
	LastResponseStatus *int    `json:"last_response_status,omitempty"`
	LastErrorMessage   *string `json:"last_error_message,omitempty"`
	CreatedAt          string  `json:"created_at"`
	CompletedAt        *string `json:"completed_at,omitempty"`
}

// MemoryWebhookDeliveriesHandler is the receiver for the deliveries
// history sub-route.
type MemoryWebhookDeliveriesHandler struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewMemoryWebhookDeliveriesHandler constructs the handler.
func NewMemoryWebhookDeliveriesHandler(db *sql.DB, logger *slog.Logger) *MemoryWebhookDeliveriesHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryWebhookDeliveriesHandler{db: db, logger: logger}
}

// List returns the most-recent deliveryHistoryLimit deliveries for the
// given webhook, scoped to the authenticated user's (subject, tenant_id)
// — cross-tenant attempts yield empty result (silent) NOT 403.
// GET /v1/memory/webhooks/{id}/deliveries
func (h *MemoryWebhookDeliveriesHandler) List(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	webhookID := chi.URLParam(r, "id")
	if !deliveryWebhookIDPattern.MatchString(webhookID) {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Webhook id must match mwh-{16hex}.")
		return
	}

	const q = `
		SELECT d.id, d.event_type, d.status, d.attempt_count,
		       d.last_response_status, d.last_error_message,
		       d.created_at, d.completed_at
		FROM memory_webhook_deliveries d
		JOIN memory_webhooks w ON w.id = d.webhook_id
		WHERE d.webhook_id = $1
		  AND w.subject = $2
		  AND w.tenant_id = $3
		ORDER BY d.created_at DESC
		LIMIT $4
	`
	rows, err := h.db.QueryContext(r.Context(), q, webhookID, user.Subject, user.TenantID, deliveryHistoryLimit)
	if err != nil {
		h.logger.Warn("memory webhook deliveries list: query failed", "error", err, "webhook_id", webhookID)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not load deliveries. Try again.")
		return
	}
	defer func() { _ = rows.Close() }()

	out := make([]DeliveryView, 0, deliveryHistoryLimit)
	for rows.Next() {
		var (
			v          DeliveryView
			lastStatus sql.NullInt64
			lastErr    sql.NullString
			completed  sql.NullString
		)
		if err := rows.Scan(&v.ID, &v.EventType, &v.Status, &v.AttemptCount,
			&lastStatus, &lastErr, &v.CreatedAt, &completed); err != nil {
			h.logger.Warn("memory webhook deliveries list: scan failed", "error", err)
			continue
		}
		if lastStatus.Valid {
			s := int(lastStatus.Int64)
			v.LastResponseStatus = &s
		}
		if lastErr.Valid {
			// Truncate the error message for display — keep audit-grade detail
			// out of the dashboard column where it doesn't fit.
			msg := lastErr.String
			if len(msg) > 200 {
				msg = msg[:200] + "…"
			}
			// Strip control chars that could mess up the JSON wire.
			msg = strings.Map(func(r rune) rune {
				if r < 32 && r != '\n' && r != '\t' {
					return ' '
				}
				return r
			}, msg)
			v.LastErrorMessage = &msg
		}
		if completed.Valid {
			v.CompletedAt = &completed.String
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": out})
}

// deliveryWebhookIDPattern is local to this file; mirrors webhookIDPattern
// from memory_webhooks.go (avoid the cross-file dependency).
var deliveryWebhookIDPattern = regexp.MustCompile(`^mwh-[0-9a-f]{16}$`)
var deliveryIDPattern = regexp.MustCompile(`^mwd-[0-9a-f]{16}$`)

// =============================================================================
// POST /v1/memory/webhooks/{webhook_id}/deliveries/{delivery_id}/replay
// Delivery Replay plan — T8.2
// =============================================================================
//
// Status guard (ADR D3):
//
//	delivered           → 400 ALREADY_DELIVERED (intentional: prevent
//	                      re-delivering an event the subscriber already saw)
//	pending | delivering → 200 no-op (idempotent — the worker is already
//	                      going to dispatch this row)
//	failed | dead_letter → 200 reset (status='pending', attempt_count=0,
//	                      next_attempt_at=NOW(), clear last_response_status
//	                      and last_error_message)
//
// D4 — cross-tenant (and "delivery exists but belongs to ANOTHER webhook
// path-param") both fall through to 404, NOT 403. Existence-leak invariant
// (CLAUDE.md GO-TO CTO mandate, invariant #2).
func (h *MemoryWebhookDeliveriesHandler) Replay(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	webhookID := chi.URLParam(r, "webhook_id")
	if !deliveryWebhookIDPattern.MatchString(webhookID) {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Webhook id must match mwh-{16hex}.")
		return
	}
	deliveryID := chi.URLParam(r, "delivery_id")
	if !deliveryIDPattern.MatchString(deliveryID) {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Delivery id must match mwd-{16hex}.")
		return
	}

	// Tenant-scoped lookup: JOIN to memory_webhooks so a delivery whose
	// owning webhook belongs to a different tenant returns sql.ErrNoRows
	// — which we map to 404, NOT 403 (P0 D5).
	const sel = `
		SELECT d.status, d.webhook_id FROM memory_webhook_deliveries d
		JOIN memory_webhooks w ON w.id = d.webhook_id
		WHERE d.id = $1 AND w.subject = $2 AND w.tenant_id = $3
	`
	var (
		status     string
		ownerWHID  string
	)
	row := h.db.QueryRowContext(r.Context(), sel, deliveryID, user.Subject, user.TenantID)
	if err := row.Scan(&status, &ownerWHID); err != nil {
		// sql.ErrNoRows is the cross-tenant + nonexistent + soft-delete
		// path. We log nothing distinguishing because telling the caller
		// the row exists in another tenant IS the existence leak.
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Delivery not found.")
		return
	}

	// D4 — path-param webhook_id must match the delivery's actual webhook.
	// Belt + suspenders against URL fishing.
	if ownerWHID != webhookID {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Delivery not found.")
		return
	}

	switch status {
	case "delivered":
		writeError(w, http.StatusBadRequest, "ALREADY_DELIVERED",
			"This event was already delivered. Replaying would send the subscriber a duplicate.")
		return
	case "pending", "delivering":
		// Idempotent no-op — the worker has this row queued. Returning the
		// current status lets the dashboard render the right pill without
		// a follow-up GET.
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     deliveryID,
			"status": status,
			"noop":   true,
		})
		return
	case "failed", "dead_letter":
		// fall through to reset
	default:
		// Unknown status — fail loud rather than silently re-arming.
		h.logger.Warn("memory webhook replay: unknown status", "status", status, "delivery_id", deliveryID)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Delivery in unknown state. Contact support.")
		return
	}

	// D2 — full reset re-arms the worker's retry budget from scratch.
	const upd = `
		UPDATE memory_webhook_deliveries SET status = 'pending',
		    attempt_count = 0,
		    next_attempt_at = NOW(),
		    last_response_status = NULL,
		    last_error_message = NULL
		WHERE id = $1
	`
	if _, err := h.db.ExecContext(r.Context(), upd, deliveryID); err != nil {
		h.logger.Warn("memory webhook replay: update failed", "error", err, "delivery_id", deliveryID)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not replay delivery. Try again.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     deliveryID,
		"status": "pending",
		"noop":   false,
	})
}
