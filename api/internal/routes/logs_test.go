// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Fake LogBuildStore ---

type fakeLogBuildStore struct {
	build *LogBuild
	err   error
}

func (f *fakeLogBuildStore) GetByID(_ context.Context, _ string) (*LogBuild, error) {
	return f.build, f.err
}

// --- Fake LogQuerier ---

type fakeLogQuerier struct {
	entries []LogEntry
	err     error
}

func (f *fakeLogQuerier) QueryBuildLogs(_ context.Context, _ string, _ LogQueryOpts) ([]LogEntry, error) {
	return f.entries, f.err
}

// --- Tests ---

func TestLogs_Get_HappyPath(t *testing.T) {
	builds := &fakeLogBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "SUCCEEDED"},
	}
	querier := &fakeLogQuerier{
		entries: []LogEntry{
			{Step: "clone", Stream: "stdout", Content: "Cloning...", Timestamp: "2026-01-01T00:00:00Z"},
			{Step: "build", Stream: "stdout", Content: "Building...", Timestamp: "2026-01-01T00:01:00Z"},
		},
	}
	h := NewLogsHandler(builds, querier)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp logsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BuildID != "bld-abc123" {
		t.Fatalf("want build_id bld-abc123, got %s", resp.BuildID)
	}
	if resp.TotalLines != 2 {
		t.Fatalf("want 2 entries, got %d", resp.TotalLines)
	}
}

func TestLogs_Get_BuildNotFound(t *testing.T) {
	builds := &fakeLogBuildStore{build: nil}
	querier := &fakeLogQuerier{}
	h := NewLogsHandler(builds, querier)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-nonexistent/logs", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "bld-nonexistent")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestLogs_Get_CrossTenantDenied(t *testing.T) {
	builds := &fakeLogBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-other-tenant", Status: "SUCCEEDED"},
	}
	querier := &fakeLogQuerier{}
	h := NewLogsHandler(builds, querier)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs", nil)
	req = withAuth(req, defaultUserContext()) // tn-test-123
	req = withChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	// SEC: cross-tenant returns 404 not 403
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for cross-tenant, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestLogs_Get_Unauthenticated(t *testing.T) {
	h := NewLogsHandler(&fakeLogBuildStore{}, &fakeLogQuerier{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs", nil)
	req = withChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestLogs_Get_LokiUnavailable_ReturnsEmptyEntries(t *testing.T) {
	builds := &fakeLogBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "RUNNING"},
	}
	querier := &fakeLogQuerier{
		err: errors.New("loki connection refused"),
	}
	h := NewLogsHandler(builds, querier)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	// Loki down returns 200 with empty entries (graceful degradation)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 even when loki unavailable, got %d body=%s", w.Code, w.Body.String())
	}

	var resp logsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TotalLines != 0 {
		t.Fatalf("want 0 entries when loki down, got %d", resp.TotalLines)
	}
}

func TestLogs_Get_WithQueryParams(t *testing.T) {
	builds := &fakeLogBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "SUCCEEDED"},
	}
	querier := &fakeLogQuerier{entries: []LogEntry{}}
	h := NewLogsHandler(builds, querier)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs?tail=100&step=build", nil)
	req = withAuth(req, defaultUserContext())
	req = withChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}
