// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

// API Key Rotation plan defaults.
const (
	defaultGraceWindowHours = 24
	minGraceWindowHours     = 1
	maxGraceWindowHours     = 168 // 7 days
)

// suppress unused import — io/ioutil is reserved for future doc snippet generators.
var _ = ioutil.Discard

// Plan 0 of dashboard-memory-section-roadmap.md (T0.3-T0.5).
// Platform unified API keys with scopes — single table, single key shape
// (loopzero_live_{base64url(32)}), gated by scope on read.
//
// ADRs locked by the plan (v1.2):
//   D1 — single unified loopzero_live_* key with multi-scope declaration
//   D2 — scope enum locked v0.1; reserved scopes recognised but rejected 503
//   D3 — secret format loopzero_live_{base64url(32)} (Stripe prefix discipline)
//   D4 — SHA-256 single-pass hashing (NOT bcrypt — 256-bit entropy already)
//   D5 — tenant scoping via Ory (subject, tenant_id) discriminator
//   D6 — expires_at optional at v0.1 (NULL = never)
//   D7 — revocation via soft-delete (revoked_at); hard DELETE forbidden
//   D8 — route → middleware dispatch: /v1/api-keys/* uses RequireJWT (session)
//
// Edge cases absorbed (v1.2 review):
//   EC-1 (list) — LIMIT 100 prevents runaway response
//   EC-2 (delete) — validate ID format BEFORE DB query (apk-{16hex} per pkg/id
//        convention; the plan's "ULID" wording is the older formulation, the
//        codebase-canonical format is the existing prefix-hex pattern from
//        pkg/id/id.go enforced by gate PKG-G06 — the security invariant
//        "validate-format-before-query" is preserved)
//   EC-3 (create) — reject expires_at in the past
//   EC-4 (create) — reject empty/over-long name BEFORE INSERT
//   EC-5 (create) — explicit rand.Read error check, never partial secrets

const (
	// SecretPrefix is the live-mode key prefix per D3. Stripe-style discipline:
	// a customer reading their secret-leak alerts immediately recognises
	// `loopzero_live_*` as a live credential.
	SecretPrefix = "loopzero_live_"

	// SecretRandomBytes is the entropy budget for the secret per D3.
	// 32 bytes = 256 bits — unguessable at any reasonable adversary cost.
	SecretRandomBytes = 32

	// RowIDPrefix is the project-canonical ID prefix per pkg/id (ADR-0028 +
	// gate PKG-G06 "Deterministic Naming And IDs"). Same shape as build-
	// / rel- / prev- / raa- — keeps the codebase coherent.
	RowIDPrefix = "apk-"

	// listLimit caps the list-handler response to prevent runaway payload
	// if a customer accumulates many keys (EC-1 absorption).
	listLimit = 100

	// keyNameMaxLength mirrors the DB CHECK constraint at migration 000024.
	keyNameMaxLength = 64
)

// validScopes is the v0.1 ACTIVE enum (D2). Lookup via map gives O(1)
// validation against typos like "momory:read".
var validScopes = map[string]bool{
	"memory:read":   true,
	"memory:write":  true,
	"memory:delete": true,
	"memory:admin":  true,
}

// reservedScopes are RESERVED for future verticals per D2. Requesting one
// gets a 503 scope_not_yet_active — honest "not implemented yet" instead
// of cryptic 403 "scope_required".
var reservedScopes = map[string]bool{
	"rag:read":      true,
	"rag:write":     true,
	"traces:read":   true,
	"agents:read":   true,
	"agents:invoke": true,
	"deploys:read":  true,
	"deploys:write": true,
}

// rowIDPattern matches the project-canonical row id `apk-{16hex}`.
var rowIDPattern = regexp.MustCompile(`^apk-[0-9a-f]{16}$`)

// IsValidAPIKeyID reports whether s matches the canonical apk-{16hex} format.
// Exported so other packages (e.g., audit log search) can validate without
// importing chi or the routes package.
func IsValidAPIKeyID(s string) bool { return rowIDPattern.MatchString(s) }

// APIKeysHandler owns CRUD for the unified loopzero_live_* API key surface.
// All endpoints require a PaaS session JWT (registered with RequireJWT
// in cloud/cmd/main.go per D8 dispatch). The Bearer-keyed `RequireScope`
// middleware lives in cloud/internal/auth/scope.go and is the consumer
// side (memory/* + future verticals) — NOT a CRUD authenticator on this
// resource (D8 forbids that loop).
type APIKeysHandler struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewAPIKeysHandler creates the handler. logger may be nil — when nil,
// slog.Default is used (matches the convention in webhook_db_test.go).
func NewAPIKeysHandler(db *sql.DB, logger *slog.Logger) *APIKeysHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &APIKeysHandler{db: db, logger: logger}
}

// APIKeyView is the wire shape returned by ListKeys + on CreateKey response.
// Secret is empty on list — only the CreateKey response includes the raw
// secret (Plan invariant: show-once).
type APIKeyView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"` // masked display: loopzero_live_Ab
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// ListKeys returns the authenticated user's active API keys, capped at 100.
// GET /v1/api-keys/list
func (h *APIKeysHandler) ListKeys(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	// EC-1: LIMIT 100 cap. Even if a customer accumulates many keys, the
	// response stays bounded. Combined with the D7 soft-delete filter, the
	// query rides the partial index idx_loopzero_api_keys_tenant.
	const q = `SELECT id, key_name, key_prefix, scopes, created_at, last_used_at, expires_at
		FROM loopzero_api_keys
		WHERE subject = $1 AND tenant_id = $2 AND revoked_at IS NULL
		ORDER BY created_at DESC
		LIMIT 100`

	rows, err := h.db.QueryContext(r.Context(), q, user.Subject, user.TenantID)
	if err != nil {
		h.logger.Warn("list api keys: query failed", "error", err, "subject", user.Subject)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not list API keys. Try again.")
		return
	}
	defer func() { _ = rows.Close() }()

	// Always serialise as `[]` not `null` — UI consumers iterate the slice.
	keys := make([]APIKeyView, 0, 16)
	for rows.Next() {
		var (
			v         APIKeyView
			scopesRaw []byte
			lastUsed  sql.NullTime
			expires   sql.NullTime
		)
		if err := rows.Scan(&v.ID, &v.Name, &v.KeyPrefix, &scopesRaw, &v.CreatedAt, &lastUsed, &expires); err != nil {
			h.logger.Warn("list api keys: row scan failed", "error", err)
			continue
		}
		if err := json.Unmarshal(scopesRaw, &v.Scopes); err != nil {
			// EC-7 spirit: jsonb corruption is non-fatal HERE (list endpoint;
			// degraded view is acceptable so customers can still see the row
			// and revoke it). Middleware path is stricter — see scope.go.
			h.logger.Warn("list api keys: scope unmarshal failed", "id", v.ID, "error", err)
			v.Scopes = []string{}
		}
		if lastUsed.Valid {
			t := lastUsed.Time
			v.LastUsedAt = &t
		}
		if expires.Valid {
			t := expires.Time
			v.ExpiresAt = &t
		}
		keys = append(keys, v)
	}
	if err := rows.Err(); err != nil {
		h.logger.Warn("list api keys: rows error", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not list API keys. Try again.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// RevokeKey soft-deletes the key (D7). Cross-tenant attempts return 404
// (D5 existence-leak prevention).
// DELETE /v1/api-keys/{id}
func (h *APIKeysHandler) RevokeKey(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	keyID := chi.URLParam(r, "id")

	// EC-2: validate the ID format BEFORE touching the DB. Malformed IDs
	// (path-traversal attempts, SQL meta-chars, random strings) get a
	// 400 INVALID_ID and the DB query plan + slow-log + audit log stays
	// clean. The security invariant — format-check before query — is what
	// matters; the format used is the codebase-canonical apk-{16hex}.
	if !IsValidAPIKeyID(keyID) {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "API key id must match apk-{16hex}.")
		return
	}

	const q = `UPDATE loopzero_api_keys
		SET revoked_at = NOW()
		WHERE id = $1 AND subject = $2 AND tenant_id = $3 AND revoked_at IS NULL`

	res, err := h.db.ExecContext(r.Context(), q, keyID, user.Subject, user.TenantID)
	if err != nil {
		h.logger.Warn("revoke api key: update failed", "error", err, "id", keyID)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not revoke API key. Try again.")
		return
	}
	affected, err := res.RowsAffected()
	if err != nil {
		h.logger.Warn("revoke api key: rows affected failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not revoke API key. Try again.")
		return
	}
	if affected == 0 {
		// Either: (a) row truly doesn't exist, (b) row belongs to a different
		// tenant, (c) row already revoked. ALL three return 404 — the cross-
		// tenant branch is the security-load-bearing one (D5 invariant: never
		// distinguish "exists but not yours" from "doesn't exist").
		writeError(w, http.StatusNotFound, "NOT_FOUND", "API key not found.")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// CreateKey generates a new loopzero_live_* secret + persists hash + scopes.
// The raw secret is returned ONCE in the response body and never again.
// POST /v1/api-keys
func (h *APIKeysHandler) CreateKey(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	// Reject obscenely large bodies before json.Decode allocates them — a
	// hostile caller could try to OOM the gateway with a multi-MB payload.
	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Could not read request body.")
		return
	}

	var req struct {
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
		// NOTE: subject / tenant_id deliberately NOT decoded — these come
		// from auth.GetUser, never the request body (D5 invariant).
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body.")
		return
	}

	// EC-4: validate name BEFORE INSERT (CHECK constraint would surface as
	// 500). Trim whitespace so " " is rejected the same as "".
	trimmed := strings.TrimSpace(req.Name)
	if trimmed == "" {
		writeError(w, http.StatusBadRequest, "EMPTY_NAME", "API key name is required.")
		return
	}
	if len(req.Name) > keyNameMaxLength {
		writeError(w, http.StatusBadRequest, "NAME_TOO_LONG",
			fmt.Sprintf("API key name must be %d characters or fewer.", keyNameMaxLength))
		return
	}

	// EC-3: expires_at in the past = born-expired key = anti-pattern. Reject
	// upfront with a clear error code, not a silent 200-then-immediate-fail.
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now()) {
		writeError(w, http.StatusBadRequest, "EXPIRES_IN_PAST",
			"expires_at must be in the future.")
		return
	}

	if len(req.Scopes) == 0 {
		writeError(w, http.StatusBadRequest, "EMPTY_SCOPES", "At least one scope is required.")
		return
	}
	// Walk scopes in input order so the FIRST offending scope drives the
	// error message — predictable for customers debugging their request.
	for _, s := range req.Scopes {
		if reservedScopes[s] {
			writeError(w, http.StatusServiceUnavailable, "scope_not_yet_active",
				fmt.Sprintf("Scope %q is reserved for a future product surface.", s))
			return
		}
		if !validScopes[s] {
			writeError(w, http.StatusBadRequest, "UNKNOWN_SCOPE",
				fmt.Sprintf("Scope %q is not a known scope.", s))
			return
		}
	}

	// D3 secret generation. EC-5: rand.Read MUST be checked — Go's contract
	// is "returns full N bytes or non-nil error", but checking explicitly
	// hardens against accidental loss of the contract in future refactors.
	raw := make([]byte, SecretRandomBytes)
	if _, err := rand.Read(raw); err != nil {
		h.logger.Error("create api key: crypto/rand failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not create API key. Try again.")
		return
	}
	secret := SecretPrefix + base64.RawURLEncoding.EncodeToString(raw)

	// D4 hash + first-12 prefix for masked display.
	sum := sha256.Sum256([]byte(secret))
	keyHash := hex.EncodeToString(sum[:])
	keyPrefix := secret[:12]

	id := generateAPIKeyRowID()
	scopesJSON, err := json.Marshal(req.Scopes)
	if err != nil {
		h.logger.Error("create api key: scopes marshal failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not create API key. Try again.")
		return
	}

	const q = `INSERT INTO loopzero_api_keys
		(id, subject, tenant_id, key_name, key_hash, key_prefix, scopes, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	if _, err := h.db.ExecContext(r.Context(), q,
		id, user.Subject, user.TenantID, trimmed, keyHash, keyPrefix, scopesJSON, req.ExpiresAt,
	); err != nil {
		// Unique-violation on key_hash is astronomically unlikely (collision
		// in 256-bit space) but is the one branch we differentiate so we
		// don't lie about it being a generic error.
		if isUniqueViolation(err) {
			h.logger.Error("create api key: key_hash collision (astronomically unlikely)", "id", id)
		}
		h.logger.Warn("create api key: insert failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not create API key. Try again.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         id,
		"name":       trimmed,
		"key_prefix": keyPrefix,
		"scopes":     req.Scopes,
		"secret":     secret, // raw — shown ONCE; never persisted plain
	})
}

// RotateKey rotates an existing key per API Key Rotation plan v1.0.
// POST /v1/api-keys/{id}/rotate
//
// Closes Plan 0 ADR D6 deferral.
//
// Body (optional): {"grace_window_hours": <int 1-168>} — default 24.
//
// Atomic single-tx:
//   1. SELECT FOR UPDATE old row (must exist, belong to authenticated
//      subject+tenant, not revoked, not already-expired). 404 otherwise.
//   2. INSERT new row with rotated_from = old.id, same scopes, fresh
//      `loopzero_live_*` secret + apk-{16hex} id, name suffixed with rotation date.
//   3. UPDATE old row's expires_at = NOW() + grace_window.
//   4. COMMIT.
//   5. Return 200 with new id + secret (shown ONCE per Plan 0 D3).
//
// D5: after grace, the existing RequireScope middleware naturally 401s
// the old key — no new dispatcher needed.
func (h *APIKeysHandler) RotateKey(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	oldID := chi.URLParam(r, "id")
	if !IsValidAPIKeyID(oldID) {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "API key id must match apk-{16hex}.")
		return
	}

	// Optional body — body absence is permitted (default 24h grace).
	// Use *int so we can distinguish "absent" from "explicitly 0" — Go's
	// zero-value semantic would silently swallow an explicit 0 with omitempty.
	bodyBytes, _ := io.ReadAll(io.LimitReader(r.Body, 1024))
	var req struct {
		GraceWindowHours *int `json:"grace_window_hours,omitempty"`
	}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid JSON body.")
			return
		}
	}
	graceHours := defaultGraceWindowHours
	if req.GraceWindowHours != nil {
		graceHours = *req.GraceWindowHours
	}
	if graceHours < minGraceWindowHours || graceHours > maxGraceWindowHours {
		writeError(w, http.StatusBadRequest, "INVALID_GRACE_WINDOW",
			fmt.Sprintf("grace_window_hours must be between %d and %d.", minGraceWindowHours, maxGraceWindowHours))
		return
	}
	graceWindow := time.Duration(graceHours) * time.Hour

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		h.logger.Warn("rotate api key: begin tx failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not rotate API key. Try again.")
		return
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()

	// SELECT FOR UPDATE the old row, scoped to authenticated tenant.
	const sel = `SELECT key_name, scopes, revoked_at, expires_at
		FROM loopzero_api_keys
		WHERE id = $1 AND subject = $2 AND tenant_id = $3
		FOR UPDATE`
	var (
		oldName    string
		scopesRaw  []byte
		revokedAt  sql.NullTime
		expiresAt  sql.NullTime
	)
	if err := tx.QueryRowContext(r.Context(), sel, oldID, user.Subject, user.TenantID).
		Scan(&oldName, &scopesRaw, &revokedAt, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// D5 — cross-tenant + nonexistent collapse to 404.
			writeError(w, http.StatusNotFound, "NOT_FOUND", "API key not found.")
			return
		}
		h.logger.Warn("rotate api key: select failed", "error", err, "id", oldID)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not rotate API key. Try again.")
		return
	}
	// Revoked + already-expired collapse to 404 (refuses to extend an
	// already-terminal key).
	if revokedAt.Valid {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "API key not found.")
		return
	}
	if expiresAt.Valid && expiresAt.Time.Before(time.Now()) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "API key not found.")
		return
	}

	// Generate new secret per Plan 0 D3.
	raw := make([]byte, SecretRandomBytes)
	if _, err := rand.Read(raw); err != nil {
		h.logger.Error("rotate api key: crypto/rand failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not rotate API key. Try again.")
		return
	}
	newSecret := SecretPrefix + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(newSecret))
	newKeyHash := hex.EncodeToString(sum[:])
	newKeyPrefix := newSecret[:12]
	newID := generateAPIKeyRowID()
	newName := oldName + " (rotated " + time.Now().UTC().Format("2006-01-02") + ")"
	// Length guard mirrors Plan 0 EC-4 — names > 64 chars would surface
	// as a CHECK violation. Truncate the rotated-name silently rather than
	// reject the whole flow.
	if len(newName) > keyNameMaxLength {
		newName = newName[:keyNameMaxLength]
	}

	const ins = `INSERT INTO loopzero_api_keys
		(id, subject, tenant_id, key_name, key_hash, key_prefix, scopes, expires_at, rotated_from)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	if _, err := tx.ExecContext(r.Context(), ins,
		newID, user.Subject, user.TenantID, newName, newKeyHash, newKeyPrefix,
		scopesRaw, nil, oldID,
	); err != nil {
		h.logger.Warn("rotate api key: insert failed", "error", err, "old_id", oldID)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not rotate API key. Try again.")
		return
	}

	// UPDATE the old key's expires_at to NOW() + grace_window.
	newExpiry := time.Now().Add(graceWindow)
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE loopzero_api_keys SET expires_at = $1 WHERE id = $2`,
		newExpiry, oldID,
	); err != nil {
		h.logger.Warn("rotate api key: update old expires_at failed", "error", err, "id", oldID)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not rotate API key. Try again.")
		return
	}

	if err := tx.Commit(); err != nil {
		h.logger.Warn("rotate api key: commit failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not rotate API key. Try again.")
		return
	}
	rollback = false

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         newID,
		"name":       newName,
		"key_prefix": newKeyPrefix,
		"secret":     newSecret,
		"old_id":     oldID,
		"old_expires_at": newExpiry.UTC().Format(time.RFC3339),
		"grace_window_hours": graceHours,
	})
}

// LineageKey returns the full rotation chain for the given key per API
// Key Lineage View plan v1.0 (closes API Key Rotation OOS #4).
// GET /v1/api-keys/{id}/lineage
//
// Walk strategy (D2): recursive CTE that traverses both directions
//
//   ancestors:   walk rotated_from backward from {id}
//   descendants: walk rows whose rotated_from points to any ancestor
//
// All filtered by the authenticated (subject, tenant_id) — defense in
// depth on top of the FK guarantee that rotations never cross tenants.
// Cross-tenant id → empty result → 404 (D3 + P0 D5 invariant).
//
// Response per D1: {"lineage": [Item, ...]} ordered ROOT FIRST → CURRENT LAST.
// Each Item carries {id, name, key_prefix, created_at, revoked_at,
// expires_at, is_current}.
//
// is_current per row: revoked_at IS NULL AND (expires_at IS NULL OR
// expires_at > NOW()).
func (h *APIKeysHandler) LineageKey(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	keyID := chi.URLParam(r, "id")
	if !IsValidAPIKeyID(keyID) {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "API key id must match apk-{16hex}.")
		return
	}

	// Recursive CTE walks both directions. Anchor = the requested key;
	// recursive arms walk ancestors (via rotated_from) and descendants
	// (via reverse FK). LIMIT 100 per D2 defensive cap.
	const q = `
		WITH RECURSIVE
		ancestors AS (
		    SELECT id, key_name, key_prefix, created_at, revoked_at, expires_at, rotated_from
		    FROM loopzero_api_keys
		    WHERE id = $1 AND subject = $2 AND tenant_id = $3
		    UNION
		    SELECT k.id, k.key_name, k.key_prefix, k.created_at, k.revoked_at, k.expires_at, k.rotated_from
		    FROM loopzero_api_keys k
		    JOIN ancestors a ON a.rotated_from = k.id
		    WHERE k.subject = $2 AND k.tenant_id = $3
		),
		descendants AS (
		    SELECT id, key_name, key_prefix, created_at, revoked_at, expires_at, rotated_from
		    FROM loopzero_api_keys
		    WHERE id = $1 AND subject = $2 AND tenant_id = $3
		    UNION
		    SELECT k.id, k.key_name, k.key_prefix, k.created_at, k.revoked_at, k.expires_at, k.rotated_from
		    FROM loopzero_api_keys k
		    JOIN descendants d ON k.rotated_from = d.id
		    WHERE k.subject = $2 AND k.tenant_id = $3
		),
		full_chain AS (
		    SELECT id, key_name, key_prefix, created_at, revoked_at, expires_at FROM ancestors
		    UNION
		    SELECT id, key_name, key_prefix, created_at, revoked_at, expires_at FROM descendants
		)
		SELECT id, key_name, key_prefix, created_at, revoked_at, expires_at
		FROM full_chain
		ORDER BY created_at ASC
		LIMIT 100
	`
	rows, err := h.db.QueryContext(r.Context(), q, keyID, user.Subject, user.TenantID)
	if err != nil {
		h.logger.Warn("lineage api key: query failed", "error", err, "id", keyID)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not load lineage. Try again.")
		return
	}
	defer func() { _ = rows.Close() }()

	type lineageItem struct {
		ID        string     `json:"id"`
		Name      string     `json:"name"`
		KeyPrefix string     `json:"key_prefix"`
		CreatedAt time.Time  `json:"created_at"`
		RevokedAt *time.Time `json:"revoked_at,omitempty"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
		IsCurrent bool       `json:"is_current"`
	}

	out := make([]lineageItem, 0, 8)
	now := time.Now()
	for rows.Next() {
		var (
			item    lineageItem
			revoked sql.NullTime
			expires sql.NullTime
		)
		if err := rows.Scan(&item.ID, &item.Name, &item.KeyPrefix, &item.CreatedAt, &revoked, &expires); err != nil {
			h.logger.Warn("lineage api key: scan failed", "error", err)
			continue
		}
		if revoked.Valid {
			t := revoked.Time
			item.RevokedAt = &t
		}
		if expires.Valid {
			t := expires.Time
			item.ExpiresAt = &t
		}
		// D1 is_current: not revoked AND (no expiry OR expiry future).
		item.IsCurrent = !revoked.Valid && (!expires.Valid || expires.Time.After(now))
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		h.logger.Warn("lineage api key: rows error", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "Could not load lineage. Try again.")
		return
	}

	// D3 — empty result means the requested id doesn't belong to this
	// (subject, tenant_id) tuple. Surface as 404 to preserve the P0 D5
	// existence-leak invariant.
	if len(out) == 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "API key not found.")
		return
	}

	// Multiple is_current=true items would indicate a data invariant
	// violation (two active keys in a single chain). The dashboard expects
	// exactly one; if the data violates this we still return the rows but
	// log for ops to investigate.
	currents := 0
	for _, it := range out {
		if it.IsCurrent {
			currents++
		}
	}
	if currents > 1 {
		h.logger.Warn("lineage api key: data invariant violation — multiple active in chain",
			"id", keyID, "active_count", currents)
	}

	writeJSON(w, http.StatusOK, map[string]any{"lineage": out})
}

// generateAPIKeyRowID returns an `apk-{16hex}` identifier per pkg/id
// convention. Inlined here rather than added to pkg/id to keep Plan 0
// scope minimal (pkg/ requires governance; the cloud package is loosely
// gated and self-sufficient on this kind of helper).
func generateAPIKeyRowID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Mirrors pkg/id behavior: crypto/rand failure is unrecoverable.
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return RowIDPrefix + hex.EncodeToString(b)
}

// isUniqueViolation reports whether the error chain contains a Postgres
// 23505 unique-violation. Uses string match against the pgx error text
// to avoid a hard dependency on the pgx error-code constants from this
// shallow check site.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return strings.Contains(err.Error(), "duplicate key value")
}
