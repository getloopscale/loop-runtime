package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
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
	Get(ctx context.Context, key string) (*IdempotencyRecord, error)
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
//   - No Idempotency-Key header → passes through (header is optional)
//   - Key exists + same request_hash → returns cached response
//   - Key exists + different request_hash → 422
//   - Key new → processes request, stores result
func Idempotency(store IdempotencyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Read and hash body
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeIdempotencyError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			hash := hashRequestBody(body)

			// Check for existing key
			existing, err := store.Get(r.Context(), key)
			if err != nil {
				writeIdempotencyError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to check idempotency key")
				return
			}

			if existing != nil {
				if existing.RequestHash != hash {
					writeIdempotencyError(w, http.StatusUnprocessableEntity, "IDEMPOTENCY_MISMATCH",
						"Idempotency key already used with a different request payload")
					return
				}
				// Same key, same payload → return cached response
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(existing.ResponseStatus)
				w.Write(existing.ResponseBody)
				return
			}

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
			store.Store(r.Context(), rec)
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
	json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
}
