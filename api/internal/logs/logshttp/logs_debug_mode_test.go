// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package logshttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"
)

func debugRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs?mode=debug", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	return routestest.WithChiParam(req, "id", "bld-abc123")
}

func debugHandler() (*LogsHandler, *fakeLogQuerier) {
	querier := &fakeLogQuerier{}
	builds := &fakeLogBuildStore{build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "SUCCEEDED"}}
	return NewLogsHandler(builds, querier), querier
}

// With no authorizer wired, debug mode is refused: a dashboard user's JWT
// never unlocks the unfiltered container output.
func TestLogs_DebugModeIsRefusedWithoutAnAuthorizer(t *testing.T) {
	h, querier := debugHandler()
	w := httptest.NewRecorder()

	h.Get(w, debugRequest())

	if w.Code != http.StatusForbidden || querier.lastOpts.Debug {
		t.Fatalf("want 403 and no debug query, got %d debug=%v", w.Code, querier.lastOpts.Debug)
	}
}

func TestLogs_DebugModeIsRefusedWhenTheAuthorizerSaysNo(t *testing.T) {
	h, _ := debugHandler()
	w := httptest.NewRecorder()

	h.WithDebugAuthorizer(func(*http.Request) bool { return false }).Get(w, debugRequest())

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
}

func TestLogs_DebugModeQueriesUnfilteredForAnAuthorizedOperator(t *testing.T) {
	h, querier := debugHandler()
	w := httptest.NewRecorder()

	h.WithDebugAuthorizer(func(*http.Request) bool { return true }).Get(w, debugRequest())

	if w.Code != http.StatusOK || !querier.lastOpts.Debug {
		t.Fatalf("want 200 with a debug query, got %d debug=%v", w.Code, querier.lastOpts.Debug)
	}
}
