// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Fake StatusBuildStore ---

type fakeStatusBuildStore struct {
	builds []StatusBuild
	err    error
}

func (f *fakeStatusBuildStore) ListByProject(_ context.Context, _, _ string, _, _ int) ([]StatusBuild, error) {
	return f.builds, f.err
}

// --- Fake StatusReleaseStore ---

type fakeStatusReleaseStore struct {
	release *StatusRelease
	err     error
}

func (f *fakeStatusReleaseStore) GetActiveByEnv(_ context.Context, _, _, _ string) (*StatusRelease, error) {
	return f.release, f.err
}

// --- Fake StatusProjectStore ---

type fakeStatusProjectStore struct {
	project *ProjectResult
}

func (f *fakeStatusProjectStore) GetByName(_ context.Context, _, name string) (*ProjectResult, error) {
	if f.project != nil && f.project.Name == name {
		return f.project, nil
	}
	return nil, nil
}

// --- Tests ---

func TestStatus_Get_HappyPath(t *testing.T) {
	now := time.Now()
	projects := &fakeStatusProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api"},
	}
	builds := &fakeStatusBuildStore{
		builds: []StatusBuild{
			{
				ID:          "bld-1",
				Environment: "production",
				Status:      "SUCCEEDED",
				CommitSHA:   "abc123",
				CreatedAt:   now,
			},
		},
	}
	releases := &fakeStatusReleaseStore{
		release: &StatusRelease{
			ID:        "rel-1",
			Status:    "ACTIVE",
			GitCommit: "abc123",
			CreatedAt: now,
			// J-25 canonical (sponsor walkthrough 2026-06-12): EntryApp is
			// the first server/frontend app in the rendered apps spec —
			// resolved by StatusReleaseStoreAdapter via release.Apps json
			// parse. status.url uses it to emit the same hostname formula
			// as the IngressRoute (pkg/naming.AppHostname single source of
			// truth).
			EntryApp: "web",
		},
	}
	// publicDomain non-empty triggers canonical URL emission per ADR D3.
	// When empty, the dashboard renders "URL pending" badge — exercised by
	// TestStatus_Get_NoBuildsNoRelease / scaling tests below that pass "".
	h := NewStatusHandler(projects, builds, releases, "dev.loopzero.example", "dev.loopzero.example", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Project != "my-api" {
		t.Fatalf("want project my-api, got %s", resp.Project)
	}
	if resp.SyncStatus != "synced" {
		t.Fatalf("want sync_status synced, got %s", resp.SyncStatus)
	}
	if resp.LastBuild == nil {
		t.Fatal("want last_build to be set")
	}
	if resp.ActiveRelease == nil {
		t.Fatal("want active_release to be set")
	}
	// Canonical formula per pkg/naming.AppHostname (single source of truth):
	// `<tenantID>-<projectID>-<appName>-<envName>.<publicDomain>` — matches
	// the IngressRoute Host emitted by appHostname() so Traefik routes
	// the dashboard "Visit" click cleanly (J-25 sponsor walkthrough fix
	// 2026-06-12 catches #21+#22).
	if resp.URL != "https://tn-test-123-my-api-web-production.dev.loopzero.example" {
		t.Fatalf("want canonical URL <tenant>-<project>-<entryApp>-<env>.<publicDomain>, got %s", resp.URL)
	}
}

func TestStatus_Get_ProjectNotFound(t *testing.T) {
	h := NewStatusHandler(
		&fakeStatusProjectStore{},
		&fakeStatusBuildStore{},
		&fakeStatusReleaseStore{},
		"dev.loopzero.example",
		"",
		"",
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nonexistent/status?env=production", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStatus_Get_MissingEnv_DefaultsToProduction(t *testing.T) {
	// 2026-06-05 J-09 dogfood Path B: backend defaults the env query
	// parameter to "production" when absent (dashboard hook drops it
	// on the project overview to keep URLs canonical). Callers
	// targeting preview/staging still pass ?env=<name> explicitly.
	h := NewStatusHandler(
		&fakeStatusProjectStore{project: &ProjectResult{ID: "p1", Name: "my-api"}},
		&fakeStatusBuildStore{},
		&fakeStatusReleaseStore{},
		"dev.loopzero.example",
		"",
		"",
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status", nil) // no ?env=
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (env defaults to production), got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"environment":"production"`) {
		t.Fatalf("expected environment=production in response, got: %s", w.Body.String())
	}
}

func TestStatus_Get_Unauthenticated(t *testing.T) {
	h := NewStatusHandler(
		&fakeStatusProjectStore{},
		&fakeStatusBuildStore{},
		&fakeStatusReleaseStore{},
		"dev.loopzero.example",
		"",
		"",
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStatus_Get_ScalingScaleToZero(t *testing.T) {
	now := time.Now()
	projects := &fakeStatusProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api"},
	}
	builds := &fakeStatusBuildStore{
		builds: []StatusBuild{
			{
				ID:          "bld-1",
				Environment: "production",
				Status:      "SUCCEEDED",
				CreatedAt:   now,
				Scaling:     &StatusBuildScaling{MinReplicas: 0, MaxReplicas: 5},
			},
		},
	}
	releases := &fakeStatusReleaseStore{
		release: &StatusRelease{
			ID: "rel-1", Status: "ACTIVE", GitCommit: "abc", CreatedAt: now,
		},
	}
	h := NewStatusHandler(projects, builds, releases, "dev.loopzero.example", "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Scaling == nil {
		t.Fatal("expected scaling field for scale-to-zero build")
	}
	if resp.Scaling.Min != 0 {
		t.Errorf("want min=0, got %d", resp.Scaling.Min)
	}
	if resp.Scaling.Max != 5 {
		t.Errorf("want max=5, got %d", resp.Scaling.Max)
	}
	if !resp.Scaling.ScaledToZero {
		t.Error("want scaled_to_zero=true when min=0")
	}
	if resp.Scaling.Replicas != 0 {
		t.Errorf("want replicas=0 for scale-to-zero, got %d", resp.Scaling.Replicas)
	}
}

func TestStatus_Get_ScalingHPA(t *testing.T) {
	now := time.Now()
	projects := &fakeStatusProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api"},
	}
	builds := &fakeStatusBuildStore{
		builds: []StatusBuild{
			{
				ID:          "bld-1",
				Environment: "production",
				Status:      "SUCCEEDED",
				CreatedAt:   now,
				Scaling:     &StatusBuildScaling{MinReplicas: 2, MaxReplicas: 10},
			},
		},
	}
	releases := &fakeStatusReleaseStore{
		release: &StatusRelease{
			ID: "rel-1", Status: "ACTIVE", GitCommit: "abc", CreatedAt: now,
		},
	}
	h := NewStatusHandler(projects, builds, releases, "dev.loopzero.example", "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Get(w, req)

	var resp statusResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Scaling == nil {
		t.Fatal("expected scaling field for HPA build")
	}
	if resp.Scaling.Min != 2 {
		t.Errorf("want min=2, got %d", resp.Scaling.Min)
	}
	if resp.Scaling.Max != 10 {
		t.Errorf("want max=10, got %d", resp.Scaling.Max)
	}
	if resp.Scaling.ScaledToZero {
		t.Error("want scaled_to_zero=false for min >= 1")
	}
	if resp.Scaling.Replicas != 2 {
		t.Errorf("want replicas=2 (minReplicas) for HPA, got %d", resp.Scaling.Replicas)
	}
}

func TestStatus_Get_NoScaling(t *testing.T) {
	now := time.Now()
	projects := &fakeStatusProjectStore{
		project: &ProjectResult{ID: "p1", Name: "my-api"},
	}
	builds := &fakeStatusBuildStore{
		builds: []StatusBuild{
			{
				ID: "bld-1", Environment: "production", Status: "SUCCEEDED", CreatedAt: now,
				// Scaling is nil — no autoscaling configured
			},
		},
	}
	h := NewStatusHandler(projects, builds, &fakeStatusReleaseStore{}, "dev.loopzero.example", "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Get(w, req)

	var resp statusResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Scaling != nil {
		t.Errorf("expected no scaling field when autoscaling is not configured, got %+v", resp.Scaling)
	}
}

func TestStatus_Get_NoBuildsNoRelease(t *testing.T) {
	h := NewStatusHandler(
		&fakeStatusProjectStore{project: &ProjectResult{ID: "p1", Name: "my-api"}},
		&fakeStatusBuildStore{builds: []StatusBuild{}},
		&fakeStatusReleaseStore{release: nil},
		"dev.loopzero.example",
		"",
		"",
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.LastBuild != nil {
		t.Fatal("want last_build nil for no builds")
	}
	if resp.ActiveRelease != nil {
		t.Fatal("want active_release nil for no release")
	}
	if resp.SyncStatus != "unknown" {
		t.Fatalf("want sync_status unknown, got %s", resp.SyncStatus)
	}
}
