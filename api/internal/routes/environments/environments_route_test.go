// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environments_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/middleware/auth/authtest"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/routes/environments"

	"github.com/go-chi/chi/v5"
)

// fakeEnvironmentManager implements routes.EnvironmentManager for testing.
type fakeEnvironmentManager struct {
	environments []ports.EnvironmentEntry
	detail       *ports.EnvironmentDetail
	listErr      error
	addErr       error
	removeErr    error
	getErr       error
}

func (f *fakeEnvironmentManager) ListEnvironments(_ context.Context, _ string) ([]ports.EnvironmentEntry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.environments, nil
}

// M2 v1.2 (2026-06-07): signatures carry `project`.
func (f *fakeEnvironmentManager) AddEnvironment(_ context.Context, _, _, _ string) error {
	return f.addErr
}

func (f *fakeEnvironmentManager) RemoveEnvironment(_ context.Context, _, _, _ string) error {
	return f.removeErr
}

func (f *fakeEnvironmentManager) GetEnvironmentDetail(_ context.Context, _, _, _ string) (*ports.EnvironmentDetail, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.detail, nil
}

func TestEnvironmentListRoute(t *testing.T) {
	t.Run("GET /api/v1/environments → 200 with environments list", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{
			environments: []ports.EnvironmentEntry{
				{Name: "production", Status: "active"},
				{Name: "staging", Status: "active"},
			},
		}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&result)
		envs, ok := result["environments"].([]interface{})
		if !ok {
			t.Fatal("expected 'environments' array in response")
		}
		if len(envs) != 2 {
			t.Fatalf("expected 2 environments, got %d", len(envs))
		}
	})

	t.Run("GET /api/v1/environments → 200 with empty list", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{
			environments: []ports.EnvironmentEntry{},
		}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&result)
		envs, ok := result["environments"].([]interface{})
		if !ok {
			t.Fatal("expected 'environments' array in response")
		}
		if len(envs) != 0 {
			t.Fatalf("expected 0 environments, got %d", len(envs))
		}
	})

	t.Run("GET /api/v1/environments → 401 without auth", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/environments → 500 on store error", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{
			listErr: errors.New("connection failed"),
		}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestEnvironmentCreateRoute(t *testing.T) {
	t.Run("POST /api/v1/environments → 201 with valid name", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{"name":"staging","project":"web"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&result)
		if result["name"] != "staging" {
			t.Fatalf("expected name=staging, got %v", result["name"])
		}
	})

	t.Run("POST /api/v1/environments → 400 with invalid name (uppercase)", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{"name":"My-Env"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/environments → 400 with leading hyphen", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{"name":"-staging"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/environments → 400 with empty name", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{"name":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/environments → 400 with reserved name default", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{"name":"default"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["code"] != "INVALID_NAME" {
			t.Fatalf("expected INVALID_NAME error code, got %v", errResp["code"])
		}
	})

	t.Run("POST /api/v1/environments → 400 with reserved name kube-system", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{"name":"kube-system"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/environments → 400 with reserved name loopzero-system", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{"name":"loopzero-system"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/environments → 400 with reserved name argocd", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{"name":"argocd"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/environments → 400 with reserved name monitoring", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{"name":"monitoring"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/environments → 401 without auth", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{"name":"staging"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/environments → 400 with invalid JSON", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{invalid`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/environments → 500 on manager error", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{
			addErr: errors.New("k8s unavailable"),
		}
		handler := environments.NewEnvironmentHandler(mgr)

		body := `{"name":"staging","project":"web"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestEnvironmentGetRoute(t *testing.T) {
	t.Run("GET /api/v1/environments/{name} → 200 with detail", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{
			detail: &ports.EnvironmentDetail{
				Name:   "production",
				Status: "active",
				Projects: []ports.EnvProjectInfo{
					{Name: "my-api", ActiveRelease: "rel-001", URL: "https://my-api.example.com"},
				},
			},
		}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/production", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "production")
		ctx := authtest.Context("github-lucas", "acme", "owner")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&result)
		if result["name"] != "production" {
			t.Fatalf("expected name=production, got %v", result["name"])
		}
		if result["status"] != "active" {
			t.Fatalf("expected status=active, got %v", result["status"])
		}
	})

	t.Run("GET /api/v1/environments/{name} → 404 when not found", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{
			detail: nil,
		}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/nonexistent", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "nonexistent")
		ctx := authtest.Context("github-lucas", "acme", "owner")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["code"] != "ENVIRONMENT_NOT_FOUND" {
			t.Fatalf("expected ENVIRONMENT_NOT_FOUND, got %v", errResp["code"])
		}
	})

	t.Run("GET /api/v1/environments/{name} → 401 without auth", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/production", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "production")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/environments/{name} → 500 on manager error", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{
			getErr: errors.New("k8s unavailable"),
		}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/production", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "production")
		ctx := authtest.Context("github-lucas", "acme", "owner")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestEnvironmentDeleteRoute(t *testing.T) {
	t.Run("DELETE /api/v1/environments/{name} → 200 on success", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "staging")
		ctx := authtest.Context("github-lucas", "acme", "owner")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Delete(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&result)
		if result["deleted"] != "staging" {
			t.Fatalf("expected deleted=staging, got %v", result["deleted"])
		}
	})

	t.Run("DELETE /api/v1/environments/{name} → 403 when deleting production", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/production", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "production")
		ctx := authtest.Context("github-lucas", "acme", "owner")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Delete(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["code"] != "FORBIDDEN" {
			t.Fatalf("expected FORBIDDEN error code, got %v", errResp["code"])
		}
	})

	t.Run("DELETE /api/v1/environments/{name} → 401 without auth", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "staging")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()

		handler.Delete(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("DELETE /api/v1/environments/{name} → 500 on manager error", func(t *testing.T) {
		mgr := &fakeEnvironmentManager{
			removeErr: errors.New("k8s unavailable"),
		}
		handler := environments.NewEnvironmentHandler(mgr)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "staging")
		ctx := authtest.Context("github-lucas", "acme", "owner")
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.Delete(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
