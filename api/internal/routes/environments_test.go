// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Fake EnvironmentManager ---

type fakeEnvMgr struct {
	envs      []EnvironmentEntry
	detail    *EnvironmentDetail
	addErr    error
	removeErr error
	listErr   error
}

func (f *fakeEnvMgr) ListEnvironments(_ context.Context, _ string) ([]EnvironmentEntry, error) {
	return f.envs, f.listErr
}

// M2 v1.2: signature carries `project`. Phase 1 cascade — fake records
// the project arg but ignores it for return-value purposes (test
// fixtures land in Phase 4 when the project arg semantics matter).
func (f *fakeEnvMgr) AddEnvironment(_ context.Context, _, _, _ string) error {
	return f.addErr
}

func (f *fakeEnvMgr) RemoveEnvironment(_ context.Context, _, _, _ string) error {
	return f.removeErr
}

func (f *fakeEnvMgr) GetEnvironmentDetail(_ context.Context, _ string, _ string, envName string) (*EnvironmentDetail, error) {
	if f.detail != nil && f.detail.Name == envName {
		return f.detail, nil
	}
	return nil, nil
}

// --- Tests ---

func TestEnvs_List_HappyPath(t *testing.T) {
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{
			{Name: "production", Status: "active"},
			{Name: "staging", Status: "active"},
		},
	}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct { Environments []EnvironmentEntry `json:"environments"`; Total int `json:"total"` }
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Environments) != 2 {
		t.Fatalf("want 2 environments, got %d", len(resp.Environments))
	}
}

func TestEnvs_List_Unauthenticated(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestEnvs_Create_HappyPath(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	// M2 v1.2: project field is required (T4.1). Stubs the
	// projectStore-bypass path so the handler doesn't try DB lookup.
	body := `{"name":"staging","project":"web"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Create_InvalidName(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	body := `{"name":"BAD NAME"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Create_ReservedName(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	body := `{"name":"kube-system"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Get_Found(t *testing.T) {
	mgr := &fakeEnvMgr{
		detail: &EnvironmentDetail{
			Name:   "production",
			Status: "active",
		},
	}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/production", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "production")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Get_NotFound(t *testing.T) {
	mgr := &fakeEnvMgr{} // nil detail
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments/nonexistent", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Delete_HappyPath(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "staging")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Delete_CannotDeleteProduction(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/production", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "production")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- J-10 v1.1 LIVE catch 2026-06-07: KPI enrichment tests ---

type fakeKPILookup struct {
	out map[string]EnvKPI
	err error
}

func (f *fakeKPILookup) LatestActivePerEnv(_ context.Context, _ string) (map[string]EnvKPI, error) {
	return f.out, f.err
}

func TestEnvs_List_KPIEnrichment_PopulatesFields(t *testing.T) {
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{
			{Name: "production", Status: "active"},
			{Name: "staging", Status: "active"},
			{Name: "dev", Status: "active"}, // no release yet → fields stay empty
		},
	}
	lookup := &fakeKPILookup{
		out: map[string]EnvKPI{
			"production": {LastDeploy: "2026-06-07T10:00:00Z", LastDeploySha: "abc1234567"},
			"staging":    {LastDeploy: "2026-06-07T09:30:00Z", LastDeploySha: "def8901234"},
		},
	}
	h := NewEnvironmentHandler(mgr).WithKPILookup(lookup)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct { Environments []EnvironmentEntry `json:"environments"`; Total int `json:"total"` }
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	envs := resp.Environments
	if len(envs) != 3 {
		t.Fatalf("want 3 envs, got %d", len(envs))
	}

	byName := map[string]EnvironmentEntry{}
	for _, e := range envs {
		byName[e.Name] = e
	}

	if byName["production"].LastDeploy != "2026-06-07T10:00:00Z" {
		t.Errorf("production: want last_deploy 2026-06-07T10:00:00Z, got %q", byName["production"].LastDeploy)
	}
	if byName["production"].LastDeploySha != "abc1234567" {
		t.Errorf("production: want sha abc1234567, got %q", byName["production"].LastDeploySha)
	}
	if byName["staging"].LastDeploy != "2026-06-07T09:30:00Z" {
		t.Errorf("staging: want last_deploy 2026-06-07T09:30:00Z, got %q", byName["staging"].LastDeploy)
	}
	if byName["dev"].LastDeploy != "" || byName["dev"].LastDeploySha != "" {
		t.Errorf("dev: should have empty KPI (no ACTIVE release), got last_deploy=%q sha=%q",
			byName["dev"].LastDeploy, byName["dev"].LastDeploySha)
	}
}

func TestEnvs_List_KPIEnrichment_LookupErrorIsTolerant(t *testing.T) {
	// When the lookup errors, the list MUST still succeed — KPI is
	// best-effort, the page rendering must not block on it.
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{{Name: "production", Status: "active"}},
	}
	lookup := &fakeKPILookup{err: errKPIBoom}
	h := NewEnvironmentHandler(mgr).WithKPILookup(lookup)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (tolerant), got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct { Environments []EnvironmentEntry `json:"environments"`; Total int `json:"total"` }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Environments) != 1 {
		t.Fatalf("want 1 env, got %d", len(resp.Environments))
	}
	if resp.Environments[0].LastDeploy != "" {
		t.Errorf("expected empty KPI on lookup error; got %q", resp.Environments[0].LastDeploy)
	}
}

func TestEnvs_List_NoKPILookup_BackwardCompatible(t *testing.T) {
	// When no lookup is wired, the response shape is the same as before
	// (no KPI fields) — preserves backward compatibility for any client
	// or test that doesn't expect them.
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{{Name: "production", Status: "active"}},
	}
	h := NewEnvironmentHandler(mgr) // no WithKPILookup

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "last_deploy") {
		t.Errorf("response should NOT contain last_deploy when no lookup wired; got: %s", body)
	}
}

var errKPIBoom = httpTestError("boom")

type httpTestError string

func (e httpTestError) Error() string { return string(e) }

// =============================================================================
// M2 v1.2 — T4.1+T4.2+T4.3 API contract tests
// Plan: 2026-06-07-runtime-envs-per-project-m2 v1.2
// =============================================================================

// fakeProjectLookup — captures the (tenant_id, name) tuple so the EC-1
// security gate can be asserted. The `exists` map keys are
// "<tenant>::<project>" so the cross-tenant scenario is unambiguous.
type fakeProjectLookup struct {
	exists      map[string]bool
	gotTenantID string
	gotProject  string
	callCount   int
	err         error
}

func (f *fakeProjectLookup) ProjectExistsForTenant(_ context.Context, tenantID, projectName string) (bool, error) {
	f.callCount++
	f.gotTenantID = tenantID
	f.gotProject = projectName
	if f.err != nil {
		return false, f.err
	}
	return f.exists[tenantID+"::"+projectName], nil
}

// fakeActiveReleaseLookup — minimal stub for EC-7 warning emission.
type fakeActiveReleaseLookup struct{ active bool }

func (f *fakeActiveReleaseLookup) HasActiveRelease(_ context.Context, _, _, _ string) (bool, error) {
	return f.active, nil
}

func TestEnvs_Create_Returns400WhenProjectMissing_M2(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})
	body := `{"name":"staging"}` // no project
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments",
		bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing project, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "project") {
		t.Errorf("error message should mention 'project', got %s", w.Body.String())
	}
}

func TestEnvs_Create_Returns404WhenProjectNotInTenant_M2(t *testing.T) {
	pl := &fakeProjectLookup{exists: map[string]bool{}} // empty
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithProjectLookup(pl)

	body := `{"name":"staging","project":"nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments",
		bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for absent project, got %d body=%s", w.Code, w.Body.String())
	}
	if pl.callCount != 1 {
		t.Errorf("want 1 ProjectExistsForTenant call, got %d", pl.callCount)
	}
}

// TestEnvs_Create_RejectsProjectFromOtherTenant_M2 — EC-1 SEPA-CRITICAL.
// User of tenant A passes a project slug owned by tenant B → 404 (NOT 201).
// Asserts the validateProject query was scoped to caller's tenant_id.
func TestEnvs_Create_RejectsProjectFromOtherTenant_M2(t *testing.T) {
	pl := &fakeProjectLookup{
		exists: map[string]bool{
			"tn-other::web": true, // owned by tenant B
			// "tn-test-123::web" → absent → 404 for tenant A caller
		},
	}
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithProjectLookup(pl)

	body := `{"name":"staging","project":"web"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments",
		bytes.NewBufferString(body))
	// defaultUserContext() → tenant_id = "tn-test-123"
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("EC-1 security gate broken — expected 404 for cross-tenant project, got %d body=%s",
			w.Code, w.Body.String())
	}
	if pl.gotTenantID != "tn-test-123" {
		t.Errorf("EC-1: validateProject query MUST scope by caller tenant_id, got %q", pl.gotTenantID)
	}
	if pl.gotProject != "web" {
		t.Errorf("validateProject got project=%q, want %q", pl.gotProject, "web")
	}
}

func TestEnvs_Create_ResponseIncludesProject_M2(t *testing.T) {
	pl := &fakeProjectLookup{exists: map[string]bool{"tn-test-123::web": true}}
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithProjectLookup(pl)

	body := `{"name":"staging","project":"web"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments",
		bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"project":"web"`) {
		t.Errorf("response must include project field, got %s", w.Body.String())
	}
}

func TestEnvs_Create_DuplicateReturns409_M2(t *testing.T) {
	pl := &fakeProjectLookup{exists: map[string]bool{"tn-test-123::web": true}}
	mgr := &fakeEnvMgr{addErr: errors.New("environment already exists for tenant")}
	h := NewEnvironmentHandler(mgr).WithProjectLookup(pl)

	body := `{"name":"staging","project":"web"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments",
		bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("EC-5: duplicate must be 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Create_ProductionRejectedCrossProject_M2(t *testing.T) {
	pl := &fakeProjectLookup{exists: map[string]bool{"tn-test-123::web": true}}
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithProjectLookup(pl)

	body := `{"name":"production","project":"web"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments",
		bytes.NewBufferString(body))
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("EC-8: production reserved cross-project, want 400, got %d body=%s",
			w.Code, w.Body.String())
	}
}

func TestEnvs_Delete_PropagatesProjectQuery_M2(t *testing.T) {
	mgr := &fakeEnvMgr{}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/environments/staging?project=web", nil)
	req = withChiParam(req, "name", "staging")
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"project":"web"`) {
		t.Errorf("Delete response should echo project, got %s", w.Body.String())
	}
}

func TestEnvs_Delete_WarnsOnActiveRelease_M2(t *testing.T) {
	mgr := &fakeEnvMgr{}
	rl := &fakeActiveReleaseLookup{active: true}
	h := NewEnvironmentHandler(mgr).WithActiveReleaseLookup(rl)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/environments/staging?project=web", nil)
	req = withChiParam(req, "name", "staging")
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"warnings":`) {
		t.Errorf("EC-7 expected warnings[] array, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `loopzero platform status`) {
		t.Errorf("EC-7 warning should suggest 'loopzero platform status' check, got %s", w.Body.String())
	}
}

func TestEnvs_Get_PropagatesProjectQuery_M2(t *testing.T) {
	mgr := &fakeEnvMgr{detail: &EnvironmentDetail{Name: "staging", Status: "active"}}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/environments/staging?project=web", nil)
	req = withChiParam(req, "name", "staging")
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}


// TestEnvs_List_ReturnsProjectInEachEntry_M2 — T4.4. Asserts the
// `project` field is propagated from the EnvironmentEntry struct
// (populated by T2.1 TenantCRManager.ListEnvironments) into the JSON
// response body. Backward-compat: omitempty preserves legacy production
// entries with empty project.
func TestEnvs_List_ReturnsProjectInEachEntry_M2(t *testing.T) {
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{
			{Name: "production", Status: "active"},                   // legacy (no project)
			{Name: "staging", Status: "active", Project: "web"},      // M2 canonical
			{Name: "staging", Status: "active", Project: "api"},      // same name, different project
		},
	}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// At least one entry must have project=web
	if !strings.Contains(body, `"project":"web"`) {
		t.Errorf("response missing project=web entry, got %s", body)
	}
	// At least one entry must have project=api
	if !strings.Contains(body, `"project":"api"`) {
		t.Errorf("response missing project=api entry, got %s", body)
	}
}
