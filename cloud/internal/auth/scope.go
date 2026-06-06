// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// Plan 0 of dashboard-memory-section-roadmap.md (T0.6).
// RequireScope is the load-bearing Bearer-keyed middleware that fronts
// every /v1/memory/* request and (post-Plan 1) every future vertical
// route (rag/traces/agents/deploys when those plans ship).
//
// ADRs the middleware enforces:
//   D1 — single unified loopzero_live_* key carries multi-scope declaration
//   D2 — only scopes in the enum are recognised; bad input = 403
//   D3 — Bearer header carries loopzero_live_{base64url(32)} secret
//   D4 — server-side SHA-256 of Bearer → key_hash; O(1) lookup
//   D5 — populated UserContext (Subject, TenantID) propagates downstream
//   D7 — revoked_at IS NULL filter excludes soft-deleted keys
//   D8 — distinct middleware from RequireJWT; never both on same route
//
// Edge cases absorbed:
//   EC-6 — D8 dispatch lockdown registered at main.go (per-route choice)
//   EC-7 — jsonb scope corruption returns 500 INTERNAL, not silent 403
//   EC-21 — last_used_at fire-and-forget; lost-on-crash accepted

const (
	bearerPrefix    = "Bearer "
	secretPrefixLP  = "loopzero_live_"
	tracerInstName  = "loopzero.auth"
	spanName        = "auth.scope_check"
	asyncWriteBudgt = 5 * time.Second
)

// RequireScope returns chi middleware that authenticates with a loopzero_live_*
// Bearer and authorises with the named scope. Failures map to typed HTTP
// codes per plan T0.6 acceptance criteria:
//
//   missing/garbled Bearer       → 401 Unauthorized
//   hash not in DB / expired      → 401 Unauthorized (no distinction — D5 invariant)
//   missing required scope        → 403 Forbidden + {error.code: scope_required, missing:[scope]}
//   jsonb scopes column corrupt   → 500 INTERNAL (NOT silent 403; EC-7)
//   query error                    → 500 INTERNAL
//   success                        → ctx populated; next.ServeHTTP runs
//
// The async last_used_at update fires after a successful auth check and
// MUST NOT block the response path — observability data is best-effort
// (Out of Scope EC-21).
func RequireScope(db *sql.DB, required string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := otel.Tracer(tracerInstName).Start(r.Context(), spanName)
			defer span.End()
			span.SetAttributes(attribute.String("scope.required", required))

			secret, ok := extractLoopZeroLiveBearer(r)
			if !ok {
				span.SetAttributes(attribute.String("scope.reject_reason", "bad_bearer_shape"))
				writeAuthError(w, "Invalid or missing API key. Provide Authorization: Bearer loopzero_live_*")
				return
			}

			sum := sha256.Sum256([]byte(secret))
			keyHash := hex.EncodeToString(sum[:])

			const q = `SELECT id, subject, tenant_id, scopes
				FROM loopzero_api_keys
				WHERE key_hash = $1
				  AND revoked_at IS NULL
				  AND (expires_at IS NULL OR expires_at > NOW())`

			var (
				keyID, subject, tenantID string
				scopesRaw                []byte
			)
			err := db.QueryRowContext(ctx, q, keyHash).Scan(&keyID, &subject, &tenantID, &scopesRaw)
			if errors.Is(err, sql.ErrNoRows) {
				// Three possibilities collapse into one response — by design:
				//   (a) key never existed
				//   (b) key revoked
				//   (c) key expired
				// Distinguishing them would let an attacker probe the keyspace.
				span.SetAttributes(attribute.String("scope.reject_reason", "key_not_found_or_inactive"))
				writeAuthError(w, "Invalid or expired API key.")
				return
			}
			if err != nil {
				slog.Warn("scope check: db query failed", "error", err, "path", r.URL.Path)
				span.SetAttributes(attribute.String("scope.reject_reason", "db_error"))
				writeScopeInternalError(w)
				return
			}

			// EC-7: jsonb scopes corruption must NOT silently degrade to
			// "missing scope" 403 — that would falsely tell the customer
			// their key was revoked or downgraded. Honest 500 + audit log
			// so support can root-cause the bad row.
			var scopes []string
			if err := json.Unmarshal(scopesRaw, &scopes); err != nil {
				slog.Error("scope check: scopes jsonb corruption",
					"key_id", keyID, "error", err, "raw_prefix", safePrefix(scopesRaw))
				span.SetAttributes(attribute.String("scope.reject_reason", "scopes_jsonb_corruption"))
				writeScopeInternalError(w)
				return
			}

			if !sliceContains(scopes, required) {
				span.SetAttributes(
					attribute.Bool("scope.granted", false),
					attribute.StringSlice("scope.held", scopes),
				)
				writeScopeRequired(w, required)
				return
			}
			span.SetAttributes(attribute.Bool("scope.granted", true))

			// EC-21: fire-and-forget last_used_at update. The middleware
			// MUST return immediately; a slow write in the goroutine is
			// invisible to the caller.
			// Context.Background() (not r.Context()) is intentional — the
			// request context is canceled when the response writes finish,
			// which would cancel the UPDATE mid-flight.
			go func(id string) {
				bgCtx, cancel := context.WithTimeout(context.Background(), asyncWriteBudgt)
				defer cancel()
				if _, err := db.ExecContext(bgCtx,
					"UPDATE loopzero_api_keys SET last_used_at = NOW() WHERE id = $1", id,
				); err != nil {
					slog.Debug("scope check: async last_used_at update failed", "error", err, "key_id", id)
				}
			}(keyID)

			user := &UserContext{Subject: subject, TenantID: tenantID}
			ctx = WithUser(ctx, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractLoopZeroLiveBearer parses the Authorization header. Returns (secret, true)
// only if the header is well-formed Bearer + the secret carries the
// loopzero_live_ prefix. Anything else is `("", false)`.
func extractLoopZeroLiveBearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, bearerPrefix) {
		return "", false
	}
	secret := strings.TrimSpace(h[len(bearerPrefix):])
	if !strings.HasPrefix(secret, secretPrefixLP) {
		return "", false
	}
	return secret, true
}

// writeScopeRequired emits the structured 403 body that the SDK depends on
// to render "you need scope X" guidance to the customer.
func writeScopeRequired(w http.ResponseWriter, required string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    "scope_required",
			"missing": []string{required},
			"message": "This API key does not carry the required scope.",
		},
	})
}

// writeScopeInternalError keeps the 500 body consistent with the rest of
// the platform (`code: INTERNAL`). The matching slog.Warn / slog.Error
// above is the only place that carries diagnostic detail.
func writeScopeInternalError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    "INTERNAL",
		"message": "Internal authentication error. Try again.",
	})
}

// safePrefix returns the first 16 chars of raw for audit logs. Bounded so
// a multi-MB corrupt jsonb blob doesn't bloat the log line.
func safePrefix(raw []byte) string {
	const max = 16
	if len(raw) <= max {
		return string(raw)
	}
	return string(raw[:max]) + "..."
}

func sliceContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
