// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/themory"
)

// Plan 3 of dashboard-memory-section-roadmap.md (T3.3).
// Async memory export jobs — customer requests a JSON dump of their
// memories; a background worker writes the file; customer downloads.
//
// ADRs honored:
//   D1 (Plan 3 / EC-16) — id is server-generated `export-{uuid v4}`;
//        client `filename` body is IGNORED. Path traversal cannot reach
//        the filesystem because the on-disk path is built from the id only.
//   D5 (Plan 0) — cross-tenant attempts surface as 404, never 403.
//   D7 (Plan 0 spirit) — no hard DELETE; status='failed' for terminal failures.

const exportIDPrefix = "export-"

// exportIDPattern matches the server-generated form: "export-" + UUID v4.
// UUIDs are 8-4-4-4-12 hex digits with hyphens. We accept any valid v4
// shape (the uuid library normalises case + hyphenation).
var exportIDPattern = regexp.MustCompile(`^export-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// MemoryExportsHandler is the receiver for /v1/memory/exports/* routes.
type MemoryExportsHandler struct {
	db          *sql.DB
	client      TheMemoryClient
	logger      *slog.Logger
	exportDir   string
	workerDelay time.Duration
}

// NewMemoryExportsHandler constructs the handler. workerDelay is the
// time the background worker sleeps before writing the file — production
// uses ~3s for a realistic "processing" feel; tests pass 0 to complete
// synchronously without races.
func NewMemoryExportsHandler(db *sql.DB, client TheMemoryClient, logger *slog.Logger, exportDir string, workerDelay time.Duration) *MemoryExportsHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryExportsHandler{
		db:          db,
		client:      client,
		logger:      logger,
		exportDir:   exportDir,
		workerDelay: workerDelay,
	}
}

// Create inserts a pending export row + spawns the background worker.
// POST /v1/memory/exports
//
// D1 invariant: the request body MAY include a `label` (stored as
// display_name); the body's `filename` field (if any) is read but
// silently discarded. The on-disk + URL filename always derive from
// the server-generated `id`.
func (h *MemoryExportsHandler) Create(w http.ResponseWriter, r *http.Request) {
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
		Label string `json:"label,omitempty"`
		// Note: `filename` field intentionally NOT in this struct.
		// Even if the client sends one, json.Unmarshal ignores
		// unknown fields by default — defense in depth (D1).
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req) // best-effort; missing body is OK
	}
	var displayName sql.NullString
	if t := strings.TrimSpace(req.Label); t != "" {
		displayName = sql.NullString{String: t, Valid: true}
	}

	id := exportIDPrefix + uuid.NewString()

	const q = `INSERT INTO memory_exports (id, subject, tenant_id, display_name, status)
		VALUES ($1, $2, $3, $4, $5)`
	if _, err := h.db.ExecContext(r.Context(), q,
		id, user.Subject, user.TenantID, displayName, "pending",
	); err != nil {
		h.logger.Warn("exports create: insert failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not create export. Try again.")
		return
	}

	// Spawn the worker. Per EC-21 spirit: detach from request ctx so the
	// request's cancellation does not cancel the worker mid-flight.
	go h.runWorker(id, user.Subject)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     id,
		"status": "pending",
	})
}

// Status returns the export row for the authenticated tenant.
// GET /v1/memory/exports/{id}/status
func (h *MemoryExportsHandler) Status(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	id := chi.URLParam(r, "id")
	if !exportIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Export id must be export-{uuid}.")
		return
	}

	const q = `SELECT id, status, error_message, file_path FROM memory_exports
		WHERE id = $1 AND subject = $2 AND tenant_id = $3`

	var (
		gotID        string
		status       string
		errorMessage sql.NullString
		filePath     sql.NullString
	)
	err := h.db.QueryRowContext(r.Context(), q, id, user.Subject, user.TenantID).
		Scan(&gotID, &status, &errorMessage, &filePath)
	if errors.Is(err, sql.ErrNoRows) {
		// D5: cross-tenant or non-existent collapse to 404.
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Export not found.")
		return
	}
	if err != nil {
		h.logger.Warn("exports status: query failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not load export status. Try again.")
		return
	}

	out := map[string]any{
		"id":     gotID,
		"status": status,
	}
	if errorMessage.Valid {
		out["error_message"] = errorMessage.String
	}
	if filePath.Valid && status == "succeeded" {
		if info, err := os.Stat(filePath.String); err == nil {
			out["file_size"] = info.Size()
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// Download streams the exported JSON file with attachment headers.
// GET /v1/memory/exports/{id}/download
func (h *MemoryExportsHandler) Download(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	id := chi.URLParam(r, "id")
	if !exportIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "Export id must be export-{uuid}.")
		return
	}

	const q = `SELECT status, file_path FROM memory_exports
		WHERE id = $1 AND subject = $2 AND tenant_id = $3`
	var (
		status   string
		filePath sql.NullString
	)
	err := h.db.QueryRowContext(r.Context(), q, id, user.Subject, user.TenantID).
		Scan(&status, &filePath)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Export not found.")
		return
	}
	if err != nil {
		h.logger.Warn("exports download: query failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not start download. Try again.")
		return
	}
	if status != "succeeded" || !filePath.Valid {
		writeError(w, http.StatusNotFound, "NOT_READY", "Export is not yet available.")
		return
	}

	f, err := os.Open(filePath.String)
	if err != nil {
		h.logger.Warn("exports download: open file failed", "error", err, "path", filePath.String)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not open export file. Try again.")
		return
	}
	defer func() { _ = f.Close() }()

	w.Header().Set("Content-Type", "application/json")
	// D1: the attachment filename uses the server-generated id, NOT any
	// client-supplied label. Path traversal here is impossible because
	// `id` matched exportIDPattern above.
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, id))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// runWorker materialises the export on disk. v0.1 is intentionally simple:
// fetch a single page of memories, write a JSON file, update the row.
// Production should add: pagination over the full set, error reporting,
// stale-job reaper. Documented Out of Scope in the plan's Risks table.
func (h *MemoryExportsHandler) runWorker(id, subject string) {
	if h.workerDelay > 0 {
		time.Sleep(h.workerDelay)
	}
	bg, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Move to 'running' so the dashboard can show progress.
	_, _ = h.db.ExecContext(bg, `UPDATE memory_exports SET status = 'running' WHERE id = $1`, id)

	memories, err := h.client.List(bg, subject, themory.ListOptions{Limit: 200})
	if err != nil {
		h.failExport(bg, id, fmt.Sprintf("memory list failed: %v", err))
		return
	}

	if err := os.MkdirAll(h.exportDir, 0o750); err != nil {
		h.failExport(bg, id, fmt.Sprintf("mkdir failed: %v", err))
		return
	}
	// D1: filename derives from the validated id ONLY — no client input,
	// no traversal possible.
	path := filepath.Join(h.exportDir, id+".json")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		h.failExport(bg, id, fmt.Sprintf("open failed: %v", err))
		return
	}
	defer func() { _ = f.Close() }()

	if err := json.NewEncoder(f).Encode(memories); err != nil {
		h.failExport(bg, id, fmt.Sprintf("encode failed: %v", err))
		return
	}

	if _, err := h.db.ExecContext(bg,
		`UPDATE memory_exports SET status = 'succeeded', file_path = $1, completed_at = NOW() WHERE id = $2`,
		path, id,
	); err != nil {
		h.logger.Warn("exports worker: mark succeeded failed", "error", err, "id", id)
	}
}

func (h *MemoryExportsHandler) failExport(ctx context.Context, id, message string) {
	h.logger.Warn("exports worker: failed", "id", id, "message", message)
	_, _ = h.db.ExecContext(ctx,
		`UPDATE memory_exports SET status = 'failed', error_message = $1, completed_at = NOW() WHERE id = $2`,
		message, id,
	)
}
