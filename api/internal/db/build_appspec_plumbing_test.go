// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db

import (
	"encoding/json"
	"testing"
)

// T0.2 (plan simplify-deploy-loopzerokit-only v2.0) — start/path/build plumbing.
// Removing loopruntime-packs (which auto-detected start/build at build time) means
// the build pipeline must PERSIST the declared services.json start command,
// app path (monorepo), and build mode. build_adapter.go:160-164 documents the
// pre-existing gap ("schema does not persist them"). This test locks the
// JSONB persistence of the new fields.
func TestBuildAppSpec_PersistsStartPathBuild(t *testing.T) {
	// Arrange
	in := BuildAppSpec{
		Name:    "api",
		Type:    "worker",
		Port:    8001,
		Start:   "node dist/worker.js",
		Path:    "apps/api",
		Build:   "buildpack",
		Runtime: "node", // T0.1 (F-dom-2): runtime persisted through JSONB
	}
	// Act — round-trip through JSON (the apps_spec column is JSONB)
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out BuildAppSpec
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Assert
	if out.Start != "node dist/worker.js" {
		t.Fatalf("Start dropped: got %q", out.Start)
	}
	if out.Path != "apps/api" {
		t.Fatalf("Path dropped: got %q", out.Path)
	}
	if out.Build != "buildpack" {
		t.Fatalf("Build dropped: got %q", out.Build)
	}
	if out.Runtime != "node" {
		t.Fatalf("Runtime dropped: got %q", out.Runtime)
	}
}

// Back-compat: an old apps_spec row without start/path/build decodes cleanly
// (empty strings), never errors.
func TestBuildAppSpec_BackCompat_OldRowNoNewFields(t *testing.T) {
	old := `{"name":"web","type":"server","port":8080}`
	var out BuildAppSpec
	if err := json.Unmarshal([]byte(old), &out); err != nil {
		t.Fatalf("back-compat unmarshal failed: %v", err)
	}
	if out.Start != "" || out.Path != "" || out.Build != "" {
		t.Fatalf("expected empty new fields on legacy row, got start=%q path=%q build=%q", out.Start, out.Path, out.Build)
	}
}
