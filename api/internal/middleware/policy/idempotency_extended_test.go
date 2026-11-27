// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Split out of the flat `middleware` package's auth_extended_test.go (B-059).

package policy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Idempotency extended tests ---

func TestIdempotency_StoreGetError_Returns500(t *testing.T) {
	store := &failingIdempotencyStore{getErr: errors.New("db timeout")}
	handler := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	req.Header.Set("Idempotency-Key", "key-fail")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

type failingIdempotencyStore struct {
	getErr error
}

func (f *failingIdempotencyStore) Get(_ context.Context, _ string, _ string) (*IdempotencyRecord, error) {
	return nil, f.getErr
}
func (f *failingIdempotencyStore) Store(_ context.Context, _ *IdempotencyRecord) error {
	return nil
}
