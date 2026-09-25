// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package observability_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	observability "github.com/getloopscale/loopzero/api/internal/middleware/observability"
)

func TestCorrelationIDMiddleware(t *testing.T) {
	uuidv4Pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	t.Run("injects Correlation-ID when not present", func(t *testing.T) {
		var capturedID string
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedID = r.Header.Get("Correlation-ID")
			w.WriteHeader(http.StatusOK)
		})

		handler := observability.CorrelationID(inner)
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if capturedID == "" {
			t.Fatal("expected Correlation-ID to be injected")
		}
		if !uuidv4Pattern.MatchString(capturedID) {
			t.Fatalf("expected UUID v4 format, got %q", capturedID)
		}

		// Also check response header
		responseID := rec.Header().Get("Correlation-ID")
		if responseID != capturedID {
			t.Fatalf("expected response Correlation-ID to match request, got %q vs %q", responseID, capturedID)
		}
	})

	t.Run("preserves existing Correlation-ID from client", func(t *testing.T) {
		var capturedID string
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedID = r.Header.Get("Correlation-ID")
			w.WriteHeader(http.StatusOK)
		})

		handler := observability.CorrelationID(inner)
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Correlation-ID", "client-provided-id")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if capturedID != "client-provided-id" {
			t.Fatalf("expected client-provided-id, got %q", capturedID)
		}

		responseID := rec.Header().Get("Correlation-ID")
		if responseID != "client-provided-id" {
			t.Fatalf("expected response to echo client ID, got %q", responseID)
		}
	})
}

func TestRequestLoggingMiddleware(t *testing.T) {
	t.Run("passes through to inner handler", func(t *testing.T) {
		called := false
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})

		handler := observability.RequestLogging(nil)(inner)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if !called {
			t.Fatal("inner handler was not called")
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
	})
}
