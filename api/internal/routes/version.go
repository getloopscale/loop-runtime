// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"net/http"
	"runtime"
)

// F-07 (CHG-2026-04-29-DOGFOOD T5.2) — /api/v1/version é endpoint público
// (sem auth) que retorna metadata do binário. Usado pelo skill de dogfood +
// smoke checks externos. Os 4 build flags são injetados via ldflags em
// `cmd/main.go`; quando ausentes (ex: `go run`), retornam strings vazias /
// "dev" — comportamento documentado pra que o /version sempre responda 200
// independente do build environment.

// Version metadata — populados via ldflags em build (ver api/Dockerfile).
//
//	go build -ldflags "\
//	  -X github.com/loopzero/loopzero/api/internal/routes.versionTag=$(git describe --tags) \
//	  -X github.com/loopzero/loopzero/api/internal/routes.versionCommit=$(git rev-parse HEAD) \
//	  -X github.com/loopzero/loopzero/api/internal/routes.versionBuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
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
