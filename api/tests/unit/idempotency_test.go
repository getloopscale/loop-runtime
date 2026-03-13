package unit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/loopzero/loopzero/api/internal/middleware"
)

// fakeIdempotencyStore implements middleware.IdempotencyStore for tests.
type fakeIdempotencyStore struct {
	keys map[string]*middleware.IdempotencyRecord
}

func newFakeIdempotencyStore() *fakeIdempotencyStore {
	return &fakeIdempotencyStore{keys: make(map[string]*middleware.IdempotencyRecord)}
}

func (f *fakeIdempotencyStore) Get(ctx context.Context, key string) (*middleware.IdempotencyRecord, error) {
	rec, ok := f.keys[key]
	if !ok {
		return nil, nil
	}
	return rec, nil
}

func (f *fakeIdempotencyStore) Store(ctx context.Context, rec *middleware.IdempotencyRecord) error {
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
		json.NewEncoder(w).Encode(map[string]string{"id": "proj-12345678"})
	})
}

func TestIdempotencyMiddleware(t *testing.T) {
	t.Run("request without Idempotency-Key passes through", func(t *testing.T) {
		store := newFakeIdempotencyStore()
		mw := middleware.Idempotency(store)
		handler := mw(dummyHandler())

		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(`{"name":"test"}`))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Should pass through — idempotency is optional per spec (only required by CLI)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("new key → processes request and stores result", func(t *testing.T) {
		store := newFakeIdempotencyStore()
		mw := middleware.Idempotency(store)
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
		record, _ := store.Get(context.Background(), "550e8400-e29b-41d4-a716-446655440000")
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
		store.keys["key-123"] = &middleware.IdempotencyRecord{
			Key:            "key-123",
			RequestHash:    hashBody(body),
			ResponseStatus: http.StatusCreated,
			ResponseBody:   []byte(`{"id":"proj-cached"}`),
		}

		callCount := 0
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusCreated)
		})

		mw := middleware.Idempotency(store)
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
		json.NewDecoder(rec.Body).Decode(&result)
		if result["id"] != "proj-cached" {
			t.Fatalf("expected cached id=proj-cached, got %q", result["id"])
		}
	})

	t.Run("same key different payload → 422", func(t *testing.T) {
		store := newFakeIdempotencyStore()

		// Pre-populate with different payload
		store.keys["key-456"] = &middleware.IdempotencyRecord{
			Key:            "key-456",
			RequestHash:    hashBody(`{"name":"original"}`),
			ResponseStatus: http.StatusCreated,
			ResponseBody:   []byte(`{"id":"proj-original"}`),
		}

		mw := middleware.Idempotency(store)
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
		json.NewDecoder(rec.Body).Decode(&body)
		if body["code"] != "IDEMPOTENCY_MISMATCH" {
			t.Fatalf("expected code=IDEMPOTENCY_MISMATCH, got %q", body["code"])
		}
	})
}
