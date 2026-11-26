// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environmentshttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Environments []EnvironmentEntry `json:"environments"`
		Total        int                `json:"total"`
	}
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "production")
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Delete_HappyPath(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "staging")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Delete_CannotDeleteProduction(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "production")
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- J-10 v1.1 LIVE catch 2026-06-07: KPI enrichment tests ---

type fakeKPILookup struct {
	out map[EnvRef]EnvKPI
	err error
}

func (f *fakeKPILookup) LatestActivePerEnv(_ context.Context, _ string) (map[EnvRef]EnvKPI, error) {
	return f.out, f.err
}

func TestEnvs_List_KPIEnrichment_PopulatesFields(t *testing.T) {
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{
			{Name: "production", Project: "web", Status: "active"},
			{Name: "staging", Project: "web", Status: "active"},
			{Name: "dev", Project: "web", Status: "active"}, // no release yet → fields stay empty
		},
	}
	lookup := &fakeKPILookup{
		out: map[EnvRef]EnvKPI{
			{Project: "web", Name: "production"}: {LastDeploy: "2026-06-07T10:00:00Z", LastDeploySha: "abc1234567"},
			{Project: "web", Name: "staging"}:    {LastDeploy: "2026-06-07T09:30:00Z", LastDeploySha: "def8901234"},
		},
	}
	h := NewEnvironmentHandler(mgr).WithKPILookup(lookup)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Environments []EnvironmentEntry `json:"environments"`
		Total        int                `json:"total"`
	}
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

// Two projects share the environment name production and only one has deployed.
// The other must not borrow its last deploy.
func TestEnvs_List_KPIEnrichment_DoesNotCrossProjects(t *testing.T) {
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{
			{Name: "production", Project: "web", Status: "active"},
			{Name: "production", Project: "worker", Status: "active"},
		},
	}
	lookup := &fakeKPILookup{
		out: map[EnvRef]EnvKPI{
			{Project: "web", Name: "production"}: {LastDeploy: "2026-06-07T10:00:00Z", LastDeploySha: "abc1234567"},
		},
	}
	h := NewEnvironmentHandler(mgr).WithKPILookup(lookup)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)

	var resp struct {
		Environments []EnvironmentEntry `json:"environments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, e := range resp.Environments {
		switch e.Project {
		case "web":
			if e.LastDeploy != "2026-06-07T10:00:00Z" {
				t.Errorf("web/production: last_deploy = %q, want its own deploy", e.LastDeploy)
			}
		case "worker":
			if e.LastDeploy != "" || e.LastDeploySha != "" {
				t.Errorf("worker/production never deployed, got last_deploy=%q sha=%q", e.LastDeploy, e.LastDeploySha)
			}
		}
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (tolerant), got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Environments []EnvironmentEntry `json:"environments"`
		Total        int                `json:"total"`
	}
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
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

func TestEnvs_Create_ExistingTupleIs409(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{addErr: fmt.Errorf("k8s: %w", ports.ErrEnvironmentExists)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(`{"name":"staging","project":"web"}`))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "ENV_ALREADY_EXISTS") {
		t.Fatalf("want 409 ENV_ALREADY_EXISTS, got %d body=%s", w.Code, w.Body.String())
	}
}

// An error whose text happens to contain "already" is not a duplicate: it is
// an unexpected failure and must not be reported as a conflict.
func TestEnvs_Create_ErrorMentioningAlreadyIsNotAConflict(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{addErr: errors.New("context already cancelled")})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments", bytes.NewBufferString(`{"name":"staging","project":"web"}`))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Delete_MissingTupleIs404(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{removeErr: fmt.Errorf("k8s: %w", ports.ErrEnvironmentNotFound)})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging?project=web", nil)
	req = routestest.WithChiParam(req, "name", "staging")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

// fakeActiveReleaseLookup — minimal stub for EC-7 warning emission.
type fakeActiveReleaseLookup struct {
	active bool
	err    error
}

func (f *fakeActiveReleaseLookup) HasActiveRelease(_ context.Context, _, _, _ string) (bool, error) {
	return f.active, f.err
}

func TestEnvs_Create_Returns400WhenProjectMissing(t *testing.T) {
	h := NewEnvironmentHandler(&fakeEnvMgr{})
	body := `{"name":"staging"}` // no project
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments",
		bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing project, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "project") {
		t.Errorf("error message should mention 'project', got %s", w.Body.String())
	}
}

func TestEnvs_Create_Returns404WhenProjectNotInTenant(t *testing.T) {
	pl := &fakeProjectLookup{exists: map[string]bool{}} // empty
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithProjectLookup(pl)

	body := `{"name":"staging","project":"nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments",
		bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for absent project, got %d body=%s", w.Code, w.Body.String())
	}
	if pl.callCount != 1 {
		t.Errorf("want 1 ProjectExistsForTenant call, got %d", pl.callCount)
	}
}

// TestEnvs_Create_RejectsProjectFromOtherTenant — EC-1 SEPA-CRITICAL.
// User of tenant A passes a project slug owned by tenant B → 404 (NOT 201).
// Asserts the validateProject query was scoped to caller's tenant_id.
func TestEnvs_Create_RejectsProjectFromOtherTenant(t *testing.T) {
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
	// routestest.DefaultUserContext() → tenant_id = "tn-test-123"
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
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

func TestEnvs_Create_ResponseIncludesProject(t *testing.T) {
	pl := &fakeProjectLookup{exists: map[string]bool{"tn-test-123::web": true}}
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithProjectLookup(pl)

	body := `{"name":"staging","project":"web"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments",
		bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"project":"web"`) {
		t.Errorf("response must include project field, got %s", w.Body.String())
	}
}

func TestEnvs_Create_DuplicateReturns409(t *testing.T) {
	pl := &fakeProjectLookup{exists: map[string]bool{"tn-test-123::web": true}}
	mgr := &fakeEnvMgr{addErr: ports.ErrEnvironmentExists}
	h := NewEnvironmentHandler(mgr).WithProjectLookup(pl)

	body := `{"name":"staging","project":"web"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments",
		bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("EC-5: duplicate must be 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEnvs_Create_ProductionRejectedCrossProject(t *testing.T) {
	pl := &fakeProjectLookup{exists: map[string]bool{"tn-test-123::web": true}}
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithProjectLookup(pl)

	body := `{"name":"production","project":"web"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/environments",
		bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("EC-8: production reserved cross-project, want 400, got %d body=%s",
			w.Code, w.Body.String())
	}
}

func TestEnvs_Delete_PropagatesProjectQuery(t *testing.T) {
	mgr := &fakeEnvMgr{}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/environments/staging?project=web", nil)
	req = routestest.WithChiParam(req, "name", "staging")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"project":"web"`) {
		t.Errorf("Delete response should echo project, got %s", w.Body.String())
	}
}

func TestEnvs_Delete_WarnsOnActiveRelease(t *testing.T) {
	mgr := &fakeEnvMgr{}
	rl := &fakeActiveReleaseLookup{active: true}
	h := NewEnvironmentHandler(mgr).WithActiveReleaseLookup(rl)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/environments/staging?project=web", nil)
	req = routestest.WithChiParam(req, "name", "staging")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"warnings":`) {
		t.Errorf("EC-7 expected warnings[] array, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `loop-runtime platform status`) {
		t.Errorf("EC-7 warning should suggest 'loop-runtime platform status' check, got %s", w.Body.String())
	}
}

// When the release lookup fails, "no active release" is not known: the
// cleanup warning must still be given rather than silently dropped.
func TestEnvs_Delete_WarnsWhenTheActiveReleaseLookupFails(t *testing.T) {
	rl := &fakeActiveReleaseLookup{err: errors.New("connection refused")}
	h := NewEnvironmentHandler(&fakeEnvMgr{}).WithActiveReleaseLookup(rl)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/environments/staging?project=web", nil)
	req = routestest.WithChiParam(req, "name", "staging")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (the delete itself succeeded), got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "loop-runtime platform status") {
		t.Errorf("a failed release lookup dropped the cleanup warning: %s", w.Body.String())
	}
}

// TestEnvs_Delete_WarningRule3Compliant — T0.1 of plan
// 2026-06-15-code-review-49-findings-remediation (R2-F1 BLOCKER).
// User-facing warning emitted on active-release env delete MUST NOT
// contain forbidden internal terms per Rule 3 (.claude/rules/cli-output.md
// § Forbidden Internal Terms). Regression-defense for the
// "ArgoCD App may persist briefly" string that shipped before T0.1.
func TestEnvs_Delete_WarningRule3Compliant(t *testing.T) {
	mgr := &fakeEnvMgr{}
	rl := &fakeActiveReleaseLookup{active: true}
	h := NewEnvironmentHandler(mgr).WithActiveReleaseLookup(rl)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/environments/staging?project=web", nil)
	req = routestest.WithChiParam(req, "name", "staging")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// Forbidden internal terms per cli-output.md. Case-insensitive
	// because user-facing strings should never reference these regardless
	// of casing. Excludes terms that are acceptable in slog (argocd
	// callback paths, k8s namespace names) — this assertion is scoped
	// to the JSON response body, not to slog.
	forbiddenTerms := []string{
		"ArgoCD", "argocd", "Gitea", "gitea", "MinIO", "minio",
		"CRD", "kubebuilder", "envtest", "Kaniko",
		"pg_advisory", "advisory lock", "GitOps", "gitops-push",
	}
	for _, term := range forbiddenTerms {
		if strings.Contains(body, term) {
			t.Errorf("Rule 3 violation: response body contains forbidden internal term %q; body=%s",
				term, body)
		}
	}

	// Positive assertion — the canonical product-language warning IS present.
	if !strings.Contains(body, "cleanup may take a few seconds") {
		t.Errorf("expected canonical product-language warning ('cleanup may take a few seconds'); got body=%s", body)
	}
}

func TestEnvs_Get_PropagatesProjectQuery(t *testing.T) {
	mgr := &fakeEnvMgr{detail: &EnvironmentDetail{Name: "staging", Status: "active"}}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/environments/staging?project=web", nil)
	req = routestest.WithChiParam(req, "name", "staging")
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestEnvs_List_ReturnsProjectInEachEntry — T4.4. Asserts the
// `project` field is propagated from the EnvironmentEntry struct
// (populated by T2.1 TenantCRManager.ListEnvironments) into the JSON
// response body. Backward-compat: omitempty preserves legacy production
// entries with empty project.
func TestEnvs_List_ReturnsProjectInEachEntry(t *testing.T) {
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{
			{Name: "production", Status: "active"},              // legacy (no project)
			{Name: "staging", Status: "active", Project: "web"}, // M2 canonical
			{Name: "staging", Status: "active", Project: "api"}, // same name, different project
		},
	}
	h := NewEnvironmentHandler(mgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
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

// Red Team loop-console#48 regression-protector — A01:2021 Broken Access
// Control / CWE-639. The Red Team verbatim reproduction:
//
//	GET /api/v1/environments?project=zzz-does-not-exist-999
//	  -> 200 total=3 (filter SILENTLY IGNORED — bypass)
//
// The canonical fix MUST honor `?project=` server-side:
//   - if the project does NOT belong to the caller's tenant -> 404 PROJECT_NOT_FOUND
//   - if it DOES, filter the response to entries whose Project matches
//     (preserving the legacy production entry with empty Project for backward compat)
//
// The 3 regression-protectors below lock the 3-axis attack surface
// (filter-honored / cross-tenant-rejected / under-filter-allowed) per
// feedback_issue_fix_requires_failing_e2e_test_2026_06_25.

func TestEnvs_List_FilterByProjectHonored(t *testing.T) {
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{
			{Name: "production", Status: "active"}, // legacy — must always surface
			{Name: "staging", Status: "active", Project: "web"},
			{Name: "qa", Status: "active", Project: "api"},
		},
	}
	// The user owns BOTH projects in their tenant — only `web` matches the filter.
	pl := &fakeProjectLookup{exists: map[string]bool{
		"tn-test-123::web": true,
		"tn-test-123::api": true,
	}}
	h := NewEnvironmentHandler(mgr).WithProjectLookup(pl)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments?project=web", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// production (legacy, empty Project) + staging (Project=web) should survive
	if !strings.Contains(body, `"project":"web"`) {
		t.Errorf("Red Team #48: response missing project=web entry, got %s", body)
	}
	if strings.Contains(body, `"project":"api"`) {
		t.Errorf("Red Team #48: project=web filter must EXCLUDE api entries, got %s", body)
	}
}

func TestEnvs_List_FilterByCrossTenantProjectRejected(t *testing.T) {
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{
			{Name: "production", Status: "active"},
			{Name: "staging", Status: "active", Project: "web"},
		},
	}
	// Caller's tenant only owns `web`; `someone-elses-project` belongs to a
	// different tenant (or doesn't exist at all). Must 404 PROJECT_NOT_FOUND
	// — matching the Create handler's EC-1 SEPA-CRITICAL pattern.
	pl := &fakeProjectLookup{exists: map[string]bool{"tn-test-123::web": true}}
	h := NewEnvironmentHandler(mgr).WithProjectLookup(pl)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/environments?project=someone-elses-project", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Red Team #48: cross-tenant filter must 404 PROJECT_NOT_FOUND, got %d body=%s",
			w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PROJECT_NOT_FOUND") {
		t.Errorf("Red Team #48: body must include PROJECT_NOT_FOUND code, got %s", w.Body.String())
	}
}

func TestEnvs_List_NoFilter_LegacyShapeUnchanged(t *testing.T) {
	mgr := &fakeEnvMgr{
		envs: []EnvironmentEntry{
			{Name: "production", Status: "active"},
			{Name: "staging", Status: "active", Project: "web"},
		},
	}
	// Even with projectStore wired, an empty ?project= MUST surface all
	// envs the tenant owns (legacy J-10 dashboard shape). The filter only
	// engages when the query param is non-empty.
	pl := &fakeProjectLookup{exists: map[string]bool{"tn-test-123::web": true}}
	h := NewEnvironmentHandler(mgr).WithProjectLookup(pl)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/environments", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("no-filter must return all 2 envs (legacy shape), got total=%d", resp.Total)
	}
}
