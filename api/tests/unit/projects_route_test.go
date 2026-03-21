package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/routes"
)

type fakeProjectStore struct {
	projects  map[string]bool
	createErr error
}

func (f *fakeProjectStore) CreateProject(ctx context.Context, p routes.CreateProjectInput) (routes.ProjectResult, error) {
	if f.createErr != nil {
		return routes.ProjectResult{}, f.createErr
	}
	key := p.TenantID + "/" + p.Name
	if f.projects[key] {
		return routes.ProjectResult{}, routes.ErrProjectExists
	}
	f.projects[key] = true
	return routes.ProjectResult{
		ID:     "proj-12345678",
		Name:   p.Name,
		TeamID: p.TenantID,
	}, nil
}

func (f *fakeProjectStore) GetByName(_ context.Context, tenantID, name string) (*routes.ProjectResult, error) {
	key := tenantID + "/" + name
	if f.projects[key] {
		return &routes.ProjectResult{ID: "proj-12345678", Name: name, TeamID: tenantID}, nil
	}
	return nil, nil
}

func (f *fakeProjectStore) DeleteProject(_ context.Context, _, _ string) error {
	return nil
}

type fakeTenantCRManager struct {
	calls    int
	lastSpec routes.TenantCRSpec
	err      error
}

func (f *fakeTenantCRManager) EnsureTenant(ctx context.Context, tenantID string, spec routes.TenantCRSpec) error {
	f.calls++
	f.lastSpec = spec
	return f.err
}

func makeAuthCtx(userID, tenantID, role string) context.Context {
	uc := &middleware.UserContext{
		UserID:   userID,
		Email:    userID + "@test.com",
		TenantID: tenantID,
		Role:     role,
	}
	return context.WithValue(context.Background(), middleware.UserContextKeyPublic(), uc)
}

func TestProjectsRoute(t *testing.T) {
	t.Run("POST /api/v1/projects → 201 with project ID", func(t *testing.T) {
		store := &fakeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{}
		handler := routes.NewProjectHandler(store, crMgr)

		body := `{"name":"my-saas","apps":[{"name":"api","framework":"express","port":3000,"path":"."}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
		handler := routes.NewProjectHandler(store, crMgr)

		body := `{"name":"my-saas"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
		handler := routes.NewProjectHandler(store, crMgr)

		body := `{"name":"My_Invalid Name!"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
		handler := routes.NewProjectHandler(store, crMgr)

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
		handler := routes.NewProjectHandler(store, crMgr)

		body := `{"name":"my-saas"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if crMgr.calls != 1 {
			t.Fatalf("expected EnsureTenant to be called once, got %d", crMgr.calls)
		}
	})

	t.Run("concurrent POST same name → exactly one 201 and one 409", func(t *testing.T) {
		store := &threadSafeProjectStore{projects: make(map[string]bool)}
		crMgr := &fakeTenantCRManager{}
		handler := routes.NewProjectHandler(store, crMgr)

		const concurrency = 10
		results := make([]int, concurrency)
		var wg sync.WaitGroup
		wg.Add(concurrency)

		for i := range concurrency {
			go func(idx int) {
				defer wg.Done()
				body := `{"name":"race-project"}`
				req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body)).
					WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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

// threadSafeProjectStore wraps fakeProjectStore with a mutex for concurrent test safety.
type threadSafeProjectStore struct {
	mu       sync.Mutex
	projects map[string]bool
}

func (f *threadSafeProjectStore) CreateProject(ctx context.Context, p routes.CreateProjectInput) (routes.ProjectResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := p.TenantID + "/" + p.Name
	if f.projects[key] {
		return routes.ProjectResult{}, routes.ErrProjectExists
	}
	f.projects[key] = true
	return routes.ProjectResult{
		ID:     "proj-12345678",
		Name:   p.Name,
		TeamID: p.TenantID,
	}, nil
}

func (f *threadSafeProjectStore) GetByName(_ context.Context, tenantID, name string) (*routes.ProjectResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tenantID + "/" + name
	if f.projects[key] {
		return &routes.ProjectResult{ID: "proj-12345678", Name: name, TeamID: tenantID}, nil
	}
	return nil, nil
}

func (f *threadSafeProjectStore) DeleteProject(_ context.Context, _, _ string) error {
	return nil
}
