// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package projectshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/routestest"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// M4 T3.1 — when the store rejects creation with the per-Account cap typed error
// (PROJECTS_EXCEED_PLAN), the handler surfaces it as HTTP 422 (not a 500), so the
// client gets the actionable upgrade message. Mirrors the #101 scale-cap 422.
func TestProjects_Create_CapExceeded_Returns422(t *testing.T) {
	store := &fakeProjectStore{
		createErr: loopzeroerrors.NewProjectsExceedPlan("starter", 1, 1, "acct-x"),
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(`{"name":"api"}`))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != 422 {
		t.Fatalf("want 422 (PROJECTS_EXCEED_PLAN), got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PROJECTS_EXCEED_PLAN") {
		t.Errorf("response missing the typed code: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "loopzero.example/pricing") {
		t.Errorf("response missing the upgrade URL (EC-4): %s", w.Body.String())
	}
}

// --- Fake ProjectStore ---

type fakeProjectStore struct {
	projects       []ports.ProjectResult
	projectSummary []ProjectSummary // T2.1: per-project summary fixture
	project        *ports.ProjectResult
	createResult   ports.ProjectResult
	createErr      error
	deleteErr      error
	summaryErr     error
}

func (f *fakeProjectStore) CreateProject(_ context.Context, input CreateProjectInput) (ports.ProjectResult, error) {
	if f.createErr != nil {
		return ports.ProjectResult{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeProjectStore) GetByName(_ context.Context, _, name string) (*ports.ProjectResult, error) {
	if f.project != nil && f.project.Name == name {
		return f.project, nil
	}
	return nil, nil
}

func (f *fakeProjectStore) ListByTenant(_ context.Context, _ string) ([]ports.ProjectResult, error) {
	return f.projects, nil
}

// T2.1 + ADR D2: per-project summary variant. If the test sets
// f.projectSummary it wins (lets a test pin specific summary fields).
// Otherwise, auto-promote f.projects (legacy fixture field) to
// summaries with zero env_count + idle status — preserves backward
// compat for the ~12 tests that wrote f.projects directly.
func (f *fakeProjectStore) ListByTenantWithSummary(_ context.Context, _ string) ([]ProjectSummary, error) {
	if f.summaryErr != nil {
		return nil, f.summaryErr
	}
	if f.projectSummary != nil {
		return f.projectSummary, nil
	}
	out := make([]ProjectSummary, 0, len(f.projects))
	for _, p := range f.projects {
		out = append(out, ProjectSummary{
			ID:               p.ID,
			Name:             p.Name,
			TeamID:           p.TeamID,
			CreatedBy:        p.CreatedBy,
			CreatedAt:        p.CreatedAt,
			EnvironmentCount: 0,
			LastDeployStatus: "idle",
		})
	}
	return out, nil
}

func (f *fakeProjectStore) DeleteProject(_ context.Context, _, _ string) error {
	return f.deleteErr
}

// --- Fake TenantCRManager ---

type fakeTenantCRMgr struct {
	err          error
	capturedSpec TenantCRSpec // T2.3.b: assert canonical Plan flows through.
}

func (f *fakeTenantCRMgr) EnsureTenant(_ context.Context, _ string, s TenantCRSpec) error {
	f.capturedSpec = s
	return f.err
}

// --- Fake PlanTierStore (T2.3.b) ---

// fakePlanTierStore satisfies ports.PlanTierStore so the Create
// handler's ResolvePlanTier call can be exercised without a real DB.
type fakePlanTierStore struct {
	plan string
	err  error
}

func (f fakePlanTierStore) GetTenantPlan(_ context.Context, _ string) (string, error) {
	return f.plan, f.err
}

// --- Fake ProjectDeleter ---

type fakeProjectDeleter struct {
	result *ProjectDeleteOutput
	err    error
}

func (f *fakeProjectDeleter) Delete(_ context.Context, _ ProjectDeleteInput) (*ProjectDeleteOutput, error) {
	return f.result, f.err
}

// --- Tests ---

func TestProjects_List_HappyPath(t *testing.T) {
	store := &fakeProjectStore{
		projects: []ports.ProjectResult{
			{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
			{ID: "p2", Name: "my-web", TeamID: "tn-test-123"},
		},
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	// T2.3 pagination scaffold: response now carries `total` alongside
	// `projects` — typed struct keeps the assertion focused.
	var resp struct {
		Projects []ports.ProjectResult `json:"projects"`
		Total    int                   `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Projects) != 2 {
		t.Fatalf("want 2 projects, got %d", len(resp.Projects))
	}
}

// T2.1 + ADR D2: handler returns the summary shape (environment_count +
// last_deploy_status) when the store's ListByTenantWithSummary is wired.
// Validates the wire-format additive contract: legacy fields preserved +
// 2 new fields present per project.
func TestProjects_List_SummaryShape(t *testing.T) {
	store := &fakeProjectStore{
		projectSummary: []ProjectSummary{
			{
				ID:               "p1",
				Name:             "my-api",
				TeamID:           "tn-test-123",
				EnvironmentCount: 3,
				LastDeployStatus: "live",
			},
			{
				ID:               "p2",
				Name:             "my-web",
				TeamID:           "tn-test-123",
				EnvironmentCount: 0,
				LastDeployStatus: "idle",
			},
		},
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	// T2.3 pagination scaffold: response now carries `total` alongside
	// `projects`.
	var resp struct {
		Projects []ProjectSummary `json:"projects"`
		Total    int              `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Projects) != 2 {
		t.Fatalf("want 2 projects, got %d", len(resp.Projects))
	}
	p1 := resp.Projects[0]
	if p1.Name != "my-api" || p1.EnvironmentCount != 3 || p1.LastDeployStatus != "live" {
		t.Errorf("p1 shape mismatch: %+v", p1)
	}
	p2 := resp.Projects[1]
	if p2.Name != "my-web" || p2.EnvironmentCount != 0 || p2.LastDeployStatus != "idle" {
		t.Errorf("p2 shape mismatch: %+v", p2)
	}
}

// T2.3 + ADR D8: pagination scaffold tests. Pinning the contract:
// ?limit= and ?offset= honored, total reflects unpaginated count,
// limit capped at 100 (defense vs scan DoS), defaults preserve
// backward compat (?limit= absent → 50, ?offset= absent → 0).

func makeProjectsList(n int) []ProjectSummary {
	out := make([]ProjectSummary, n)
	for i := 0; i < n; i++ {
		out[i] = ProjectSummary{
			ID:               "p-" + string(rune('a'+i)),
			Name:             "proj-" + string(rune('a'+i)),
			TeamID:           "tn-test-123",
			EnvironmentCount: 0,
			LastDeployStatus: "idle",
		}
	}
	return out
}

func TestProjects_List_PaginationLimitHonored(t *testing.T) {
	store := &fakeProjectStore{projectSummary: makeProjectsList(5)}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects?limit=2", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Projects []ProjectSummary `json:"projects"`
		Total    int              `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Projects) != 2 {
		t.Errorf("want 2 projects (limit), got %d", len(resp.Projects))
	}
	if resp.Total != 5 {
		t.Errorf("total should reflect unpaginated count: want 5, got %d", resp.Total)
	}
}

func TestProjects_List_PaginationOffsetHonored(t *testing.T) {
	store := &fakeProjectStore{projectSummary: makeProjectsList(5)}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects?offset=2&limit=2", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)

	var resp struct {
		Projects []ProjectSummary `json:"projects"`
		Total    int              `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Projects) != 2 || resp.Projects[0].Name != "proj-c" {
		t.Errorf("offset=2 should skip first 2 (a,b) and return c,d; got %+v", resp.Projects)
	}
	if resp.Total != 5 {
		t.Errorf("want total=5, got %d", resp.Total)
	}
}

func TestProjects_List_PaginationLimitCappedAt100(t *testing.T) {
	store := &fakeProjectStore{projectSummary: makeProjectsList(150)}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects?limit=1000", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)

	var resp struct {
		Projects []ProjectSummary `json:"projects"`
		Total    int              `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Projects) != 100 {
		t.Errorf("EC-6: limit must be capped at 100, got %d", len(resp.Projects))
	}
	if resp.Total != 150 {
		t.Errorf("total should be 150 (unpaginated count), got %d", resp.Total)
	}
}

func TestProjects_List_PaginationDefaultsBackwardsCompat(t *testing.T) {
	// No ?limit= and no ?offset= → defaults (limit=50, offset=0).
	// Backwards compat: CLI clients that never sent pagination params
	// still get the full default window.
	store := &fakeProjectStore{projectSummary: makeProjectsList(30)}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)

	var resp struct {
		Projects []ProjectSummary `json:"projects"`
		Total    int              `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	// 30 projects, default limit 50 → all 30 returned.
	if len(resp.Projects) != 30 {
		t.Errorf("default limit=50 should return all 30, got %d", len(resp.Projects))
	}
	if resp.Total != 30 {
		t.Errorf("want total=30, got %d", resp.Total)
	}
}

// T2.1 + EC-2: handler propagates summary errors from the store. The
// /projects handler currently maps any store error to 500 (per legacy
// shape — the dashboard treats this as "API unhealthy"). Pinning the
// status code keeps the contract observable.
func TestProjects_List_SummaryErrorReturns500(t *testing.T) {
	store := &fakeProjectStore{summaryErr: errors.New("synthetic db error")}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_List_Unauthenticated(t *testing.T) {
	h := NewProjectHandler(&fakeProjectStore{}, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestProjects_Get_Found(t *testing.T) {
	// T5.2 of plan 2026-06-10-j22-layer2-code-review-fixes: Get handler was
	// refactored to use ListByTenantWithSummary in T2.1 (2026-06-08 dashboard
	// screens review). Pre-fix fixture only set the `project` field which fed
	// the legacy GetByName path; the new lookup iterates `projectSummary`
	// looking for matching `Name`. Updating fixture restores GREEN.
	store := &fakeProjectStore{
		projectSummary: []ProjectSummary{
			{
				ID:        "p1",
				Name:      "my-api",
				TeamID:    "tn-test-123",
				CreatedAt: time.Now(),
			},
		},
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp ports.ProjectResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Name != "my-api" {
		t.Fatalf("want name my-api, got %s", resp.Name)
	}
}

func TestProjects_Get_NotFound(t *testing.T) {
	store := &fakeProjectStore{} // nil project
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nonexistent", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestProjects_Create_ResolvedPlanTierFlowsToTenantCR — T2.3.b closes
// H7 (CRITICAL): the Create handler must NOT hardcode `Plan: "starter"`
// in EnsureTenant. When a PlanTierStore is wired, the resolved tier
// MUST be the value passed to the Tenant CR.
func TestProjects_Create_ResolvedPlanTierFlowsToTenantCR(t *testing.T) {
	store := &fakeProjectStore{
		createResult: ports.ProjectResult{ID: "p1", Name: "api", TeamID: "tn-paying"},
	}
	crMgr := &fakeTenantCRMgr{}
	h := NewProjectHandler(store, crMgr)
	// Tenant is on Pro plan — silent downgrade to Starter is the bug.
	h.SetPlanTierStore(fakePlanTierStore{plan: "pro"})

	body := `{"name":"api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects",
		bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	if crMgr.capturedSpec.Plan != "pro" {
		t.Errorf("EnsureTenant got Plan=%q; want %q (H7 silent downgrade)",
			crMgr.capturedSpec.Plan, "pro")
	}
}

// TestProjects_Create_LegacyGrowthMapsToProInCR — ADR D4 alias path.
// Existing tenants with DB Plan="growth" (pre-MEET-2026-05-12 pricing)
// must surface as canonical Pro at the CR boundary.
func TestProjects_Create_LegacyGrowthMapsToProInCR(t *testing.T) {
	store := &fakeProjectStore{
		createResult: ports.ProjectResult{ID: "p1", Name: "api", TeamID: "tn-legacy"},
	}
	crMgr := &fakeTenantCRMgr{}
	h := NewProjectHandler(store, crMgr)
	h.SetPlanTierStore(fakePlanTierStore{plan: "growth"})

	body := `{"name":"api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects",
		bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	if crMgr.capturedSpec.Plan != "pro" {
		t.Errorf("legacy growth must map to pro at CR boundary: got %q",
			crMgr.capturedSpec.Plan)
	}
}

// TestProjects_Create_UnknownPlanReturns503 — ADR D4 closure invariant:
// when the store returns an unrecognized Plan value, the handler MUST
// surface 503 instead of silently downgrading to Starter.
func TestProjects_Create_UnknownPlanReturns503(t *testing.T) {
	store := &fakeProjectStore{
		createResult: ports.ProjectResult{ID: "p1", Name: "api", TeamID: "tn-x"},
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})
	h.SetPlanTierStore(fakePlanTierStore{plan: "platinum-vip"})

	body := `{"name":"api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects",
		bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("unknown plan must surface 503; got %d body=%s",
			w.Code, w.Body.String())
	}
}

// TestProjects_Create_NoStoreFallsBackToStarter — defensive transition
// path. When PlanTierStore is nil (legacy main.go without wire-up), the
// handler falls back to Starter — same behavior as pre-T2.3.b. This
// keeps existing tests passing during the transition window. The
// production wire-up in main.go IS the closure of the bug.
func TestProjects_Create_NoStoreFallsBackToStarter(t *testing.T) {
	store := &fakeProjectStore{
		createResult: ports.ProjectResult{ID: "p1", Name: "api", TeamID: "tn-x"},
	}
	crMgr := &fakeTenantCRMgr{}
	h := NewProjectHandler(store, crMgr) // no SetPlanTierStore call

	body := `{"name":"api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects",
		bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	if crMgr.capturedSpec.Plan != "starter" {
		t.Errorf("nil store transition fallback must be starter, got %q",
			crMgr.capturedSpec.Plan)
	}
}

func TestProjects_Create_HappyPath(t *testing.T) {
	store := &fakeProjectStore{
		createResult: ports.ProjectResult{
			ID:     "p1",
			Name:   "my-api",
			TeamID: "tn-test-123",
		},
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	body := `{"name":"my-api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}

	var resp ports.ProjectResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Name != "my-api" {
		t.Fatalf("want name my-api, got %s", resp.Name)
	}
}

func TestProjects_Create_AlreadyExists(t *testing.T) {
	store := &fakeProjectStore{
		createErr: ErrProjectExists,
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})

	body := `{"name":"my-api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_Create_InvalidName(t *testing.T) {
	h := NewProjectHandler(&fakeProjectStore{}, &fakeTenantCRMgr{})

	body := `{"name":"UPPERCASE-BAD"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_Create_InvalidJSON(t *testing.T) {
	h := NewProjectHandler(&fakeProjectStore{}, &fakeTenantCRMgr{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString("not-json"))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_Delete_Success(t *testing.T) {
	store := &fakeProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	deleter := &fakeProjectDeleter{
		result: &ProjectDeleteOutput{
			ProjectName:         "my-api",
			DeletedEnvironments: []string{"production"},
			CancelledBuilds:     0,
		},
	}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})
	h.SetDeleter(deleter)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/my-api", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_Delete_NotFound(t *testing.T) {
	store := &fakeProjectStore{} // nil project
	h := NewProjectHandler(store, &fakeTenantCRMgr{})
	h.SetDeleter(&fakeProjectDeleter{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/nonexistent", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProjects_Delete_NoDeleter(t *testing.T) {
	store := &fakeProjectStore{}
	h := NewProjectHandler(store, &fakeTenantCRMgr{})
	// deleter not set

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/my-api", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
}

type fakeTenantCellResolver struct {
	cell string
	err  error
}

func (f fakeTenantCellResolver) GetTenantCellID(_ context.Context, _ string) (string, error) {
	return f.cell, f.err
}

// The Tenant CR is created at the first project, and it must name the cell the
// engine placed the tenant on — tenants.cell_id — not the CRD default (#412).
func TestProjects_Create_TheTenantCRNamesThePlacedCell(t *testing.T) {
	store := &fakeProjectStore{createResult: ports.ProjectResult{ID: "p1", Name: "api", TeamID: "tn-x"}}
	crMgr := &fakeTenantCRMgr{}
	h := NewProjectHandler(store, crMgr)
	h.SetTenantCellResolver(fakeTenantCellResolver{cell: "cell-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(`{"name":"api"}`))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	if crMgr.capturedSpec.CellID != "cell-1" {
		t.Errorf("EnsureTenant got CellID=%q, want cell-1", crMgr.capturedSpec.CellID)
	}
}

// A cell that cannot be read is not the default cell: provisioning on a guess is how
// the tenant ends up split across two cells.
func TestProjects_Create_CellLookupFailureReturns503(t *testing.T) {
	store := &fakeProjectStore{createResult: ports.ProjectResult{ID: "p1", Name: "api", TeamID: "tn-x"}}
	crMgr := &fakeTenantCRMgr{}
	h := NewProjectHandler(store, crMgr)
	h.SetTenantCellResolver(fakeTenantCellResolver{err: errors.New("cell store: no rows")})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(`{"name":"api"}`))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", w.Code, w.Body.String())
	}
	if crMgr.capturedSpec != (TenantCRSpec{}) {
		t.Errorf("EnsureTenant ran with %+v on an unknown cell", crMgr.capturedSpec)
	}
}
