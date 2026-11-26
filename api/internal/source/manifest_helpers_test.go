// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package source

import (
	"strings"
	"testing"
)

// T3.1 (plan simplify-deploy-loopzerokit-only v2.0) — native-validator helpers.
// These replace routes/source.go's dependency on the deprecated
// ValidateServicesJSON bridge: the handler only needs the project name +
// sorted app names, both derivable from the native *ServicesManifest.

func TestProjectNameFromManifest_V2Project(t *testing.T) {
	// Arrange
	m := &ServicesManifest{Version: 2, Project: "myapp"}
	// Act
	got := ProjectNameFromManifest(m)
	// Assert
	if got != "myapp" {
		t.Fatalf("ProjectNameFromManifest v2: got %q, want %q", got, "myapp")
	}
}

func TestProjectNameFromManifest_V1LegacyFallback(t *testing.T) {
	// Arrange — v1 payload without project
	m := &ServicesManifest{Version: 1, Project: ""}
	// Act
	got := ProjectNameFromManifest(m)
	// Assert — legacy fallback preserved (ADR D10 — keeps Gitea repo lineage)
	if got != servicesJSONV1LegacyProject {
		t.Fatalf("ProjectNameFromManifest v1 legacy: got %q, want %q", got, servicesJSONV1LegacyProject)
	}
}

func TestAppNamesFromManifest_Sorted(t *testing.T) {
	// Arrange — out-of-order service names
	m := &ServicesManifest{Services: []ServiceEntry{{Name: "web"}, {Name: "api"}, {Name: "worker"}}}
	// Act
	got := AppNamesFromManifest(m)
	// Assert — sorted deterministically
	want := []string{"api", "web", "worker"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("AppNamesFromManifest: got %v, want %v", got, want)
	}
}

// C7 — the deprecated bridge hardcoded Type:"server", mislabeling workers.
// The native manifest MUST preserve the declared type.
func TestValidateServicesJSONNative_PreservesWorkerType(t *testing.T) {
	content := `{
		"version": 2,
		"project": "myapp",
		"services": [
			{
				"name": "jobs",
				"runtime": "node",
				"type": "worker",
				"port": 8001,
				"proxy": "/jobs",
				"dev": "npm run dev",
				"start": "node dist/worker.js",
				"healthcheck": "/healthz",
				"cors": false,
				"passSetCookie": false
			}
		]
	}`
	// Act
	m, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	// Assert
	if err != nil {
		t.Fatalf("ValidateServicesJSONNative: unexpected error: %v", err)
	}
	if len(m.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(m.Services))
	}
	if m.Services[0].Type != "worker" {
		t.Fatalf("worker type flattened: got %q, want %q (C7 regression)", m.Services[0].Type, "worker")
	}
	if m.Services[0].Start != "node dist/worker.js" {
		t.Fatalf("start dropped: got %q", m.Services[0].Start)
	}
}
