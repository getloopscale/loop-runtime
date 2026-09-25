// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package projectshttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/middleware/auth/authtest"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/projects/projectshttp"

	"github.com/go-chi/chi/v5"
)

type fakeProjectStore struct {
	projects  map[string]bool
	createErr error
	listErr   error
}

func (f *fakeProjectStore) CreateProject(ctx context.Context, p ports.CreateProjectInput) (ports.ProjectResult, error) {
	if f.createErr != nil {
		return ports.ProjectResult{}, f.createErr
	}
	key := p.TenantID + "/" + p.Name
	if f.projects[key] {
		return ports.ProjectResult{}, ports.ErrProjectExists
	}
	f.projects[key] = true
	return ports.ProjectResult{
		ID:     "proj-12345678",
		Name:   p.Name,
		TeamID: p.TenantID,
	}, nil
}

func (f *fakeProjectStore) GetByName(_ context.Context, tenantID, name string) (*ports.ProjectResult, error) {
	key := tenantID + "/" + name
	if f.projects[key] {
		return &ports.ProjectResult{ID: "proj-12345678", Name: name, TeamID: tenantID}, nil
	}
	return nil, nil
}

func (f *fakeProjectStore) ListByTenant(_ context.Context, tenantID string) ([]ports.ProjectResult, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var results []ports.ProjectResult
	for key := range f.projects {
		parts := splitKey(key)
		if parts[0] == tenantID {
			results = append(results, ports.ProjectResult{
				ID:     "proj-12345678",
				Name:   parts[1],
				TeamID: tenantID,
			})
		}
	}
	if results == nil {
		results = []ports.ProjectResult{}
	}
	return results, nil
}

func splitKey(key string) [2]string {
	for i, c := range key {
		if c == '/' {
			return [2]string{key[:i], key[i+1:]}
		}
	}
	return [2]string{key, ""}
}

// T2.1 + ADR D2: per-project summary variant. Mirrors ListByTenant
// shape; summary fields default to zero (post-handler-test the production
// route renders these in the response — these unit tests focus on the
// shape/auth/error paths, not the summary semantics, which live in
// status_mapper_test.go + integration tests).
func (f *fakeProjectStore) ListByTenantWithSummary(_ context.Context, tenantID string) ([]ports.ProjectSummary, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var results []ports.ProjectSummary
	for key := range f.projects {
		parts := splitKey(key)
		if parts[0] == tenantID {
			results = append(results, ports.ProjectSummary{
				ID:               "proj-12345678",
				Name:             parts[1],
				TeamID:           tenantID,
				EnvironmentCount: 0,
				LastDeployStatus: "idle",
			})
		}
	}
	if results == nil {
		results = []ports.ProjectSummary{}
	}
	return results, nil
}

func (f *fakeProjectStore) DeleteProject(_ context.Context, _, _ string) error {
	return nil
}

type fakeTenantCRManager struct {
	calls    int
	lastSpec ports.TenantCRSpec
	err      error
}

func (f *fakeTenantCRManager) EnsureTenant(ctx context.Context, tenantID string, spec ports.TenantCRSpec) error {
	f.calls++
	f.lastSpec = spec
	return f.err
}

func TestProjectsRoute(t *testing.T) {
	t.Run("POST /api/v1/projects → 201 with project ID", func(t *testing.T) {
		store := &fakeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		body := `{"name":"my-saas","apps":[{"name":"api","framework":"express","port":3000,"path":"."}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&result)
		if result["id"] == nil || result["id"] == "" {
			t.Fatal("expected project ID in response")
		}
	})

	t.Run("POST /api/v1/projects → 409 if name already exists in tenant", func(t *testing.T) {
		store := &fakeProjectStore{
			projects: map[string]bool{"acme/my-saas": true},
		}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		body := `{"name":"my-saas"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/projects → 400 if name is not RFC 1123", func(t *testing.T) {
		store := &fakeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		body := `{"name":"My_Invalid Name!"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/projects → 401 without auth", func(t *testing.T) {
		store := &fakeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		body := `{"name":"my-saas"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/projects → calls TenantCRManager.EnsureTenant", func(t *testing.T) {
		store := &fakeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		body := `{"name":"my-saas"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if crMgr.calls != 1 {
			t.Fatalf("expected EnsureTenant to be called once, got %d", crMgr.calls)
		}
	})

	t.Run("POST /api/v1/projects → 503 if EnsureTenant fails (ADR-0006 Invariant D)", func(t *testing.T) {
		store := &fakeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{err: context.DeadlineExceeded}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		body := `{"name":"my-saas"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 when EnsureTenant fails, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["code"] == nil {
			t.Fatal("expected error response with 'code' field")
		}
	})

	t.Run("concurrent POST same name → exactly one 201 and one 409", func(t *testing.T) {
		store := &threadSafeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		const concurrency = 10
		results := make([]int, concurrency)
		var wg sync.WaitGroup
		wg.Add(concurrency)

		for i := range concurrency {
			go func(idx int) {
				defer wg.Done()
				body := `{"name":"race-project"}`
				req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body)).
					WithContext(authtest.Context("github-lucas", "acme", "owner"))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				handler.Create(rec, req)
				results[idx] = rec.Code
			}(i)
		}
		wg.Wait()

		count201 := 0
		count409 := 0
		for _, code := range results {
			switch code {
			case http.StatusCreated:
				count201++
			case http.StatusConflict:
				count409++
			default:
				t.Errorf("unexpected status code: %d", code)
			}
		}

		if count201 != 1 {
			t.Errorf("expected exactly 1 x 201, got %d", count201)
		}
		if count409 != concurrency-1 {
			t.Errorf("expected %d x 409, got %d", concurrency-1, count409)
		}
	})
}

func TestProjectListRoute(t *testing.T) {
	t.Run("GET /api/v1/projects → 200 with projects list", func(t *testing.T) {
		store := &fakeProjectStore{
			projects: map[string]bool{
				"acme/api-service": true,
				"acme/web-app":     true,
			},
		}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&result)
		projects, ok := result["projects"].([]interface{})
		if !ok {
			t.Fatal("expected 'projects' array in response")
		}
		if len(projects) != 2 {
			t.Fatalf("expected 2 projects, got %d", len(projects))
		}
	})

	t.Run("GET /api/v1/projects → 200 with empty list when no projects", func(t *testing.T) {
		store := &fakeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&result)
		projects, ok := result["projects"].([]interface{})
		if !ok {
			t.Fatal("expected 'projects' array in response")
		}
		if len(projects) != 0 {
			t.Fatalf("expected 0 projects, got %d", len(projects))
		}
	})

	t.Run("GET /api/v1/projects → 401 without auth", func(t *testing.T) {
		store := &fakeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/projects → only returns tenant's own projects", func(t *testing.T) {
		store := &fakeProjectStore{
			projects: map[string]bool{
				"acme/my-app":      true,
				"other-tenant/app": true,
			},
		}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		handler.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&result)
		projects := result["projects"].([]interface{})
		if len(projects) != 1 {
			t.Fatalf("expected 1 project for acme tenant, got %d", len(projects))
		}
	})
}

func TestProjectGetRoute(t *testing.T) {
	t.Run("GET /api/v1/projects/{name} → 200 with project details", func(t *testing.T) {
		store := &fakeProjectStore{
			projects: map[string]bool{"acme/my-app": true},
		}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-app", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "my-app")
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
		if result["name"] != "my-app" {
			t.Fatalf("expected name=my-app, got %v", result["name"])
		}
		if result["team"] != "acme" {
			t.Fatalf("expected team=acme, got %v", result["team"])
		}
	})

	t.Run("GET /api/v1/projects/{name} → 404 when not found", func(t *testing.T) {
		store := &fakeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nonexistent", nil)
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
		if errResp["code"] != "PROJECT_NOT_FOUND" {
			t.Fatalf("expected error code PROJECT_NOT_FOUND, got %v", errResp["code"])
		}
	})

	t.Run("GET /api/v1/projects/{name} → 401 without auth", func(t *testing.T) {
		store := &fakeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{}
		handler := projectshttp.NewProjectHandler(store, crMgr)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-app", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "my-app")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}

// threadSafeProjectStore wraps fakeProjectStore with a mutex for concurrent test safety.
type threadSafeProjectStore struct {
	mu       sync.Mutex
	projects map[string]bool
}

func (f *threadSafeProjectStore) CreateProject(ctx context.Context, p ports.CreateProjectInput) (ports.ProjectResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := p.TenantID + "/" + p.Name
	if f.projects[key] {
		return ports.ProjectResult{}, ports.ErrProjectExists
	}
	f.projects[key] = true
	return ports.ProjectResult{
		ID:     "proj-12345678",
		Name:   p.Name,
		TeamID: p.TenantID,
	}, nil
}

func (f *threadSafeProjectStore) GetByName(_ context.Context, tenantID, name string) (*ports.ProjectResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tenantID + "/" + name
	if f.projects[key] {
		return &ports.ProjectResult{ID: "proj-12345678", Name: name, TeamID: tenantID}, nil
	}
	return nil, nil
}

func (f *threadSafeProjectStore) ListByTenant(_ context.Context, tenantID string) ([]ports.ProjectResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var results []ports.ProjectResult
	for key := range f.projects {
		parts := splitKey(key)
		if parts[0] == tenantID {
			results = append(results, ports.ProjectResult{
				ID:     "proj-12345678",
				Name:   parts[1],
				TeamID: tenantID,
			})
		}
	}
	if results == nil {
		results = []ports.ProjectResult{}
	}
	return results, nil
}

// T2.1 + ADR D2: thread-safe summary variant.
func (f *threadSafeProjectStore) ListByTenantWithSummary(_ context.Context, tenantID string) ([]ports.ProjectSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var results []ports.ProjectSummary
	for key := range f.projects {
		parts := splitKey(key)
		if parts[0] == tenantID {
			results = append(results, ports.ProjectSummary{
				ID:               "proj-12345678",
				Name:             parts[1],
				TeamID:           tenantID,
				EnvironmentCount: 0,
				LastDeployStatus: "idle",
			})
		}
	}
	if results == nil {
		results = []ports.ProjectSummary{}
	}
	return results, nil
}

func (f *threadSafeProjectStore) DeleteProject(_ context.Context, _, _ string) error {
	return nil
}
