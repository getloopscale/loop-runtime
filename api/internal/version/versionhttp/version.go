// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package versionhttp serves the build-metadata endpoints — `/api/v1/version`,
// `/v1/version` and the fleet variant.
//
// First family to leave the flat `routes` package (B-041 slice-8). It was chosen
// as the first precisely because it depends on nothing inside `routes` — not
// even the shared transport — so the move proved the recipe without the result
// resting on anything else moving correctly.
package versionhttp

import (
	"encoding/json"
	"net/http"
	"runtime"
)

// F-07 (CHG-2026-04-29-DOGFOOD T5.2) — /api/v1/version is a public endpoint
// (no auth) that returns the binary's metadata, used by dogfood runs and
// external smoke checks. The build flags are injected via ldflags; when absent
// (e.g. `go run`) they fall back to "dev" / "unknown", documented so /version
// always answers 200 whatever the build environment.

// Version metadata — set via ldflags at build time (see api/Dockerfile).
//
// The -X path is the PACKAGE path, and it changed when the family moved out of
// `api/internal/routes` (B-041 slice-8). A -X with the wrong path is DROPPED
// SILENTLY by the linker — measured: with the old path the binary reports
// `versionTag="dev"` with no warning. This block is the one correct reference
// in the repository, and what issue #284 should copy when fixing the
// Dockerfile (which gets the org wrong, not the path).
//
//	go build -ldflags "\
//	  -X github.com/getloopscale/loop-runtime/api/internal/version/versionhttp.versionTag=$(git describe --tags) \
//	  -X github.com/getloopscale/loop-runtime/api/internal/version/versionhttp.versionCommit=$(git rev-parse HEAD) \
//	  -X github.com/getloopscale/loop-runtime/api/internal/version/versionhttp.versionBuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	versionTag       = "dev"
	versionCommit    = "unknown"
	versionBuildDate = "unknown"
)

// VersionResponse is the schema returned by GET /api/v1/version.
type VersionResponse struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// fleetVersionResponse is the schema the dashboard's status panel consumes.
//
// The field names are NOT ours to choose: they are the contract
// loopzero-cloud/internal/serviceversions/probe.go already speaks with five services
// (`version` + `built_at`). The engine was left off the panel for diverging from
// it in both path AND field name.
type fleetVersionResponse struct {
	Version string `json:"version"`
	BuiltAt string `json:"built_at"`
}

// FleetVersionHandler serves GET /v1/version — the status fleet contract.
//
// It coexists with VersionHandler on purpose. `/api/v1/version` is the CLI's
// stable public contract (Rule 5): renaming `build_date` to `built_at` there
// would break consumers without a major bump. Both read the SAME globals, so
// they serve the same truth — a test pins that equality, because two sources
// that CAN diverge end up diverging.
//
// No authentication, like /api/v1/version: build metadata is not a secret, and
// the collector probes the target without any session.
func FleetVersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Explicit fallback: an empty panel cell over a service that answered `ok`
		// suggests missing data, when what happened was a build without ldflags.
		_ = json.NewEncoder(w).Encode(fleetVersionResponse{
			Version: orUnknown(versionTag),
			BuiltAt: orUnknown(versionBuildDate),
		})
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// VersionHandler returns a chi.HandlerFunc for /api/v1/version.
// No authentication required. Stable schema (per Rule 5 — JSON contract).
func VersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp := VersionResponse{
			Version:   versionTag,
			Commit:    versionCommit,
			BuildDate: versionBuildDate,
			GoVersion: runtime.Version(),
			Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}
