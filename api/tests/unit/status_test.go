// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// fakeStatusProjectStore implements routes.StatusProjectStore for testing.
type fakeStatusProjectStore struct {
	project *routes.ProjectResult
	err     error
}

func (f *fakeStatusProjectStore) GetByName(ctx context.Context, tenantID, name string) (*routes.ProjectResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.project, nil
}

// fakeStatusBuildStore implements routes.StatusBuildStore for testing.
type fakeStatusBuildStore struct {
	builds []routes.StatusBuild
	err    error
}

func (f *fakeStatusBuildStore) ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]routes.StatusBuild, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.builds, nil
}

// fakeStatusReleaseStore implements routes.StatusReleaseStore for testing.
type fakeStatusReleaseStore struct {
	release *routes.StatusRelease
	err     error
}

func (f *fakeStatusReleaseStore) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*routes.StatusRelease, error) {
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
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
			project: &routes.ProjectResult{ID: "proj-123", Name: "my-app", TeamID: "acme"},
		}
		builds := &fakeStatusBuildStore{
			builds: []routes.StatusBuild{
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
			release: &routes.StatusRelease{
				ID:        "rel-def456",
				Status:    "ACTIVE",
				GitCommit: "abc123",
				CreatedAt: time.Date(2026, 3, 25, 1, 5, 30, 0, time.UTC),
			},
		}

		handler := routes.NewStatusHandler(projects, builds, releases, "dev.loopzero.example")
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
		if resp["url"] != "https://my-app-production.dev.loopzero.example" {
			t.Errorf("expected url 'https://my-app-production.dev.loopzero.example', got '%v'", resp["url"])
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

		handler := routes.NewStatusHandler(projects, builds, releases, "dev.loopzero.example")
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

	t.Run("GET /api/v1/projects/{name}/status → defaults env to production when not specified", func(t *testing.T) {
		// Arrange
		projects := &fakeStatusProjectStore{
			project: &routes.ProjectResult{ID: "proj-123", Name: "my-app", TeamID: "acme"},
		}
		builds := &fakeStatusBuildStore{
			builds: []routes.StatusBuild{
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

		handler := routes.NewStatusHandler(projects, builds, releases, "dev.loopzero.example")
		req := newStatusRequest("my-app", "", true)
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

		if resp["environment"] != "production" {
			t.Errorf("expected environment 'production' as default, got '%v'", resp["environment"])
		}
	})

	t.Run("GET /api/v1/projects/{name}/status → 200 with null builds and releases when none exist", func(t *testing.T) {
		// Arrange
		projects := &fakeStatusProjectStore{
			project: &routes.ProjectResult{ID: "proj-123", Name: "my-app", TeamID: "acme"},
		}
		builds := &fakeStatusBuildStore{builds: []routes.StatusBuild{}}
		releases := &fakeStatusReleaseStore{release: nil}

		handler := routes.NewStatusHandler(projects, builds, releases, "dev.loopzero.example")
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

		handler := routes.NewStatusHandler(projects, builds, releases, "dev.loopzero.example")
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
			project: &routes.ProjectResult{ID: "proj-123", Name: "my-app", TeamID: "acme"},
		}
		builds := &fakeStatusBuildStore{
			builds: []routes.StatusBuild{
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

		handler := routes.NewStatusHandler(projects, builds, releases, "dev.loopzero.example")
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
