// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package policy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	policy "github.com/getloopscale/loop-runtime/api/internal/middleware/policy"
)

// fakeIdempotencyStore implements policy.IdempotencyStore for tests.
type fakeIdempotencyStore struct {
	keys map[string]*policy.IdempotencyRecord
}

func newFakeIdempotencyStore() *fakeIdempotencyStore {
	return &fakeIdempotencyStore{keys: make(map[string]*policy.IdempotencyRecord)}
}

func (f *fakeIdempotencyStore) Get(ctx context.Context, key string, tenantID string) (*policy.IdempotencyRecord, error) {
	rec, ok := f.keys[key]
	if !ok {
		return nil, nil
	}
	if rec.TenantID != "" && tenantID != "" && rec.TenantID != tenantID {
		return nil, nil
	}
	return rec, nil
}

func (f *fakeIdempotencyStore) Store(ctx context.Context, rec *policy.IdempotencyRecord) error {
	f.keys[rec.Key] = rec
	return nil
}

func hashBody(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

// dummyHandler returns a simple 201 handler for testing.
func dummyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "proj-12345678"})
	})
}

func TestIdempotencyMiddleware(t *testing.T) {
	t.Run("request without Idempotency-Key returns 400", func(t *testing.T) {
		store := newFakeIdempotencyStore()
		mw := policy.Idempotency(store)
		handler := mw(dummyHandler())

		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(`{"name":"test"}`))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["code"] != "MISSING_IDEMPOTENCY_KEY" {
			t.Fatalf("expected code=MISSING_IDEMPOTENCY_KEY, got %q", body["code"])
		}
		if body["message"] != "Idempotency-Key header is required for this operation." {
			t.Fatalf("expected specific error message, got %q", body["message"])
		}
	})

	t.Run("new key → processes request and stores result", func(t *testing.T) {
		store := newFakeIdempotencyStore()
		mw := policy.Idempotency(store)
		handler := mw(dummyHandler())

		body := `{"name":"my-saas"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
		req.Header.Set("Idempotency-Key", "550e8400-e29b-41d4-a716-446655440000")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify stored
		record, _ := store.Get(context.Background(), "550e8400-e29b-41d4-a716-446655440000", "")
		if record == nil {
			t.Fatal("expected idempotency record to be stored")
		}
		if record.RequestHash != hashBody(body) {
			t.Fatalf("expected request hash %q, got %q", hashBody(body), record.RequestHash)
		}
	})

	t.Run("same key same payload → returns cached response", func(t *testing.T) {
		store := newFakeIdempotencyStore()
		body := `{"name":"my-saas"}`

		// Pre-populate cache
		store.keys["key-123"] = &policy.IdempotencyRecord{
			Key:            "key-123",
			Endpoint:       "/api/v1/projects",
			RequestHash:    hashBody(body),
			ResponseStatus: http.StatusCreated,
			ResponseBody:   []byte(`{"id":"proj-cached"}`),
		}

		callCount := 0
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusCreated)
		})

		mw := policy.Idempotency(store)
		handler := mw(inner)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
		req.Header.Set("Idempotency-Key", "key-123")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 (cached), got %d: %s", rec.Code, rec.Body.String())
		}

		if callCount != 0 {
			t.Fatalf("expected inner handler NOT to be called (cached), but was called %d times", callCount)
		}

		var result map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&result)
		if result["id"] != "proj-cached" {
			t.Fatalf("expected cached id=proj-cached, got %q", result["id"])
		}
	})

	t.Run("same key different payload → 422", func(t *testing.T) {
		store := newFakeIdempotencyStore()

		// Pre-populate with different payload
		store.keys["key-456"] = &policy.IdempotencyRecord{
			Key:            "key-456",
			RequestHash:    hashBody(`{"name":"original"}`),
			ResponseStatus: http.StatusCreated,
			ResponseBody:   []byte(`{"id":"proj-original"}`),
		}

		mw := policy.Idempotency(store)
		handler := mw(dummyHandler())

		differentBody := `{"name":"different"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(differentBody))
		req.Header.Set("Idempotency-Key", "key-456")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["code"] != "IDEMPOTENCY_MISMATCH" {
			t.Fatalf("expected code=IDEMPOTENCY_MISMATCH, got %q", body["code"])
		}
	})

	t.Run("body larger than 10MB is refused, not processed truncated", func(t *testing.T) {
		store := newFakeIdempotencyStore()
		called := false
		mw := policy.Idempotency(store)
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusCreated)
		}))

		const limit = 10 << 20 // 10 MB
		largeBody := bytes.Repeat([]byte{'A'}, limit+1)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader(largeBody))
		req.Header.Set("Idempotency-Key", "key-large-body")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413 for a body over 10MB, got %d: %s", rec.Code, rec.Body.String())
		}
		if called {
			t.Fatal("handler ran on a request the middleware refused")
		}
		if record, _ := store.Get(context.Background(), "key-large-body", ""); record != nil {
			t.Fatal("a refused request must not leave an idempotency record")
		}
	})
}
