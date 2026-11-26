// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package healthhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/health/healthhttp"
)

type fakeHealthChecker struct {
	err error
}

func (f *fakeHealthChecker) Ping() error {
	return f.err
}

func TestHealthHandler(t *testing.T) {
	t.Run("GET /api/v1/health returns 200 with status ok", func(t *testing.T) {
		handler := healthhttp.NewHealthHandler(nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()

		handler.Health(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode response body: %v", err)
		}
		if body["status"] != "ok" {
			t.Fatalf("expected status=ok, got %q", body["status"])
		}
	})

	t.Run("GET /api/v1/health returns content-type application/json", func(t *testing.T) {
		handler := healthhttp.NewHealthHandler(nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()

		handler.Health(rec, req)

		ct := rec.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Fatalf("expected Content-Type application/json, got %q", ct)
		}
	})
}

func TestDeepHealthHandler(t *testing.T) {
	t.Run("returns 200 when DB is healthy", func(t *testing.T) {
		checker := &fakeHealthChecker{err: nil}
		handler := healthhttp.NewHealthHandler(checker)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
		rec := httptest.NewRecorder()

		handler.DeepHealth(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["status"] != "ok" {
			t.Fatalf("expected status=ok, got %q", body["status"])
		}
		if body["database"] != "connected" {
			t.Fatalf("expected database=connected, got %q", body["database"])
		}
	})

	t.Run("returns 503 when DB is down", func(t *testing.T) {
		checker := &fakeHealthChecker{err: errors.New("connection refused")}
		handler := healthhttp.NewHealthHandler(checker)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
		rec := httptest.NewRecorder()

		handler.DeepHealth(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected status 503, got %d", rec.Code)
		}

		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["status"] != "degraded" {
			t.Fatalf("expected status=degraded, got %q", body["status"])
		}
		if body["database"] != "disconnected" {
			t.Fatalf("expected database=disconnected, got %q", body["database"])
		}
	})

	t.Run("returns 200 when DB and all services healthy", func(t *testing.T) {
		checker := &fakeHealthChecker{err: nil}
		handler := healthhttp.NewHealthHandler(checker,
			&fakeServiceChecker{name: "argocd", err: nil},
			&fakeServiceChecker{name: "gitea", err: nil},
		)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
		rec := httptest.NewRecorder()

		handler.DeepHealth(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["argocd"] != "connected" {
			t.Errorf("expected argocd=connected, got %q", body["argocd"])
		}
		if body["gitea"] != "connected" {
			t.Errorf("expected gitea=connected, got %q", body["gitea"])
		}
	})

	t.Run("returns 503 when any service is unreachable", func(t *testing.T) {
		checker := &fakeHealthChecker{err: nil} // DB ok
		handler := healthhttp.NewHealthHandler(checker,
			&fakeServiceChecker{name: "argocd", err: errors.New("connection refused")},
			&fakeServiceChecker{name: "gitea", err: nil},
		)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
		rec := httptest.NewRecorder()

		handler.DeepHealth(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected status 503, got %d", rec.Code)
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["status"] != "degraded" {
			t.Errorf("expected status=degraded, got %q", body["status"])
		}
		if body["argocd"] != "unreachable" {
			t.Errorf("expected argocd=unreachable, got %q", body["argocd"])
		}
		if body["database"] != "connected" {
			t.Errorf("expected database=connected (DB ok), got %q", body["database"])
		}
	})

	t.Run("returns auth_failed for 401 errors", func(t *testing.T) {
		checker := &fakeHealthChecker{err: nil}
		handler := healthhttp.NewHealthHandler(checker,
			&fakeServiceChecker{name: "argocd", err: fmt.Errorf("argocd: %w", healthhttp.ErrServiceAuthFailed)},
		)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health/deep", nil)
		rec := httptest.NewRecorder()

		handler.DeepHealth(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rec.Code)
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["argocd"] != "auth_failed" {
			t.Errorf("expected argocd=auth_failed, got %q", body["argocd"])
		}
	})
}

// fakeServiceChecker implements health.ServiceHealthChecker for testing.
type fakeServiceChecker struct {
	name string
	err  error
}

func (f *fakeServiceChecker) Name() string                  { return f.name }
func (f *fakeServiceChecker) Check(_ context.Context) error { return f.err }
