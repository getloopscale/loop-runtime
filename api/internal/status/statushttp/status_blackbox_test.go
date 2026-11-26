// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statushttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth/authtest"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/status/statushttp"

	"github.com/go-chi/chi/v5"
)

// fakeStatusProjectStore implements routes.StatusProjectStore for testing.
type fakeStatusProjectStore struct {
	project *ports.ProjectResult
	err     error
}

func (f *fakeStatusProjectStore) GetByName(ctx context.Context, tenantID, name string) (*ports.ProjectResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.project, nil
}

// fakeStatusBuildStore implements routes.StatusBuildStore for testing.
type fakeStatusBuildStore struct {
	builds []statushttp.StatusBuild
	err    error
}

func (f *fakeStatusBuildStore) ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]statushttp.StatusBuild, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.builds, nil
}

// fakeStatusReleaseStore implements routes.StatusReleaseStore for testing.
type fakeStatusReleaseStore struct {
	release *statushttp.StatusRelease
	err     error
}

func (f *fakeStatusReleaseStore) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*statushttp.StatusRelease, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.release, nil
}

// newStatusRequest creates a GET request with chi URL params and auth context.
func newStatusRequest(name, env string, authenticated bool) *http.Request {
	url := "/api/v1/projects/" + name + "/status"
	if env != "" {
		url += "?env=" + env
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)

	// Set chi URL params via a chi RouteContext
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	if authenticated {
		req = req.WithContext(authtest.Context("github-lucas", "acme", "owner"))
		// Re-add chi route context after auth context override
		rctx2 := chi.NewRouteContext()
		rctx2.URLParams.Add("name", name)
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx2)
		req = req.WithContext(ctx)
	}

	return req
}

func TestStatusRoute(t *testing.T) {
	finishedAt := time.Date(2026, 3, 25, 1, 5, 0, 0, time.UTC)
	duration := 300

	t.Run("GET /api/v1/projects/{name}/status → 200 with valid project and env", func(t *testing.T) {
		// Arrange
		projects := &fakeStatusProjectStore{
			project: &ports.ProjectResult{ID: "proj-123", Name: "my-app", TeamID: "acme"},
		}
		builds := &fakeStatusBuildStore{
			builds: []statushttp.StatusBuild{
				{
					ID:              "build-abc123",
					Environment:     "production",
					Status:          "SUCCEEDED",
					CommitSHA:       "abc123",
					CreatedAt:       time.Date(2026, 3, 25, 1, 0, 0, 0, time.UTC),
					FinishedAt:      &finishedAt,
					DurationSeconds: &duration,
					ErrorMessage:    "",
				},
			},
		}
		releases := &fakeStatusReleaseStore{
			release: &statushttp.StatusRelease{
				ID:        "rel-def456",
				Status:    "ACTIVE",
				GitCommit: "abc123",
				CreatedAt: time.Date(2026, 3, 25, 1, 5, 30, 0, time.UTC),
				// J-25 canonical (sponsor walkthrough 2026-06-12): EntryApp
				// resolved by StatusReleaseStoreAdapter via release.Apps
				// json parse — exercises the canonical URL emit path.
				EntryApp: "web",
			},
		}

		// publicDomain non-empty triggers canonical URL emission per ADR D3.
		handler := statushttp.NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "dev.getloopscale.com", "")
		req := newStatusRequest("my-app", "production", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp["project"] != "my-app" {
			t.Errorf("expected project 'my-app', got '%v'", resp["project"])
		}
		if resp["environment"] != "production" {
			t.Errorf("expected environment 'production', got '%v'", resp["environment"])
		}
		if resp["sync_status"] != "synced" {
			t.Errorf("expected sync_status 'synced', got '%v'", resp["sync_status"])
		}
		// Canonical formula per pkg/naming.AppHostname:
		// `<tenantID>-<projectID>-<appName>-<envName>.<publicDomain>`
		// (J-25 sponsor walkthrough 2026-06-12 catches #21+#22 — closes the
		// mismatch between status.url and the IngressRoute Host rule).
		if resp["url"] != "https://acme-my-app-web-production.dev.getloopscale.com" {
			t.Errorf("expected canonical url 'https://acme-my-app-web-production.dev.getloopscale.com', got '%v'", resp["url"])
		}

		lastBuild, ok := resp["last_build"].(map[string]interface{})
		if !ok {
			t.Fatal("expected last_build to be an object")
		}
		if lastBuild["id"] != "build-abc123" {
			t.Errorf("expected build id 'build-abc123', got '%v'", lastBuild["id"])
		}
		if lastBuild["status"] != "SUCCEEDED" {
			t.Errorf("expected build status 'SUCCEEDED', got '%v'", lastBuild["status"])
		}
		if lastBuild["duration_seconds"] != float64(300) {
			t.Errorf("expected duration_seconds 300, got '%v'", lastBuild["duration_seconds"])
		}

		activeRelease, ok := resp["active_release"].(map[string]interface{})
		if !ok {
			t.Fatal("expected active_release to be an object")
		}
		if activeRelease["id"] != "rel-def456" {
			t.Errorf("expected release id 'rel-def456', got '%v'", activeRelease["id"])
		}
	})

	t.Run("GET /api/v1/projects/{name}/status → 404 when project not found", func(t *testing.T) {
		// Arrange
		projects := &fakeStatusProjectStore{project: nil}
		builds := &fakeStatusBuildStore{}
		releases := &fakeStatusReleaseStore{}

		handler := statushttp.NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "", "")
		req := newStatusRequest("nonexistent", "production", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["code"] != "PROJECT_NOT_FOUND" {
			t.Errorf("expected error code 'PROJECT_NOT_FOUND', got '%v'", resp["code"])
		}
	})

	t.Run("GET /api/v1/projects/{name}/status → 200 with production fallback when env omitted (J-09 LIVE fix)", func(t *testing.T) {
		// T3.3 of plan 2026-06-11-code-review-remediation v1.2 — closes a
		// pre-existing RED that asserted 400 on missing env. The handler
		// was updated during J-09 dogfood (2026-06-05) to default to
		// the canonical permanent env when env="" so the dashboard's
		// useProjectStatus hook can hit /status without forcing an env
		// selector before the project's environments resolve.
		// Memory: feedback_drift_3layer_canonical_pattern_2026_06_06.
		// Behavioral contract is now "fallback to production, never
		// 400 on omitted env".
		projects := &fakeStatusProjectStore{
			project: &ports.ProjectResult{ID: "proj-123", Name: "my-app", TeamID: "acme"},
		}
		builds := &fakeStatusBuildStore{
			builds: []statushttp.StatusBuild{
				{
					ID:          "build-1",
					Environment: "production",
					Status:      "SUCCEEDED",
					CommitSHA:   "def456",
					CreatedAt:   time.Date(2026, 3, 25, 1, 0, 0, 0, time.UTC),
				},
			},
		}
		releases := &fakeStatusReleaseStore{}

		handler := statushttp.NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "", "")
		req := newStatusRequest("my-app", "", true)
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 with production fallback, got %d: %s",
				rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp["environment"] != "production" {
			t.Errorf("expected env fallback to 'production', got %v", resp["environment"])
		}
	})

	t.Run("GET /api/v1/projects/{name}/status → 200 with null builds and releases when none exist", func(t *testing.T) {
		// Arrange
		projects := &fakeStatusProjectStore{
			project: &ports.ProjectResult{ID: "proj-123", Name: "my-app", TeamID: "acme"},
		}
		builds := &fakeStatusBuildStore{builds: []statushttp.StatusBuild{}}
		releases := &fakeStatusReleaseStore{release: nil}

		handler := statushttp.NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "", "")
		req := newStatusRequest("my-app", "production", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp["project"] != "my-app" {
			t.Errorf("expected project 'my-app', got '%v'", resp["project"])
		}
		if resp["last_build"] != nil {
			t.Errorf("expected last_build to be null, got '%v'", resp["last_build"])
		}
		if resp["active_release"] != nil {
			t.Errorf("expected active_release to be null, got '%v'", resp["active_release"])
		}
		if resp["sync_status"] != "unknown" {
			t.Errorf("expected sync_status 'unknown' when no release, got '%v'", resp["sync_status"])
		}
	})

	t.Run("GET /api/v1/projects/{name}/status → 401 without auth", func(t *testing.T) {
		// Arrange
		projects := &fakeStatusProjectStore{}
		builds := &fakeStatusBuildStore{}
		releases := &fakeStatusReleaseStore{}

		handler := statushttp.NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "", "")
		req := newStatusRequest("my-app", "production", false)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/projects/{name}/status → filters builds by environment", func(t *testing.T) {
		// Arrange — builds for different environments
		projects := &fakeStatusProjectStore{
			project: &ports.ProjectResult{ID: "proj-123", Name: "my-app", TeamID: "acme"},
		}
		builds := &fakeStatusBuildStore{
			builds: []statushttp.StatusBuild{
				{
					ID:          "build-staging",
					Environment: "staging",
					Status:      "SUCCEEDED",
					CommitSHA:   "aaa111",
					CreatedAt:   time.Date(2026, 3, 25, 2, 0, 0, 0, time.UTC),
				},
				{
					ID:          "build-prod",
					Environment: "production",
					Status:      "SUCCEEDED",
					CommitSHA:   "bbb222",
					CreatedAt:   time.Date(2026, 3, 25, 1, 0, 0, 0, time.UTC),
				},
			},
		}
		releases := &fakeStatusReleaseStore{}

		handler := statushttp.NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "", "")
		req := newStatusRequest("my-app", "staging", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		lastBuild, ok := resp["last_build"].(map[string]interface{})
		if !ok {
			t.Fatal("expected last_build to be an object")
		}
		if lastBuild["id"] != "build-staging" {
			t.Errorf("expected build id 'build-staging' (filtered by env), got '%v'", lastBuild["id"])
		}
	})
}

// TestStatusRoute_MetricsRangeURLAndPromQueryKeys verifies T1.1 of plan
// 2026-06-06-dashboard-journey-j18-status-runtime-health v1.1:
//   - statusResponse.metrics_range_url is templated when grafanaBaseURL set
//   - statusResponse.metrics_range_url is empty (honest disabled) when grafanaBaseURL empty
//   - statusResponse.prom_query_keys always present with 8 canonical names in order
func TestStatusRoute_MetricsRangeURLAndPromQueryKeys(t *testing.T) {
	t.Parallel()

	projects := &fakeStatusProjectStore{
		project: &ports.ProjectResult{
			ID:     "proj-123",
			Name:   "sample",
			TeamID: "tn-acme",
		},
	}
	builds := &fakeStatusBuildStore{builds: []statushttp.StatusBuild{}}
	releases := &fakeStatusReleaseStore{}

	t.Run("metrics_range_url is templated when grafanaBaseURL set", func(t *testing.T) {
		handler := statushttp.NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "", "https://grafana.loopruntime.local")
		req := newStatusRequest("sample", "production", true)
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		want := "https://grafana.loopruntime.local/d/loopruntime-project?var-project=sample&var-env=production"
		if got, _ := resp["metrics_range_url"].(string); got != want {
			t.Errorf("metrics_range_url mismatch: got %q, want %q", got, want)
		}
	})

	t.Run("metrics_range_url is empty (honest) when grafanaBaseURL empty", func(t *testing.T) {
		handler := statushttp.NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "", "")
		req := newStatusRequest("sample", "production", true)
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if got, _ := resp["metrics_range_url"].(string); got != "" {
			t.Errorf("metrics_range_url should be empty when grafanaBaseURL unset, got %q", got)
		}
	})

	t.Run("prom_query_keys is always present with 8 canonical names in order", func(t *testing.T) {
		handler := statushttp.NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "", "")
		req := newStatusRequest("sample", "production", true)
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		raw, ok := resp["prom_query_keys"].([]interface{})
		if !ok {
			t.Fatalf("prom_query_keys missing or not array, got %T", resp["prom_query_keys"])
		}

		want := []string{
			"uptime_24h",
			"replicas_ready",
			"p95_latency",
			"error_rate",
			"rps",
			"status_terminal",
			"alerts_active",
			"last_deploy_time",
		}

		if len(raw) != len(want) {
			t.Fatalf("prom_query_keys length: got %d, want %d", len(raw), len(want))
		}
		for i, expected := range want {
			got, _ := raw[i].(string)
			if got != expected {
				t.Errorf("prom_query_keys[%d]: got %q, want %q", i, got, expected)
			}
		}
	})

	t.Run("prom_query_keys is canonical even when grafanaBaseURL set", func(t *testing.T) {
		// Defense: the prom_query_keys list is canonical and independent of
		// grafanaBaseURL — dashboard consumers need the list to render
		// signal pills regardless of whether Grafana link is available.
		handler := statushttp.NewStatusHandler(projects, builds, releases, "dev.getloopscale.com", "", "https://grafana.loopruntime.local")
		req := newStatusRequest("sample", "production", true)
		rec := httptest.NewRecorder()

		handler.Get(rec, req)

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		raw, _ := resp["prom_query_keys"].([]interface{})
		if len(raw) != 8 {
			t.Errorf("prom_query_keys length: got %d, want 8 (canonical regardless of grafana flag)", len(raw))
		}
	})
}
