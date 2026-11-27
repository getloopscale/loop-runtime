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

// --- Correlation ID middleware tests ---

func TestCorrelationID_GeneratesWhenMissing(t *testing.T) {
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("Correlation-ID")
		if id == "" {
			t.Error("Correlation-ID should be generated and set on request")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Correlation-ID") == "" {
		t.Error("Correlation-ID should be set on response")
	}
}

func TestCorrelationID_PreservesExisting(t *testing.T) {
	handler := CorrelationID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Correlation-ID") != "my-corr-id" {
			t.Errorf("expected preserved Correlation-ID, got %q", r.Header.Get("Correlation-ID"))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Correlation-ID", "my-corr-id")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Correlation-ID") != "my-corr-id" {
		t.Errorf("response Correlation-ID = %q, want my-corr-id", rec.Header().Get("Correlation-ID"))
	}
}
