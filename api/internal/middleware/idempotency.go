// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/loopzero/loopzero/api/internal/metrics"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// IdempotencyRecord holds a cached idempotent response.
type IdempotencyRecord struct {
	Key            string
	Endpoint       string
	TenantID       string
	RequestHash    string
	ResponseStatus int
	ResponseBody   []byte
}

// IdempotencyStore abstracts persistence for idempotency keys (DIP).
type IdempotencyStore interface {
	Get(ctx context.Context, key string, tenantID string) (*IdempotencyRecord, error)
	Store(ctx context.Context, rec *IdempotencyRecord) error
}

// responseCapture wraps http.ResponseWriter to capture the response for caching.
type responseCapture struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (rc *responseCapture) WriteHeader(code int) {
	rc.status = code
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	rc.body.Write(b)
	return rc.ResponseWriter.Write(b)
}

// Idempotency returns middleware that enforces idempotency-key semantics per spec §4.3.
//
// Behavior:
//   - No Idempotency-Key header → 400 MISSING_IDEMPOTENCY_KEY
//   - Key exists + same request_hash → returns cached response
//   - Key exists + different request_hash → 422
//   - Key new → processes request, stores result
func Idempotency(store IdempotencyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				writeIdempotencyError(w, http.StatusBadRequest, "MISSING_IDEMPOTENCY_KEY",
					"Idempotency-Key header is required for this operation.")
				return
			}

			// Limit body read to 10MB for idempotency hashing.
			// Source uploads (large payloads) should not use idempotency keys,
			// so this limit only affects API mutation endpoints (builds, projects).
			const maxIdempotencyBodySize = 10 << 20 // 10 MB
			body, err := io.ReadAll(io.LimitReader(r.Body, maxIdempotencyBodySize))
			if err != nil {
				writeIdempotencyError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			hash := hashRequestBody(body)

			// Extract tenant for scoped lookup
			var tenantID string
			if uc := GetUserContext(r.Context()); uc != nil {
				tenantID = uc.TenantID
			}

			// Check for existing key (scoped by tenant to prevent cross-tenant replay)
			ctx, span := otel.Tracer("loopzero-api").Start(r.Context(), "loopzero.api.idempotency.check",
				trace.WithAttributes(
					attribute.String("loopzero.idempotency.key", key),
				))

			existing, err := store.Get(ctx, key, tenantID)
			if err != nil {
				span.SetAttributes(attribute.Bool("loopzero.idempotency.hit", false))
				span.End()
				writeIdempotencyError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to check idempotency key")
				return
			}

			if existing != nil {
				if existing.RequestHash != hash {
					span.SetAttributes(attribute.Bool("loopzero.idempotency.hit", false))
					span.End()
					writeIdempotencyError(w, http.StatusUnprocessableEntity, "IDEMPOTENCY_MISMATCH",
						"Idempotency key already used with a different request payload")
					return
				}
				// Same key, same payload → return cached response
				span.SetAttributes(attribute.Bool("loopzero.idempotency.hit", true))
				span.End()
				metrics.IdempotencyHits.WithLabelValues(r.URL.Path).Inc()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(existing.ResponseStatus)
				// G705 false positive: existing.ResponseBody is the API's
				// OWN previous response replayed verbatim (idempotency
				// cache hit). Never user-input-derived.
				_, _ = w.Write(existing.ResponseBody) //nolint:gosec // G705 false positive: cached API response replay
				return
			}

			span.SetAttributes(attribute.Bool("loopzero.idempotency.hit", false))
			span.End()
			metrics.IdempotencyMisses.WithLabelValues(r.URL.Path).Inc()
			// New key → process request and capture response
			capture := &responseCapture{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(capture, r)

			// Store result for future replays
			rec := &IdempotencyRecord{
				Key:            key,
				Endpoint:       r.URL.Path,
				RequestHash:    hash,
				ResponseStatus: capture.status,
				ResponseBody:   capture.body.Bytes(),
			}

			// Extract tenant from context if available
			if uc := GetUserContext(r.Context()); uc != nil {
				rec.TenantID = uc.TenantID
			}

			// Best-effort store — don't fail the request if storage fails
			if storeErr := store.Store(r.Context(), rec); storeErr != nil {
				slog.Error("idempotency store failed — retry may reprocess", "key", key, "error", storeErr)
			}
		})
	}
}

func hashRequestBody(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

func writeIdempotencyError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
}
