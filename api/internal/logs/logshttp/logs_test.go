// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package logshttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

// --- Fake LogBuildStore ---

type fakeLogBuildStore struct {
	build *LogBuild
	err   error
}

func (f *fakeLogBuildStore) GetByID(_ context.Context, _ string) (*LogBuild, error) {
	return f.build, f.err
}

// GetByIDForTenant — T0.4 of deep-review-2026-05-10-remediation-plan.
// Emulates the production SQL filter (WHERE id=$1 AND tenant_id=$2):
// returns nil when the build belongs to a different tenant. Unit tests
// that set build.TenantID to a different value than the auth context
// rely on this filter for the cross-tenant 404 path.
func (f *fakeLogBuildStore) GetByIDForTenant(_ context.Context, tenantID, _ string) (*LogBuild, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.build == nil {
		return nil, nil
	}
	if f.build.TenantID != tenantID {
		return nil, nil
	}
	return f.build, nil
}

// --- Fake LogQuerier ---

type fakeLogQuerier struct {
	entries  []LogEntry
	err      error
	lastOpts LogQueryOpts
}

func (f *fakeLogQuerier) QueryBuildLogs(_ context.Context, _ string, opts LogQueryOpts) ([]LogEntry, error) {
	// Beta Logs Gate Criterion 7 — capture the SinceSeconds the
	// handler propagated so tests can assert it.
	f.lastOpts = opts
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-nonexistent")
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
	req = routestest.WithAuth(req, routestest.DefaultUserContext()) // tn-test-123
	req = routestest.WithChiParam(req, "id", "bld-abc123")
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
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// A log backend outage is not "this build has no logs": answering 200 with no
// entries made the CLI tell users their logs "were not captured or have been
// pruned" while the logs were intact and only unreachable.
func TestLogs_Get_LogBackendUnavailableIs503(t *testing.T) {
	builds := &fakeLogBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "SUCCEEDED"},
	}
	querier := &fakeLogQuerier{err: errors.New("loki connection refused")}
	h := NewLogsHandler(builds, querier)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when the log backend is unreachable, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["code"] != "LOGS_UNAVAILABLE" {
		t.Errorf("code = %v, want LOGS_UNAVAILABLE", body["code"])
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "loki") {
		t.Errorf("response names an internal component: %s", w.Body.String())
	}
}

func TestLogs_Get_WithQueryParams(t *testing.T) {
	builds := &fakeLogBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "SUCCEEDED"},
	}
	querier := &fakeLogQuerier{entries: []LogEntry{}}
	h := NewLogsHandler(builds, querier)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs?tail=100&step=build", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// Beta Logs Gate Criterion 7 — `?since=` propagates to LogQueryOpts.
func TestLogs_Get_SinceParamPropagatesToQuerier(t *testing.T) {
	cases := []struct {
		name        string
		queryParam  string
		wantSeconds int64
	}{
		// Day shorthand — most common operator usage
		{"7d", "7d", 7 * 86400},
		{"30d", "30d", 30 * 86400},
		{"1d", "1d", 86400},
		// Go duration syntax
		{"24h", "24h", 24 * 3600},
		{"30m", "30m", 1800},
		{"45s", "45s", 45},
		// Pure integer seconds
		{"raw seconds 3600", "3600", 3600},
		// Edge: zero / negative → no override (0 = use default)
		{"zero", "0", 0},
		{"negative", "-5", 0},
		// Edge: garbage → no override
		{"garbage", "potato", 0},
		// Edge: empty → no override (handler skips entirely)
		{"empty", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builds := &fakeLogBuildStore{
				build: &LogBuild{ID: "bld-x", TenantID: "tn-test-123", Status: "SUCCEEDED"},
			}
			querier := &fakeLogQuerier{entries: []LogEntry{}}
			h := NewLogsHandler(builds, querier)

			path := "/api/v1/builds/bld-x/logs"
			if tc.queryParam != "" {
				path += "?since=" + tc.queryParam
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req = routestest.WithAuth(req, routestest.DefaultUserContext())
			req = routestest.WithChiParam(req, "id", "bld-x")
			w := httptest.NewRecorder()

			h.Get(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
			}
			if querier.lastOpts.SinceSeconds != tc.wantSeconds {
				t.Errorf("since=%q → SinceSeconds want %d, got %d",
					tc.queryParam, tc.wantSeconds, querier.lastOpts.SinceSeconds)
			}
		})
	}
}

// TestParseSinceParam — direct unit tests on the pure helper.
// Duplicates a few of the handler-level cases above intentionally:
// the helper is exported-by-package so other routes (runtime logs,
// future SSE filters) can reuse it.
func TestParseSinceParam(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"7d", 7 * 86400},
		{"24h", 86400},
		{"3600", 3600},
		{"30m", 1800},
		{"", 0},
		{"0", 0},
		{"-1", 0},
		{"abc", 0},
		{"1d", 86400},
		{"100d", 100 * 86400},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := parseSinceParam(tc.in); got != tc.want {
				t.Errorf("parseSinceParam(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
