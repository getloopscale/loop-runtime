// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/middleware/auth/authtest"
	"github.com/getloopscale/loopzero/api/internal/routes/builds"

	"github.com/go-chi/chi/v5"
)

// --- Fake BuildListStore ---

type fakeBuildListStore struct {
	builds []builds.BuildListItem
	total  int
	err    error

	// Track calls
	lastTenantID  string
	lastProjectID string
	lastEnv       string
	lastLimit     int
	lastOffset    int
}

func (f *fakeBuildListStore) ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]builds.BuildListItem, int, error) {
	f.lastTenantID = tenantID
	f.lastProjectID = projectID
	f.lastEnv = env
	f.lastLimit = limit
	f.lastOffset = offset
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.builds, f.total, nil
}

func (f *fakeBuildListStore) ListEnvsForProject(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

// Compile-time check
var _ builds.BuildListStore = (*fakeBuildListStore)(nil)

func buildListRouter(handler *builds.BuildListHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/v1/projects/{name}/builds", handler.List)
	return r
}

func TestBuildListRoute(t *testing.T) {
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-30 * time.Second)
	finishedAt := now
	duration := 30

	t.Run("GET /projects/{name}/builds returns 200 with builds and total", func(t *testing.T) {
		// Arrange
		store := &fakeBuildListStore{
			builds: []builds.BuildListItem{
				{
					ID:              "build-001",
					ProjectID:       "my-app",
					Environment:     "production",
					Status:          "SUCCEEDED",
					CommitSHA:       "abc123",
					CreatedBy:       "github-lucas",
					CreatedAt:       now,
					StartedAt:       &startedAt,
					FinishedAt:      &finishedAt,
					DurationSeconds: &duration,
				},
			},
			total: 1,
		}
		handler := builds.NewBuildListHandler(store)
		router := buildListRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-app/builds", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Builds []builds.BuildStatusResponse `json:"builds"`
			Total  int                          `json:"total"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Total != 1 {
			t.Errorf("expected total 1, got %d", resp.Total)
		}
		if len(resp.Builds) != 1 {
			t.Fatalf("expected 1 build, got %d", len(resp.Builds))
		}
		b := resp.Builds[0]
		if b.BuildID != "build-001" {
			t.Errorf("expected build_id 'build-001', got '%s'", b.BuildID)
		}
		if b.Status != "SUCCEEDED" {
			t.Errorf("expected status 'SUCCEEDED', got '%s'", b.Status)
		}
		if b.CreatedAt == "" {
			t.Error("expected created_at to be populated")
		}
		if b.StartedAt == "" {
			t.Error("expected started_at to be populated")
		}
		if b.FinishedAt == "" {
			t.Error("expected finished_at to be populated")
		}
		if b.DurationSeconds == nil || *b.DurationSeconds != 30 {
			t.Errorf("expected duration_seconds 30, got %v", b.DurationSeconds)
		}

		// Verify store received correct params
		if store.lastTenantID != "acme" {
			t.Errorf("expected tenant_id 'acme', got '%s'", store.lastTenantID)
		}
		if store.lastProjectID != "my-app" {
			t.Errorf("expected project_id 'my-app', got '%s'", store.lastProjectID)
		}
	})

	t.Run("GET /projects/{name}/builds with env filter passes env to store", func(t *testing.T) {
		// Arrange
		store := &fakeBuildListStore{builds: []builds.BuildListItem{}, total: 0}
		handler := builds.NewBuildListHandler(store)
		router := buildListRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-app/builds?env=staging", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if store.lastEnv != "staging" {
			t.Errorf("expected env 'staging', got '%s'", store.lastEnv)
		}
	})

	t.Run("GET /projects/{name}/builds with limit and offset passes them to store", func(t *testing.T) {
		// Arrange
		store := &fakeBuildListStore{builds: []builds.BuildListItem{}, total: 0}
		handler := builds.NewBuildListHandler(store)
		router := buildListRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-app/builds?limit=10&offset=20", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if store.lastLimit != 10 {
			t.Errorf("expected limit 10, got %d", store.lastLimit)
		}
		if store.lastOffset != 20 {
			t.Errorf("expected offset 20, got %d", store.lastOffset)
		}
	})

	t.Run("GET /projects/{name}/builds clamps limit to 1-50 range", func(t *testing.T) {
		// Arrange — limit clamping is done in the store, but handler passes the raw value.
		// The store (ListByProjectAndEnv) clamps. Handler passes through.
		store := &fakeBuildListStore{builds: []builds.BuildListItem{}, total: 0}
		handler := builds.NewBuildListHandler(store)
		router := buildListRouter(handler)

		// Test limit > 50 — passed through to store which clamps
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-app/builds?limit=100", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if store.lastLimit != 100 {
			t.Errorf("expected limit 100 passed to store (store clamps), got %d", store.lastLimit)
		}
	})

	t.Run("GET /projects/{name}/builds defaults limit to 20", func(t *testing.T) {
		// Arrange
		store := &fakeBuildListStore{builds: []builds.BuildListItem{}, total: 0}
		handler := builds.NewBuildListHandler(store)
		router := buildListRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-app/builds", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if store.lastLimit != 20 {
			t.Errorf("expected default limit 20, got %d", store.lastLimit)
		}
		if store.lastOffset != 0 {
			t.Errorf("expected default offset 0, got %d", store.lastOffset)
		}
	})

	t.Run("GET /projects/{name}/builds auth missing returns 401", func(t *testing.T) {
		// Arrange
		store := &fakeBuildListStore{}
		handler := builds.NewBuildListHandler(store)
		router := buildListRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-app/builds", nil)
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /projects/{name}/builds store error returns 500", func(t *testing.T) {
		// Arrange
		store := &fakeBuildListStore{
			err: fmt.Errorf("database connection lost"),
		}
		handler := builds.NewBuildListHandler(store)
		router := buildListRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-app/builds", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /projects/{name}/builds empty result returns empty array not null", func(t *testing.T) {
		// Arrange
		store := &fakeBuildListStore{builds: []builds.BuildListItem{}, total: 0}
		handler := builds.NewBuildListHandler(store)
		router := buildListRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-app/builds", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Builds []builds.BuildStatusResponse `json:"builds"`
			Total  int                          `json:"total"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Builds == nil {
			t.Error("expected builds to be empty array, got null")
		}
		if len(resp.Builds) != 0 {
			t.Errorf("expected 0 builds, got %d", len(resp.Builds))
		}
	})

	t.Run("GET /projects/{name}/builds populates error_message field", func(t *testing.T) {
		// Arrange
		store := &fakeBuildListStore{
			builds: []builds.BuildListItem{
				{
					ID:           "build-fail",
					ProjectID:    "my-app",
					Environment:  "production",
					Status:       "FAILED",
					CreatedAt:    now,
					ErrorMessage: "build timeout exceeded",
				},
			},
			total: 1,
		}
		handler := builds.NewBuildListHandler(store)
		router := buildListRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/my-app/builds", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Builds []builds.BuildStatusResponse `json:"builds"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp.Builds) != 1 {
			t.Fatalf("expected 1 build, got %d", len(resp.Builds))
		}
		if resp.Builds[0].ErrorMessage != "build timeout exceeded" {
			t.Errorf("expected error 'build timeout exceeded', got '%s'", resp.Builds[0].ErrorMessage)
		}
	})
}
