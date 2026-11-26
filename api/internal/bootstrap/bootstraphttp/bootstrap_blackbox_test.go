// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package bootstraphttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/bootstrap/bootstraphttp"
	"github.com/getloopscale/loop-runtime/api/internal/bootstrap/bootstraphttp/bootstraptest"
)

// stubCellEngine implements routes.CellPlacementEngine for tests.
// CHG-2026-04-30-DELETE-SINGLE-CELL: replaces deleted placement.DefaultEngine.
// Always returns "cell-0" — same semantics, but production code has
// no single-cell engine to drift back to.
type stubCellEngine struct{}

func (stubCellEngine) AssignCell(_ context.Context) (string, error) {
	return "cell-0", nil
}

func (stubCellEngine) AssignAndExecute(ctx context.Context, fn func(context.Context, string) error) error {
	return fn(ctx, "cell-0")
}

func TestBootstrapRoute(t *testing.T) {
	t.Run("POST /internal/bootstrap → 200 creates records", func(t *testing.T) {
		store := bootstraptest.NewInMemoryStore()
		handler := bootstraphttp.NewBootstrapHandler(store, "", stubCellEngine{})

		body := map[string]string{
			"admin_github_id":     "123456",
			"admin_login":         "paulo",
			"tenant_slug":         "loopzero",
			"tenant_display_name": "loopzero",
			"plan":                "starter",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Bootstrap(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result bootstraphttp.BootstrapResponse
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if result.Status != "ok" {
			t.Errorf("expected status=ok, got %q", result.Status)
		}
		if result.UserID != "github-paulo" {
			t.Errorf("expected user_id=github-paulo, got %q", result.UserID)
		}
		if result.TenantID != "tn-loopzero" {
			t.Errorf("expected tenant_id=tn-loopzero, got %q", result.TenantID)
		}
		if result.Created != true {
			t.Errorf("expected created=true, got %v", result.Created)
		}
	})

	t.Run("POST /internal/bootstrap → 200 idempotent on second call", func(t *testing.T) {
		store := bootstraptest.NewInMemoryStore()
		handler := bootstraphttp.NewBootstrapHandler(store, "", stubCellEngine{})

		body := map[string]string{
			"admin_github_id":     "123456",
			"admin_login":         "paulo",
			"tenant_slug":         "loopzero",
			"tenant_display_name": "loopzero",
			"plan":                "starter",
		}
		b, _ := json.Marshal(body)

		// First call — creates records
		req1 := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader(b))
		req1.Header.Set("Content-Type", "application/json")
		rec1 := httptest.NewRecorder()
		handler.Bootstrap(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("first call: expected 200, got %d: %s", rec1.Code, rec1.Body.String())
		}

		// Second call — idempotent, no error
		req2 := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader(b))
		req2.Header.Set("Content-Type", "application/json")
		rec2 := httptest.NewRecorder()
		handler.Bootstrap(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("second call: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
		}

		var result bootstraphttp.BootstrapResponse
		if err := json.NewDecoder(rec2.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if result.Created != false {
			t.Errorf("second call should return created=false, got %v", result.Created)
		}
		if result.Status != "ok" {
			t.Errorf("expected status=ok, got %q", result.Status)
		}
	})

	t.Run("POST /internal/bootstrap → 400 missing admin_github_id", func(t *testing.T) {
		store := bootstraptest.NewInMemoryStore()
		handler := bootstraphttp.NewBootstrapHandler(store, "", stubCellEngine{})

		body := map[string]string{
			"admin_login":         "paulo",
			"tenant_slug":         "loopzero",
			"tenant_display_name": "loopzero",
			"plan":                "starter",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Bootstrap(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}

		// Stage C T2.3 migration: response is RFC 9457 problem+json which
		// includes int `status` field; decode as map[string]any.
		var errBody map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
			t.Fatalf("failed to decode error: %v", err)
		}
		if code, _ := errBody["code"].(string); code != "MISSING_FIELD" {
			t.Errorf("expected code=MISSING_FIELD, got %q", code)
		}
	})

	t.Run("POST /internal/bootstrap → 400 missing admin_login", func(t *testing.T) {
		store := bootstraptest.NewInMemoryStore()
		handler := bootstraphttp.NewBootstrapHandler(store, "", stubCellEngine{})

		body := map[string]string{
			"admin_github_id":     "123456",
			"tenant_slug":         "loopzero",
			"tenant_display_name": "loopzero",
			"plan":                "starter",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Bootstrap(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /internal/bootstrap → 400 missing tenant_slug", func(t *testing.T) {
		store := bootstraptest.NewInMemoryStore()
		handler := bootstraphttp.NewBootstrapHandler(store, "", stubCellEngine{})

		body := map[string]string{
			"admin_github_id":     "123456",
			"admin_login":         "paulo",
			"tenant_display_name": "loopzero",
			"plan":                "starter",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Bootstrap(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /internal/bootstrap → 400 invalid JSON body", func(t *testing.T) {
		store := bootstraptest.NewInMemoryStore()
		handler := bootstraphttp.NewBootstrapHandler(store, "", stubCellEngine{})

		req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Bootstrap(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /internal/bootstrap → tenant_id uses tn- prefix", func(t *testing.T) {
		store := bootstraptest.NewInMemoryStore()
		handler := bootstraphttp.NewBootstrapHandler(store, "", stubCellEngine{})

		body := map[string]string{
			"admin_github_id":     "999",
			"admin_login":         "admin",
			"tenant_slug":         "my-team",
			"tenant_display_name": "My Team",
			"plan":                "growth",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Bootstrap(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result bootstraphttp.BootstrapResponse
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if result.TenantID != "tn-my-team" {
			t.Errorf("expected tenant_id=tn-my-team, got %q", result.TenantID)
		}
		if result.UserID != "github-admin" {
			t.Errorf("expected user_id=github-admin, got %q", result.UserID)
		}
	})

	t.Run("POST /internal/bootstrap → defaults plan to starter when empty", func(t *testing.T) {
		store := bootstraptest.NewInMemoryStore()
		handler := bootstraphttp.NewBootstrapHandler(store, "", stubCellEngine{})

		body := map[string]string{
			"admin_github_id":     "123456",
			"admin_login":         "paulo",
			"tenant_slug":         "loopzero",
			"tenant_display_name": "loopzero",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Bootstrap(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// ADR-0008: Cell placement tests

	t.Run("POST /internal/bootstrap → assigns cell_id from PlacementEngine", func(t *testing.T) {
		store := bootstraptest.NewInMemoryStore()
		handler := bootstraphttp.NewBootstrapHandler(store, "", stubCellEngine{})

		body := map[string]string{
			"admin_github_id":     "123456",
			"admin_login":         "paulo",
			"tenant_slug":         "acme",
			"tenant_display_name": "Acme Corp",
			"plan":                "growth",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Bootstrap(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify cell_id was stored
		cellID := store.GetCellID("tn-acme")
		if cellID != "cell-0" {
			t.Errorf("expected cell_id=cell-0, got %q", cellID)
		}
	})

	t.Run("POST /internal/bootstrap → 500 when PlacementEngine fails", func(t *testing.T) {
		store := bootstraptest.NewInMemoryStore()
		handler := bootstraphttp.NewBootstrapHandler(store, "", &failingPlacementEngine{})

		body := map[string]string{
			"admin_github_id":     "123456",
			"admin_login":         "paulo",
			"tenant_slug":         "broken",
			"tenant_display_name": "Broken Corp",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/internal/bootstrap", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Bootstrap(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 when placement fails, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// failingPlacementEngine always returns an error.
type failingPlacementEngine struct{}

func (e *failingPlacementEngine) AssignCell(_ context.Context) (string, error) {
	return "", fmt.Errorf("cell placement unavailable: all cells full")
}

func (e *failingPlacementEngine) AssignAndExecute(_ context.Context, _ func(context.Context, string) error) error {
	return fmt.Errorf("cell placement unavailable: all cells full")
}
