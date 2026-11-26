// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Split out of the flat `middleware` package's middleware_test.go (B-059): a
// test file lives in the package of its subject, and that file tested six
// middlewares that now live in five packages.

package policy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Idempotency middleware tests ---

type inMemoryIdempotencyStore struct {
	records map[string]*IdempotencyRecord
}

func newInMemoryIdempotencyStore() *inMemoryIdempotencyStore {
	return &inMemoryIdempotencyStore{records: make(map[string]*IdempotencyRecord)}
}

func (s *inMemoryIdempotencyStore) Get(_ context.Context, key string, tenantID string) (*IdempotencyRecord, error) {
	rec, ok := s.records[tenantID+":"+key]
	if !ok {
		return nil, nil
	}
	return rec, nil
}

func (s *inMemoryIdempotencyStore) Store(_ context.Context, rec *IdempotencyRecord) error {
	s.records[rec.TenantID+":"+rec.Key] = rec
	return nil
}

func TestIdempotency_MissingKey_Returns400(t *testing.T) {
	// Arrange
	store := newInMemoryIdempotencyStore()
	handler := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	// Act
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}

	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "MISSING_IDEMPOTENCY_KEY" {
		t.Errorf("expected MISSING_IDEMPOTENCY_KEY code, got %q", body["code"])
	}
}

func TestIdempotency_NewKey_ProcessesRequest(t *testing.T) {
	// Arrange
	store := newInMemoryIdempotencyStore()
	called := false
	handler := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"build_id":"bld-1"}`))
	}))

	// Act
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(`{"project":"my-app"}`))
	req.Header.Set("Idempotency-Key", "key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Assert
	if !called {
		t.Error("handler should have been called for new key")
	}
	if rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rec.Code)
	}
}

func TestIdempotency_SameKeySamePayload_ReturnsCached(t *testing.T) {
	// Arrange
	store := newInMemoryIdempotencyStore()
	callCount := 0
	handler := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"build_id":"bld-1"}`))
	}))

	payload := `{"project":"my-app"}`

	// First call
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(payload))
	req1.Header.Set("Idempotency-Key", "key-2")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second call with same key and payload
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(payload))
	req2.Header.Set("Idempotency-Key", "key-2")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	// Assert
	if callCount != 1 {
		t.Errorf("handler should have been called once, got %d", callCount)
	}
	if rec2.Code != http.StatusAccepted {
		t.Errorf("cached replay should return 202, got %d", rec2.Code)
	}
}

func TestIdempotency_SameKeyDifferentPayload_Returns422(t *testing.T) {
	// Arrange
	store := newInMemoryIdempotencyStore()
	handler := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"build_id":"bld-1"}`))
	}))

	// First call
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(`{"project":"app-1"}`))
	req1.Header.Set("Idempotency-Key", "key-3")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second call: same key, different payload
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/builds", strings.NewReader(`{"project":"app-2"}`))
	req2.Header.Set("Idempotency-Key", "key-3")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	// Assert
	if rec2.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rec2.Code)
	}

	var body map[string]string
	_ = json.Unmarshal(rec2.Body.Bytes(), &body)
	if body["code"] != "IDEMPOTENCY_MISMATCH" {
		t.Errorf("expected IDEMPOTENCY_MISMATCH code, got %q", body["code"])
	}
}
