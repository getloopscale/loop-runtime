// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package previewhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// withChiParams sets multiple chi URL parameters at once. Unlike
// withChiParam (which replaces the route context on each call), this
// helper accumulates all params into one context so handlers that read
// multiple URL params see them all.
func withChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	return r.WithContext(ctx)
}

// fakePreviewStore is a controllable PreviewStore for handler tests.
type fakePreviewStore struct {
	listByTenantFn  func(ctx context.Context, tenantID string) ([]ports.Preview, error)
	listByProjectFn func(ctx context.Context, tenantID, projectID string) ([]ports.Preview, error)
	getByIDFn       func(ctx context.Context, tenantID, previewID string) (*ports.Preview, error)
	requestDeleteFn func(ctx context.Context, tenantID, previewID string) error
}

func (f *fakePreviewStore) ListByTenant(ctx context.Context, tenantID string) ([]ports.Preview, error) {
	if f.listByTenantFn != nil {
		return f.listByTenantFn(ctx, tenantID)
	}
	return nil, nil
}
func (f *fakePreviewStore) ListByProject(ctx context.Context, tenantID, projectID string) ([]ports.Preview, error) {
	if f.listByProjectFn != nil {
		return f.listByProjectFn(ctx, tenantID, projectID)
	}
	return nil, nil
}
func (f *fakePreviewStore) GetByID(ctx context.Context, tenantID, previewID string) (*ports.Preview, error) {
	if f.getByIDFn != nil {
		return f.getByIDFn(ctx, tenantID, previewID)
	}
	return nil, loopzeroerrors.ErrPreviewNotFound
}
func (f *fakePreviewStore) RequestDeletion(ctx context.Context, tenantID, previewID string) error {
	if f.requestDeleteFn != nil {
		return f.requestDeleteFn(ctx, tenantID, previewID)
	}
	return nil
}

type fakePreviewService struct {
	createFn func(ctx context.Context, req CreatePreviewRequest) (*ports.Preview, error)
}

func (f *fakePreviewService) CreatePreview(ctx context.Context, req CreatePreviewRequest) (*ports.Preview, error) {
	if f.createFn != nil {
		return f.createFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

// =============================================================================
// List
// =============================================================================

func TestPreviewHandler_List_HappyPath(t *testing.T) {
	store := &fakePreviewStore{
		listByProjectFn: func(_ context.Context, tenantID, projectID string) ([]ports.Preview, error) {
			if tenantID != "tn-test-123" || projectID != "api" {
				t.Errorf("scoping wrong: tenant=%q project=%q", tenantID, projectID)
			}
			return []ports.Preview{
				{ID: "prev-a", ProjectID: "api", PRNumber: 1, Branch: "feat/a", Status: "active"},
				{ID: "prev-b", ProjectID: "api", PRNumber: 2, Branch: "feat/b", Status: "creating"},
			}, nil
		},
	}
	h := NewPreviewHandler(store, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/api/previews", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "project", "api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Previews []ports.Preview `json:"previews"`
		Count    int             `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 2 || len(resp.Previews) != 2 {
		t.Errorf("expected 2 previews, got count=%d len=%d", resp.Count, len(resp.Previews))
	}
}

func TestPreviewHandler_List_Unauthenticated(t *testing.T) {
	h := NewPreviewHandler(&fakePreviewStore{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/api/previews", nil)
	req = routestest.WithChiParam(req, "project", "api")
	w := httptest.NewRecorder()
	h.List(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestPreviewHandler_List_MissingProject(t *testing.T) {
	h := NewPreviewHandler(&fakePreviewStore{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects//previews", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPreviewHandler_List_NoInternalTermsInResponse(t *testing.T) {
	// Rule 3: tenant_id and ArgoCD/GitOps should not leak.
	store := &fakePreviewStore{
		listByProjectFn: func(_ context.Context, _, _ string) ([]ports.Preview, error) {
			return []ports.Preview{
				{ID: "prev-a", TenantID: "tn-test-123", ProjectID: "api", PRNumber: 1, Branch: "main", Status: "active"},
			}, nil
		},
	}
	h := NewPreviewHandler(store, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/api/previews", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "project", "api")
	w := httptest.NewRecorder()
	h.List(w, req)

	body := w.Body.String()
	for _, term := range []string{"tenant_id", "ArgoCD", "argocd", "gitops-push", "Gitea", "MinIO", "Kaniko"} {
		if strings.Contains(body, term) {
			t.Errorf("Rule 3 violation: response contains %q\nBody: %s", term, body)
		}
	}
}

// =============================================================================
// Get
// =============================================================================

func TestPreviewHandler_Get_HappyPath(t *testing.T) {
	store := &fakePreviewStore{
		getByIDFn: func(_ context.Context, tenantID, previewID string) (*ports.Preview, error) {
			if tenantID != "tn-test-123" || previewID != "prev-x" {
				t.Errorf("scoping wrong: tenant=%q preview=%q", tenantID, previewID)
			}
			return &ports.Preview{ID: "prev-x", ProjectID: "api", PRNumber: 7, Branch: "topic", Status: "active"}, nil
		},
	}
	h := NewPreviewHandler(store, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/api/previews/prev-x", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = withChiParams(req, map[string]string{"project": "api", "id": "prev-x"})
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestPreviewHandler_Get_NotFound(t *testing.T) {
	store := &fakePreviewStore{
		getByIDFn: func(_ context.Context, _, _ string) (*ports.Preview, error) {
			return nil, loopzeroerrors.ErrPreviewNotFound
		},
	}
	h := NewPreviewHandler(store, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/api/previews/x", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = withChiParams(req, map[string]string{"project": "api", "id": "missing"})
	w := httptest.NewRecorder()
	h.Get(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestPreviewHandler_Get_CrossProjectIsolation(t *testing.T) {
	// Even when the preview exists for the tenant, it should not be
	// returned if the URL project doesn't match.
	store := &fakePreviewStore{
		getByIDFn: func(_ context.Context, _, _ string) (*ports.Preview, error) {
			return &ports.Preview{ID: "prev-x", ProjectID: "api", PRNumber: 1, Branch: "x", Status: "active"}, nil
		},
	}
	h := NewPreviewHandler(store, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/web/previews/prev-x", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	// different project in URL!
	req = withChiParams(req, map[string]string{"project": "web", "id": "prev-x"})
	w := httptest.NewRecorder()
	h.Get(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-project access should 404, got %d", w.Code)
	}
}

// =============================================================================
// Delete
// =============================================================================

func TestPreviewHandler_Delete_HappyPath(t *testing.T) {
	deleteCalled := false
	store := &fakePreviewStore{
		getByIDFn: func(_ context.Context, _, _ string) (*ports.Preview, error) {
			return &ports.Preview{ID: "prev-x", ProjectID: "api", PRNumber: 1, Status: "active"}, nil
		},
		requestDeleteFn: func(_ context.Context, tenantID, previewID string) error {
			deleteCalled = true
			if tenantID != "tn-test-123" || previewID != "prev-x" {
				t.Errorf("scoping wrong: tenant=%q preview=%q", tenantID, previewID)
			}
			return nil
		},
	}
	h := NewPreviewHandler(store, nil, nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/api/previews/prev-x", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = withChiParams(req, map[string]string{"project": "api", "id": "prev-x"})
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if !deleteCalled {
		t.Error("RequestDeletion was not called")
	}
}

func TestPreviewHandler_Delete_NotFound(t *testing.T) {
	store := &fakePreviewStore{
		getByIDFn: func(_ context.Context, _, _ string) (*ports.Preview, error) {
			return nil, loopzeroerrors.ErrPreviewNotFound
		},
	}
	h := NewPreviewHandler(store, nil, nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/api/previews/missing", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = withChiParams(req, map[string]string{"project": "api", "id": "missing"})
	w := httptest.NewRecorder()
	h.Delete(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// =============================================================================
// Create
// =============================================================================

func TestPreviewHandler_Create_HappyPath(t *testing.T) {
	svc := &fakePreviewService{
		createFn: func(_ context.Context, req CreatePreviewRequest) (*ports.Preview, error) {
			if req.TenantID() != "tn-test-123" {
				t.Errorf("tenant injection failed: got %q", req.TenantID())
			}
			if req.ProjectID != "api" {
				t.Errorf("project from URL not preserved: %q", req.ProjectID)
			}
			if req.PRNumber != 42 || req.Branch != "feature/x" || req.CommitSHA != "abc1234" {
				t.Errorf("body fields wrong: %+v", req)
			}
			return &ports.Preview{
				ID: "prev-new", ProjectID: req.ProjectID, PRNumber: req.PRNumber,
				Branch: req.Branch, Status: "creating",
			}, nil
		},
	}
	body := []byte(`{"pr_number":42,"branch":"feature/x","commit_sha":"abc1234","repo_full_name":"acme/api"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/api/previews", bytes.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "project", "api")
	w := httptest.NewRecorder()

	h := NewPreviewHandler(&fakePreviewStore{}, svc, nil)
	h.Create(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestPreviewHandler_Create_RejectsBadJSON(t *testing.T) {
	svc := &fakePreviewService{}
	h := NewPreviewHandler(&fakePreviewStore{}, svc, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/api/previews", strings.NewReader("not json"))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "project", "api")
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPreviewHandler_Create_RejectsMissingFields(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"pr_number missing", `{"branch":"x","commit_sha":"y"}`},
		{"pr_number zero", `{"pr_number":0,"branch":"x","commit_sha":"y"}`},
		{"branch missing", `{"pr_number":1,"commit_sha":"y"}`},
		{"commit_sha missing", `{"pr_number":1,"branch":"x"}`},
	}
	for _, tc := range cases {
		// Go 1.22+: per-iteration loop variable; no shadow needed.
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakePreviewService{}
			h := NewPreviewHandler(&fakePreviewStore{}, svc, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/api/previews", strings.NewReader(tc.body))
			req = routestest.WithAuth(req, routestest.DefaultUserContext())
			req = routestest.WithChiParam(req, "project", "api")
			w := httptest.NewRecorder()
			h.Create(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestPreviewHandler_Create_NoServiceWired(t *testing.T) {
	// When the PreviewService is not configured, Create returns 5xx
	// rather than crashing.
	h := NewPreviewHandler(&fakePreviewStore{}, nil, nil)
	body := []byte(`{"pr_number":1,"branch":"x","commit_sha":"y"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/api/previews", bytes.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "project", "api")
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code < 500 || w.Code > 599 {
		t.Fatalf("expected 5xx, got %d", w.Code)
	}
}

// =============================================================================
// Compile-time check: PreviewHandler satisfies the routes interface
// =============================================================================

func TestPreviewHandler_Construction(t *testing.T) {
	h := NewPreviewHandler(&fakePreviewStore{}, &fakePreviewService{}, nil)
	if h == nil {
		t.Fatal("handler should not be nil")
	}
}

// =============================================================================
// Red Team loopzero-cloud#92 regression-protector (2026-06-27): missing-parent-guard.
// When a PreviewProjectChecker is wired, every preview handler MUST refuse
// to operate on non-existent projects with 404 PROJECT_NOT_FOUND instead
// of 200 + empty list.
// =============================================================================

type fakePreviewProjectChecker struct {
	getByNameFn func(ctx context.Context, tenantID, name string) (*ports.ProjectResult, error)
}

func (f *fakePreviewProjectChecker) GetByName(ctx context.Context, tenantID, name string) (*ports.ProjectResult, error) {
	if f.getByNameFn != nil {
		return f.getByNameFn(ctx, tenantID, name)
	}
	return nil, nil
}

func TestPreviewHandler_List_NonExistentProject_Returns404(t *testing.T) {
	// Red Team #92: project does not exist for this tenant → 404
	// PROJECT_NOT_FOUND (canonical defense + no existence leak).
	checker := &fakePreviewProjectChecker{
		getByNameFn: func(_ context.Context, _, _ string) (*ports.ProjectResult, error) {
			return nil, nil // project not found
		},
	}
	h := NewPreviewHandler(&fakePreviewStore{}, nil, nil).WithProjectChecker(checker)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nonexistent-zzz/previews", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "project", "nonexistent-zzz")
	w := httptest.NewRecorder()
	h.List(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 PROJECT_NOT_FOUND, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PROJECT_NOT_FOUND") {
		t.Errorf("response body must include PROJECT_NOT_FOUND code, got %s", w.Body.String())
	}
}

func TestPreviewHandler_List_ExistingProject_AllowsThrough(t *testing.T) {
	// Red Team #92: project exists → handler proceeds normally.
	checker := &fakePreviewProjectChecker{
		getByNameFn: func(_ context.Context, _, _ string) (*ports.ProjectResult, error) {
			return &ports.ProjectResult{Name: "api"}, nil
		},
	}
	store := &fakePreviewStore{
		listByProjectFn: func(_ context.Context, _, _ string) ([]ports.Preview, error) {
			return []ports.Preview{{ID: "prev-1"}}, nil
		},
	}
	h := NewPreviewHandler(store, nil, nil).WithProjectChecker(checker)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/api/previews", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "project", "api")
	w := httptest.NewRecorder()
	h.List(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPreviewHandler_List_NilChecker_BackwardsCompat(t *testing.T) {
	// Without a wired checker (legacy/test path), the gate is skipped.
	h := NewPreviewHandler(&fakePreviewStore{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/api/previews", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "project", "api")
	w := httptest.NewRecorder()
	h.List(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (nil checker = no gate), got %d", w.Code)
	}
}

func TestPreviewHandler_Get_NonExistentProject_Returns404(t *testing.T) {
	checker := &fakePreviewProjectChecker{
		getByNameFn: func(_ context.Context, _, _ string) (*ports.ProjectResult, error) {
			return nil, nil
		},
	}
	h := NewPreviewHandler(&fakePreviewStore{}, nil, nil).WithProjectChecker(checker)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nonexistent/previews/prev-1", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = withChiParams(req, map[string]string{"project": "nonexistent", "id": "prev-1"})
	w := httptest.NewRecorder()
	h.Get(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 PROJECT_NOT_FOUND on Get, got %d body=%s", w.Code, w.Body.String())
	}
}
