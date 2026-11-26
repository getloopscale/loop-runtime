// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package buildshttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/pagination"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// --- Fake BuildListStore ---

type fakeBuildListStore struct {
	builds    []BuildListItem
	total     int
	err       error
	buildEnvs []string // T2.2: ordered envs for ListEnvsForProject
	envsErr   error
}

func (f *fakeBuildListStore) ListByProjectAndEnv(_ context.Context, _, _, _ string, _, _ int) ([]BuildListItem, int, error) {
	return f.builds, f.total, f.err
}

// T2.2 + ADR D3: distinct envs the project has built into, ordered by
// most-recently-used. Used to feed the dashboard /deployments env filter
// chips (replaces the hardcoded production/staging/preview/all literals).
func (f *fakeBuildListStore) ListEnvsForProject(_ context.Context, _, _ string) ([]string, error) {
	return f.buildEnvs, f.envsErr
}

// --- Tests ---

func TestBuildList_HappyPath(t *testing.T) {
	now := time.Now()
	store := &fakeBuildListStore{
		builds: []BuildListItem{
			{
				ID:          "bld-1",
				ProjectID:   "my-api",
				Environment: "production",
				Status:      "SUCCEEDED",
				CommitSHA:   "abc123",
				CreatedAt:   now,
			},
			{
				ID:          "bld-2",
				ProjectID:   "my-api",
				Environment: "production",
				Status:      "FAILED",
				CommitSHA:   "def456",
				CreatedAt:   now.Add(-1 * time.Hour),
			},
		},
		total: 2,
	}
	h := NewBuildListHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp buildListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Builds) != 2 {
		t.Fatalf("want 2 builds, got %d", len(resp.Builds))
	}
	if resp.Total != 2 {
		t.Fatalf("want total=2, got %d", resp.Total)
	}
}

func TestBuildList_Empty(t *testing.T) {
	store := &fakeBuildListStore{
		builds: []BuildListItem{},
		total:  0,
	}
	h := NewBuildListHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp buildListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Builds) != 0 {
		t.Fatalf("want 0 builds, got %d", len(resp.Builds))
	}
}

func TestBuildList_Unauthenticated(t *testing.T) {
	h := NewBuildListHandler(&fakeBuildListStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds", nil)
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildList_WithPagination(t *testing.T) {
	store := &fakeBuildListStore{
		builds: []BuildListItem{},
		total:  50,
	}
	h := NewBuildListHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds?limit=10&offset=20", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildList_WithTimestamps(t *testing.T) {
	now := time.Now()
	started := now.Add(-5 * time.Minute)
	finished := now
	duration := 300
	store := &fakeBuildListStore{
		builds: []BuildListItem{
			{
				ID:              "bld-1",
				ProjectID:       "my-api",
				Environment:     "production",
				Status:          "SUCCEEDED",
				CommitSHA:       "abc123",
				CreatedAt:       now.Add(-10 * time.Minute),
				StartedAt:       &started,
				FinishedAt:      &finished,
				DurationSeconds: &duration,
			},
		},
		total: 1,
	}
	h := NewBuildListHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp buildListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Builds[0].StartedAt == "" {
		t.Fatal("want started_at to be set")
	}
	if resp.Builds[0].FinishedAt == "" {
		t.Fatal("want finished_at to be set")
	}
}

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		input      string
		defaultVal int
		want       int
	}{
		{"", 20, 20},
		{"10", 20, 10},
		{"abc", 20, 20},
		{"0", 20, 0},
	}

	for _, tt := range tests {
		got := pagination.ParseIntParam(tt.input, tt.defaultVal)
		if got != tt.want {
			t.Errorf("pagination.ParseIntParam(%q, %d) = %d, want %d", tt.input, tt.defaultVal, got, tt.want)
		}
	}
}

// --- T2.2 GET /api/v1/projects/{name}/build-envs tests (ADR D3 + EC-2) ---

func TestBuildEnvs_ReturnsDistinct(t *testing.T) {
	// 3 builds in env=production + 1 in env=staging should distinct to 2 envs.
	store := &fakeBuildListStore{
		buildEnvs: []string{"production", "staging"},
	}
	projectStore := &fakeProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewBuildListHandlerWithGate(store, projectStore)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/build-envs", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.BuildEnvs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Envs  []string `json:"envs"`
		Total int      `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Envs) != 2 || resp.Envs[0] != "production" || resp.Envs[1] != "staging" {
		t.Errorf("envs mismatch: %+v", resp.Envs)
	}
	if resp.Total != 2 {
		t.Errorf("total mismatch: got %d want 2", resp.Total)
	}
}

func TestBuildEnvs_OrdersByRecency(t *testing.T) {
	// Store returns envs in recency order — handler echoes that order.
	store := &fakeBuildListStore{
		buildEnvs: []string{"qa", "staging", "production"},
	}
	projectStore := &fakeProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewBuildListHandlerWithGate(store, projectStore)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/build-envs", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.BuildEnvs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Envs []string `json:"envs"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Envs) != 3 || resp.Envs[0] != "qa" || resp.Envs[2] != "production" {
		t.Errorf("recency order broken: %+v", resp.Envs)
	}
}

func TestBuildEnvs_EmptyForColdProject(t *testing.T) {
	// Cold project (no builds) returns empty envs slice, not nil.
	store := &fakeBuildListStore{
		buildEnvs: nil,
	}
	projectStore := &fakeProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewBuildListHandlerWithGate(store, projectStore)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/build-envs", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.BuildEnvs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Envs  []string `json:"envs"`
		Total int      `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Envs == nil {
		t.Errorf("envs must be empty slice (not nil) per Rule 5 contract")
	}
	if len(resp.Envs) != 0 || resp.Total != 0 {
		t.Errorf("cold project should have empty envs+total=0, got %+v / total=%d", resp.Envs, resp.Total)
	}
}

// EC-2 SEPA-CRITICAL: cross-tenant project requests return 404
// PROJECT_NOT_FOUND (NOT 403) — same response shape as "project does
// not exist anywhere" to prevent existence leak. Mirrors the M2 pattern
// from api/cmd/env_lookups_adapter.go.
func TestBuildEnvs_CrossTenantBlockedWith404(t *testing.T) {
	store := &fakeBuildListStore{}
	// ProjectStore returns nil for the requested name (gate result: no
	// project under THIS tenant with that name). The CALLER's tenant_id
	// in the auth context does NOT match the project's tenant. The 404
	// shape is identical whether the project truly doesn't exist OR
	// belongs to another tenant — existence-leak prevention per EC-1
	// SEPA-CRITICAL pattern.
	projectStore := &fakeProjectStore{project: nil}
	h := NewBuildListHandlerWithGate(store, projectStore)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/other-tenant-project/build-envs", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "other-tenant-project")
	w := httptest.NewRecorder()

	h.BuildEnvs(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("EC-2 SEPA-CRITICAL: want 404 (NOT 403 — existence leak), got %d body=%s", w.Code, w.Body.String())
	}
	// Body shape is RFC 9457 problem-details (`code` is top-level, NOT
	// nested under `error`). Match M2's adapter contract.
	var errResp struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errResp.Code != "PROJECT_NOT_FOUND" {
		t.Errorf("EC-2: error code should be PROJECT_NOT_FOUND, got %q (body=%s)", errResp.Code, w.Body.String())
	}
}

// Red Team loopruntime-cloud#92 regression-protector (2026-06-27): the plain
// /api/v1/projects/{name}/builds endpoint (not /build-envs) MUST also
// reject non-existent projects with 404 PROJECT_NOT_FOUND. Without this,
// the dashboard's Deployments page rendered functional UI for a deleted
// project (the BuildEnvs gate was the only one wired).
func TestBuildList_NonExistentProject_Returns404(t *testing.T) {
	store := &fakeBuildListStore{}
	projectStore := &fakeProjectStore{project: nil} // project missing
	h := NewBuildListHandlerWithGate(store, projectStore)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nonexistent-zzz/builds", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "nonexistent-zzz")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Red Team #92: want 404 PROJECT_NOT_FOUND for missing project, got %d body=%s", w.Code, w.Body.String())
	}
	var errResp struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errResp.Code != "PROJECT_NOT_FOUND" {
		t.Errorf("Red Team #92: error code should be PROJECT_NOT_FOUND, got %q (body=%s)", errResp.Code, w.Body.String())
	}
}

func TestBuildList_ExistingProject_AllowsThrough(t *testing.T) {
	store := &fakeBuildListStore{}
	projectStore := &fakeProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewBuildListHandlerWithGate(store, projectStore)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/builds", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 OK for existing project, got %d body=%s", w.Code, w.Body.String())
	}
}

// Backwards-compat: NewBuildListHandler (without gate) still works for
// legacy/test code paths.
func TestBuildList_NilProjectStore_NoGate(t *testing.T) {
	store := &fakeBuildListStore{}
	h := NewBuildListHandler(store) // no gate

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/whatever/builds", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "whatever")
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (nil projects = no gate), got %d", w.Code)
	}
}
