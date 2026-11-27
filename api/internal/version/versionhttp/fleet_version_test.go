// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package versionhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The status FLEET contract — what the dashboard's /status panel consumes.
//
// WHY A SECOND ENDPOINT, rather than a change to /api/v1/version:
//
// The loop-console collector (internal/serviceversions/probe.go) calls
// `GET {base}/v1/version` and reads `{version, built_at}`. Five services already
// speak that contract. The engine spoke another — `/api/v1/version` with
// `build_date` — so it was left off the panel for diverging in path AND field.
//
// Renaming `build_date` to `built_at` on the existing endpoint would break Rule 5
// (the --json schema is a stable contract and does not change without a major
// bump). The two endpoints coexist serving the SAME truth in two shapes: one is
// the CLI's public contract, the other the fleet's.
func TestFleetVersionHandler_SpeaksTheFleetContract(t *testing.T) {
	rr := httptest.NewRecorder()
	FleetVersionHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/version", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	// Decode in the exact shape the collector expects. A field with another name
	// arrives here as the zero value, which is precisely the failure to pin.
	var got struct {
		Version string `json:"version"`
		BuiltAt string `json:"built_at"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version == "" {
		t.Error("empty version — the panel would show a blank cell for an `ok` service")
	}
	if got.BuiltAt == "" {
		t.Error("empty built_at — the panel's `Built` column would be blank")
	}
}

// The same truth on both endpoints. If they diverge, one of them is lying, and
// the panel and the CLI would disagree about which binary is running.
func TestFleetVersionHandler_AgreesWithTheCLIContract(t *testing.T) {
	fleetRec := httptest.NewRecorder()
	FleetVersionHandler().ServeHTTP(fleetRec, httptest.NewRequest(http.MethodGet, "/v1/version", nil))
	cliRec := httptest.NewRecorder()
	VersionHandler().ServeHTTP(cliRec, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))

	var fleet struct {
		Version string `json:"version"`
		BuiltAt string `json:"built_at"`
	}
	var cli VersionResponse
	if err := json.NewDecoder(fleetRec.Body).Decode(&fleet); err != nil {
		t.Fatalf("decode fleet: %v", err)
	}
	if err := json.NewDecoder(cliRec.Body).Decode(&cli); err != nil {
		t.Fatalf("decode cli: %v", err)
	}
	if fleet.Version != cli.Version {
		t.Errorf("version diverges: fleet=%q cli=%q", fleet.Version, cli.Version)
	}
	if fleet.BuiltAt != cli.BuildDate {
		t.Errorf("build date diverges: fleet=%q cli=%q", fleet.BuiltAt, cli.BuildDate)
	}
}

// Without ldflags the globals fall back to their declared defaults. The panel
// needs a readable value — never an empty cell over a service that answered `ok`.
func TestFleetVersionHandler_NeverEmptyWithoutLdflags(t *testing.T) {
	origTag, origBuild := versionTag, versionBuildDate
	versionTag, versionBuildDate = "", ""
	defer func() { versionTag, versionBuildDate = origTag, origBuild }()

	rr := httptest.NewRecorder()
	FleetVersionHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/version", nil))

	var got struct {
		Version string `json:"version"`
		BuiltAt string `json:"built_at"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version == "" || got.BuiltAt == "" {
		t.Errorf("empty fields without ldflags: version=%q built_at=%q", got.Version, got.BuiltAt)
	}
}
