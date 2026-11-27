// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/getloopscale/loop-runtime/api/internal/ports"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// IdempotencyRecord holds a cached idempotent response. The type is owned by
// ports so the store that persists it does not import this middleware.
type IdempotencyRecord = ports.IdempotencyRecord

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

// maxIdempotencyBodySize caps the body an idempotent request may carry (it is
// read whole to be hashed).
const maxIdempotencyBodySize = 10 << 20 // 10 MB

// Idempotency returns middleware that enforces idempotency-key semantics per spec §4.3.
//
// Behavior:
//   - No Idempotency-Key header → 400 MISSING_IDEMPOTENCY_KEY
//   - Body larger than maxIdempotencyBodySize → 413 PAYLOAD_TOO_LARGE
//   - Key exists + same request_hash + same endpoint → returns cached response
//   - Key exists + different request_hash or endpoint → 422
//   - Key new → processes request, stores the result unless it is a 5xx
func Idempotency(store IdempotencyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				writeIdempotencyError(w, http.StatusBadRequest, "MISSING_IDEMPOTENCY_KEY",
					"Idempotency-Key header is required for this operation.")
				return
			}

			// The body is hashed, so it is read whole — capped at
			// maxIdempotencyBodySize. A larger body is refused (413): cutting
			// it at the cap would forward a truncated request to the handler.
			// Source uploads (large payloads) do not use idempotency keys.
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxIdempotencyBodySize))
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					writeIdempotencyError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE",
						"Request body exceeds the size an idempotent request may have.")
					return
				}
				writeIdempotencyError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			hash := hashRequestBody(body)

			// Extract tenant for scoped lookup
			var tenantID string
			if uc := auth.GetUserContext(r.Context()); uc != nil {
				tenantID = uc.TenantID
			}

			// Check for existing key (scoped by tenant to prevent cross-tenant replay)
			ctx, span := otel.Tracer("loop-runtime-api").Start(r.Context(), "loop-runtime.api.idempotency.check",
				trace.WithAttributes(
					attribute.String("loop-runtime.idempotency.key", key),
				))

			existing, err := store.Get(ctx, key, tenantID)
			if err != nil {
				span.SetAttributes(attribute.Bool("loop-runtime.idempotency.hit", false))
				span.End()
				writeIdempotencyError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to check idempotency key")
				return
			}

			if existing != nil {
				// A key belongs to the endpoint it was first used on; reused on
				// another one it must not replay that endpoint's answer.
				if existing.RequestHash != hash || existing.Endpoint != r.URL.Path {
					span.SetAttributes(attribute.Bool("loop-runtime.idempotency.hit", false))
					span.End()
					writeIdempotencyError(w, http.StatusUnprocessableEntity, "IDEMPOTENCY_MISMATCH",
						"Idempotency key already used with a different request (payload or endpoint)")
					return
				}
				// Same key, same payload → return cached response
				span.SetAttributes(attribute.Bool("loop-runtime.idempotency.hit", true))
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

			span.SetAttributes(attribute.Bool("loop-runtime.idempotency.hit", false))
			span.End()
			metrics.IdempotencyMisses.WithLabelValues(r.URL.Path).Inc()
			// New key → process request and capture response
			capture := &responseCapture{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(capture, r)

			// A server error is not the answer to this request: caching it would
			// replay the failure to the retry the client is told to make.
			if capture.status >= http.StatusInternalServerError {
				return
			}

			// Store result for future replays
			rec := &IdempotencyRecord{
				Key:            key,
				Endpoint:       r.URL.Path,
				RequestHash:    hash,
				ResponseStatus: capture.status,
				ResponseBody:   capture.body.Bytes(),
			}

			// Extract tenant from context if available
			if uc := auth.GetUserContext(r.Context()); uc != nil {
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
