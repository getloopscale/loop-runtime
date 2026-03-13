package unit

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/loopzero/loopzero/api/internal/routes"
)

type fakeHealthChecker struct {
	err error
}

func (f *fakeHealthChecker) Ping() error {
	return f.err
}

func TestHealthHandler(t *testing.T) {
	t.Run("GET /api/v1/health returns 200 with status ok", func(t *testing.T) {
		handler := routes.NewHealthHandler(nil)
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
		handler := routes.NewHealthHandler(nil)
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
		handler := routes.NewHealthHandler(checker)
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
		handler := routes.NewHealthHandler(checker)
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
}
