// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Split out of the flat `middleware` package's middleware_test.go (B-059): a
// test file lives in the package of its subject, and that file tested six
// middlewares that now live in five packages.

package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Request Logging middleware tests ---

func TestRequestLogging_NilLogger_NoPanic(t *testing.T) {
	handler := RequestLogging(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req) // should not panic

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
