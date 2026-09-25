// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package versionhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

// F-07 — /api/v1/version endpoint contract.
func TestVersionHandler_Returns200(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	VersionHandler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func TestVersionHandler_SchemaCorrect(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	VersionHandler().ServeHTTP(rr, req)

	var resp VersionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// Required fields must always be populated, even without ldflags.
	if resp.Version == "" {
		t.Error("version field empty")
	}
	if resp.Commit == "" {
		t.Error("commit field empty")
	}
	if resp.BuildDate == "" {
		t.Error("build_date field empty")
	}
	if resp.GoVersion != runtime.Version() {
		t.Errorf("go_version = %q, want %q", resp.GoVersion, runtime.Version())
	}
	if resp.Platform == "" {
		t.Error("platform field empty")
	}
}

// When build flags are unset, defaults must produce a valid response (smoke).
func TestVersionHandler_HandlesUnsetBuildFlags(t *testing.T) {
	// Save / restore globals (table tests would need state isolation).
	origTag, origCommit, origBuild := versionTag, versionCommit, versionBuildDate
	versionTag, versionCommit, versionBuildDate = "", "", ""
	defer func() { versionTag, versionCommit, versionBuildDate = origTag, origCommit, origBuild }()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	VersionHandler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 even with unset flags, got %d", rr.Code)
	}
	var resp VersionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// GoVersion + Platform always come from the runtime, so they MUST be present.
	if resp.GoVersion == "" || resp.Platform == "" {
		t.Error("runtime-derived fields must be populated")
	}
}
