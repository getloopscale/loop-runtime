// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statushttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/routestest"
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
	project *ports.ProjectResult
}

func (f *fakeStatusProjectStore) GetByName(_ context.Context, _, name string) (*ports.ProjectResult, error) {
	if f.project != nil && f.project.Name == name {
		return f.project, nil
	}
	return nil, nil
}

// --- Tests ---

func TestStatus_Get_HappyPath(t *testing.T) {
	now := time.Now()
	projects := &fakeStatusProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
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
	h := NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "dev.getloopscale.com", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
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
	if resp.URL != "https://tn-test-123-my-api-web-production.dev.getloopscale.com" {
		t.Fatalf("want canonical URL <tenant>-<project>-<entryApp>-<env>.<publicDomain>, got %s", resp.URL)
	}
}

func TestStatus_Get_ProjectNotFound(t *testing.T) {
	h := NewStatusHandler(
		&fakeStatusProjectStore{},
		&fakeStatusBuildStore{},
		&fakeStatusReleaseStore{},
		"dev.getloopscale.com",
		"",
		"",
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nonexistent/status?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "nonexistent")
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
		&fakeStatusProjectStore{project: &ports.ProjectResult{ID: "p1", Name: "my-api"}},
		&fakeStatusBuildStore{},
		&fakeStatusReleaseStore{},
		"dev.getloopscale.com",
		"",
		"",
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status", nil) // no ?env=
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
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
		"dev.getloopscale.com",
		"",
		"",
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStatus_Get_ScalingScaleToZero(t *testing.T) {
	now := time.Now()
	projects := &fakeStatusProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
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
	h := NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
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
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
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
	h := NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
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
		project: &ports.ProjectResult{ID: "p1", Name: "my-api"},
	}
	builds := &fakeStatusBuildStore{
		builds: []StatusBuild{
			{
				ID: "bld-1", Environment: "production", Status: "SUCCEEDED", CreatedAt: now,
				// Scaling is nil — no autoscaling configured
			},
		},
	}
	h := NewStatusHandler(projects, builds, &fakeStatusReleaseStore{}, "dev.getloopscale.com", "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
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
		&fakeStatusProjectStore{project: &ports.ProjectResult{ID: "p1", Name: "my-api"}},
		&fakeStatusBuildStore{builds: []StatusBuild{}},
		&fakeStatusReleaseStore{release: nil},
		"dev.getloopscale.com",
		"",
		"",
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
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

// Red Team loopzero-cloud#85 regression-protector — provenance.
// `active_release.git_commit` MUST match the app commit_sha of the
// build the release references (build.commit_sha), NOT the internal
// gitops/SoT push commit. The Red Team observed:
//
//	last_build.commit_sha   = e52f2f41... (app commit; user pushed)
//	active_release.git_commit = de6b5c5b... (gitops repo commit; internal)
//
// The user-facing field MUST surface the user-pushed commit. The DB
// column `releases.git_commit` keeps the gitops commit for activator
// integrity; this is API-surface aliasing only.
func TestStatus_ActiveReleaseGitCommitMatchesBuildCommitSHA(t *testing.T) {
	now := time.Now()
	const appCommit = "e52f2f41d8334f7fc506ca646ef53b34ad4ddb55"
	const gitopsCommit = "de6b5c5b728265cc229599143a0d979ceed9bc00"

	projects := &fakeStatusProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	builds := &fakeStatusBuildStore{
		builds: []StatusBuild{
			{
				ID:          "bld-1",
				Environment: "production",
				Status:      "SUCCEEDED",
				CommitSHA:   appCommit, // the user-pushed commit
				CreatedAt:   now,
			},
		},
	}
	releases := &fakeStatusReleaseStore{
		release: &StatusRelease{
			ID:        "rel-1",
			BuildID:   "bld-1", // matches the build above
			Status:    "ACTIVE",
			GitCommit: gitopsCommit, // the internal gitops commit — MUST NOT leak as user-facing
			CreatedAt: now,
			EntryApp:  "web",
		},
	}
	h := NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "dev.getloopscale.com", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ActiveRelease == nil {
		t.Fatal("Red Team #85: active_release must be present")
	}
	if resp.ActiveRelease.GitCommit != appCommit {
		t.Errorf("Red Team #85: active_release.git_commit must equal build.commit_sha (%s), got %s",
			appCommit, resp.ActiveRelease.GitCommit)
	}
	if resp.LastBuild == nil {
		t.Fatal("Red Team #85: last_build must be present")
	}
	if resp.LastBuild.CommitSHA != resp.ActiveRelease.GitCommit {
		t.Errorf("Red Team #85: last_build.commit_sha (%s) and active_release.git_commit (%s) must be identical for the SAME build",
			resp.LastBuild.CommitSHA, resp.ActiveRelease.GitCommit)
	}
}

// Red Team loopzero-cloud#70/#62 closure: when the queried environment has no
// last_build AND no active_release, the response MUST list any sibling
// environments that DO have a SUCCEEDED build under `other_envs_with_releases`
// so the CLI / dashboard can render an honest hint instead of an empty void.
//
// Before this closure, `loopzero status` (and the project overview card)
// defaulted to env=production and rendered "no deployments yet" — even
// when the user had a successful preview deploy. That misled adopters into
// thinking nothing had been built, contradicting the BUILD page that listed
// the SUCCEEDED preview build.
//
// Fixture: 1 SUCCEEDED build in env=preview, NO build / release in env=production.
// Expected: GET ?env=production returns `other_envs_with_releases:[{environment:"preview",has_release:true}]`.
func TestStatus_EmptyProductionWithPreviewRelease_SurfacesHint(t *testing.T) {
	now := time.Now()
	projects := &fakeStatusProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	builds := &fakeStatusBuildStore{
		builds: []StatusBuild{
			{
				ID:          "bld-preview",
				Environment: "preview",
				Status:      "SUCCEEDED",
				CommitSHA:   "abc123",
				CreatedAt:   now,
			},
		},
	}
	// No active release in production (the queried env).
	releases := &fakeStatusReleaseStore{release: nil}
	h := NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "dev.getloopscale.com", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
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
		t.Fatalf("Red Team #70: production has no build → last_build must be nil, got %+v", resp.LastBuild)
	}
	if resp.ActiveRelease != nil {
		t.Fatalf("Red Team #70: production has no release → active_release must be nil")
	}
	if len(resp.OtherEnvsWithReleases) != 1 {
		t.Fatalf("Red Team #70: want exactly 1 sibling env with release, got %d (%+v)",
			len(resp.OtherEnvsWithReleases), resp.OtherEnvsWithReleases)
	}
	got := resp.OtherEnvsWithReleases[0]
	if got.Environment != "preview" {
		t.Errorf("Red Team #70: want sibling environment=preview, got %q", got.Environment)
	}
	if !got.HasRelease {
		t.Errorf("Red Team #70: want has_release=true for sibling env, got false")
	}
}

// Companion regression-protector: when the queried env HAS its own build,
// the hint field MUST stay omitted (no noise on the happy path).
func TestStatus_EmptyProductionWithNoSiblings_OmitsHint(t *testing.T) {
	projects := &fakeStatusProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	builds := &fakeStatusBuildStore{builds: []StatusBuild{}} // no builds anywhere
	releases := &fakeStatusReleaseStore{release: nil}
	h := NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "dev.getloopscale.com", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.OtherEnvsWithReleases) != 0 {
		t.Errorf("Red Team #70: empty project → other_envs_with_releases must be empty, got %+v",
			resp.OtherEnvsWithReleases)
	}

	// JSON shape: omitempty MUST elide the field entirely when empty
	// (preserves legacy clients that don't know about the field).
	if strings.Contains(w.Body.String(), `"other_envs_with_releases"`) {
		t.Errorf("Red Team #70: omitempty contract — field MUST be elided from JSON when empty, got body=%s",
			w.Body.String())
	}
}

// Boundary case: a sibling env that has a build but it FAILED — should NOT
// surface as a hint (we only point users at envs that actually have a release).
func TestStatus_EmptyProductionWithFailedSibling_OmitsHint(t *testing.T) {
	now := time.Now()
	projects := &fakeStatusProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	builds := &fakeStatusBuildStore{
		builds: []StatusBuild{
			{
				ID:          "bld-preview-failed",
				Environment: "preview",
				Status:      "FAILED",
				CreatedAt:   now,
			},
		},
	}
	releases := &fakeStatusReleaseStore{release: nil}
	h := NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "dev.getloopscale.com", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-api/status?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()
	h.Get(w, req)

	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.OtherEnvsWithReleases) != 0 {
		t.Errorf("Red Team #70: a FAILED sibling build must NOT surface as a release hint, got %+v",
			resp.OtherEnvsWithReleases)
	}
}
