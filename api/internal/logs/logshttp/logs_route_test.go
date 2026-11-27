// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package logshttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/logs"
	"github.com/getloopscale/loop-runtime/api/internal/logs/logshttp"
	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth/authtest"

	"github.com/go-chi/chi/v5"
)

// fakeLogQuerier implements routes.LogQuerier for testing.
type fakeLogQuerier struct {
	entries []logs.LogEntry
	err     error
}

func (f *fakeLogQuerier) QueryBuildLogs(ctx context.Context, buildID string, opts logs.LogQueryOpts) ([]logs.LogEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

// fakeLogBuildStore implements routes.LogBuildStore for testing.
type fakeLogBuildStore struct {
	build *logshttp.LogBuild
	err   error
}

func (f *fakeLogBuildStore) GetByID(ctx context.Context, buildID string) (*logshttp.LogBuild, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.build, nil
}

// GetByIDForTenant — T0.4 of deep-review-2026-05-10-remediation-plan.
// Emulates the production SQL filter for the unit-test scope.
func (f *fakeLogBuildStore) GetByIDForTenant(_ context.Context, tenantID, _ string) (*logshttp.LogBuild, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.build == nil || f.build.TenantID != tenantID {
		return nil, nil
	}
	return f.build, nil
}

// newLogsRequest creates a GET request with chi URL params and optional auth context.
func newLogsRequest(buildID string, query string, authenticated bool) *http.Request {
	url := "/api/v1/builds/" + buildID + "/logs"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", buildID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	if authenticated {
		req = req.WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rctx2 := chi.NewRouteContext()
		rctx2.URLParams.Add("id", buildID)
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx2)
		req = req.WithContext(ctx)
	}

	return req
}

func TestLogsRoute(t *testing.T) {
	t.Run("GET /api/v1/builds/{id}/logs → 200 with entries", func(t *testing.T) {
		// Arrange
		buildStore := &fakeLogBuildStore{
			build: &logshttp.LogBuild{
				ID:       "build-abc123",
				TenantID: "acme",
				Status:   "SUCCEEDED",
			},
		}
		querier := &fakeLogQuerier{
			entries: []logs.LogEntry{
				{Step: "clone", Stream: "stdout", Content: "Cloning into '/workspace'...", Timestamp: "2026-03-25T14:30:01Z"},
				{Step: "install-deps", Stream: "stdout", Content: "npm install", Timestamp: "2026-03-25T14:30:05Z"},
			},
		}

		handler := logshttp.NewLogsHandler(buildStore, querier)
		req := newLogsRequest("build-abc123", "", true)
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

		if resp["build_id"] != "build-abc123" {
			t.Errorf("expected build_id 'build-abc123', got '%v'", resp["build_id"])
		}
		if resp["status"] != "SUCCEEDED" {
			t.Errorf("expected status 'SUCCEEDED', got '%v'", resp["status"])
		}
		entries, ok := resp["entries"].([]interface{})
		if !ok {
			t.Fatal("expected entries to be an array")
		}
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}
		entry0 := entries[0].(map[string]interface{})
		if entry0["step"] != "clone" {
			t.Errorf("expected first entry step 'clone', got '%v'", entry0["step"])
		}
	})

	t.Run("GET /api/v1/builds/{id}/logs → 404 when build not found", func(t *testing.T) {
		// Arrange
		buildStore := &fakeLogBuildStore{build: nil}
		querier := &fakeLogQuerier{}

		handler := logshttp.NewLogsHandler(buildStore, querier)
		req := newLogsRequest("nonexistent", "", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/builds/{id}/logs → 200 with empty entries when no logs available", func(t *testing.T) {
		// Arrange
		buildStore := &fakeLogBuildStore{
			build: &logshttp.LogBuild{
				ID:       "build-abc123",
				TenantID: "acme",
				Status:   "RUNNING",
			},
		}
		querier := &fakeLogQuerier{entries: []logs.LogEntry{}}

		handler := logshttp.NewLogsHandler(buildStore, querier)
		req := newLogsRequest("build-abc123", "", true)
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

		entries, ok := resp["entries"].([]interface{})
		if !ok {
			t.Fatal("expected entries to be an array")
		}
		if len(entries) != 0 {
			t.Errorf("expected 0 entries, got %d", len(entries))
		}
	})

	t.Run("GET /api/v1/builds/{id}/logs?tail=1 → returns last N entries", func(t *testing.T) {
		// Arrange
		buildStore := &fakeLogBuildStore{
			build: &logshttp.LogBuild{
				ID:       "build-abc123",
				TenantID: "acme",
				Status:   "SUCCEEDED",
			},
		}
		querier := &fakeLogQuerier{
			entries: []logs.LogEntry{
				{Step: "build", Stream: "stdout", Content: "Done", Timestamp: "2026-03-25T14:31:00Z"},
			},
		}

		handler := logshttp.NewLogsHandler(buildStore, querier)
		req := newLogsRequest("build-abc123", "tail=1", true)
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

		entries := resp["entries"].([]interface{})
		if len(entries) != 1 {
			t.Errorf("expected 1 entry with tail=1, got %d", len(entries))
		}
	})

	t.Run("GET /api/v1/builds/{id}/logs?step=clone → filters by step", func(t *testing.T) {
		// Arrange
		buildStore := &fakeLogBuildStore{
			build: &logshttp.LogBuild{
				ID:       "build-abc123",
				TenantID: "acme",
				Status:   "SUCCEEDED",
			},
		}
		querier := &fakeLogQuerier{
			entries: []logs.LogEntry{
				{Step: "clone", Stream: "stdout", Content: "Cloning...", Timestamp: "2026-03-25T14:30:01Z"},
			},
		}

		handler := logshttp.NewLogsHandler(buildStore, querier)
		req := newLogsRequest("build-abc123", "step=clone", true)
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

		entries := resp["entries"].([]interface{})
		if len(entries) != 1 {
			t.Errorf("expected 1 entry for step=clone, got %d", len(entries))
		}
	})

	t.Run("GET /api/v1/builds/{id}/logs → 401 without auth", func(t *testing.T) {
		// Arrange
		buildStore := &fakeLogBuildStore{}
		querier := &fakeLogQuerier{}

		handler := logshttp.NewLogsHandler(buildStore, querier)
		req := newLogsRequest("build-abc123", "", false)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/builds/{id}/logs → 404 for build of another tenant", func(t *testing.T) {
		// Arrange — build belongs to tenant "other-tenant", request auth is "acme"
		buildStore := &fakeLogBuildStore{
			build: &logshttp.LogBuild{
				ID:       "build-abc123",
				TenantID: "other-tenant",
				Status:   "SUCCEEDED",
			},
		}
		querier := &fakeLogQuerier{}

		handler := logshttp.NewLogsHandler(buildStore, querier)
		req := newLogsRequest("build-abc123", "", true)
		rec := httptest.NewRecorder()

		// Act
		handler.Get(rec, req)

		// Assert
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for tenant isolation, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
