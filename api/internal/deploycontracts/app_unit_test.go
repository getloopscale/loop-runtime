// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package deploycontracts

import (
	"strings"
	"testing"
)

// TestNormalizeApp_CopiesRuntime locks the T0.1 (F-dom-2) wiring: NormalizeApp
// MUST carry the services.json `runtime` onto the AppUnit so the build-strategy
// resolution (Phase 1) can dispatch on it. Given a raw AppSpec declaring
// runtime "python", When normalized, Then AppUnit.Runtime == "python".
func TestNormalizeApp_CopiesRuntime(t *testing.T) {
	for _, rt := range []string{"python", "node"} {
		unit, err := NormalizeApp("api", AppSpec{Path: ".", Runtime: rt, Port: 8080})
		if err != nil {
			t.Fatalf("runtime %q: unexpected error: %v", rt, err)
		}
		if unit.Runtime != rt {
			t.Errorf("Runtime = %q, want %q (NormalizeApp must carry services.json runtime — T0.1/F-dom-2)", unit.Runtime, rt)
		}
	}
}

func TestNormalizeApp_ServerExpress(t *testing.T) {
	unit, err := NormalizeApp("api", AppSpec{
		Path: "apps/api", Runtime: "node", Port: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Type != "server" {
		t.Errorf("Type = %q, want server", unit.Type)
	}
	if unit.Build != "buildpack" {
		t.Errorf("Build = %q, want buildpack", unit.Build)
	}
	if unit.Port != 3000 {
		t.Errorf("Port = %d, want 3000", unit.Port)
	}
	if unit.Dockerfile != "" {
		t.Errorf("Dockerfile = %q, want empty (buildpack)", unit.Dockerfile)
	}
}

func TestNormalizeApp_NextjsWithoutExplicitType_DefaultsServer(t *testing.T) {
	unit, err := NormalizeApp("web", AppSpec{
		Path: "apps/web", Port: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Type != "server" {
		t.Errorf("Type = %q, want server (T-B.1 explicit-type contract)", unit.Type)
	}
}

func TestNormalizeApp_RemixIsServer(t *testing.T) {
	// MEET-2026-03-29-016: Remix is SSR-first, classified as server
	unit, err := NormalizeApp("web", AppSpec{
		Path: "apps/web", Port: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Type != "server" {
		t.Errorf("Type = %q, want server (remix is SSR-first)", unit.Type)
	}
}

func TestNormalizeApp_WorkerExplicit(t *testing.T) {
	unit, err := NormalizeApp("worker", AppSpec{
		Path: "apps/worker", Type: "worker", Port: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Type != "worker" {
		t.Errorf("Type = %q, want worker", unit.Type)
	}
	if unit.Port != 0 {
		t.Errorf("Port = %d, want 0 (workers have no port)", unit.Port)
	}
}

// TestNormalizeApp_UserDockerfileIgnored_D4 locks the D4 security invariant
// (plan simplify-deploy-loopzerokit-only): BYO is deleted, so a user-supplied
// Build="dockerfile" + Dockerfile is NOT honored — NormalizeApp pins build to
// the platform-canonical "buildpack" and clears the dockerfile path. Given a
// raw app declaring a custom Dockerfile, When normalized, Then build is
// "buildpack" and dockerfile is empty.
func TestNormalizeApp_UserDockerfileIgnored_D4(t *testing.T) {
	unit, err := NormalizeApp("api", AppSpec{
		Path: ".", Build: "dockerfile", Dockerfile: "Dockerfile.prod",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Build != "buildpack" {
		t.Errorf("Build = %q, want buildpack (user Build=dockerfile must be ignored — D4)", unit.Build)
	}
	if unit.Dockerfile != "" {
		t.Errorf("Dockerfile = %q, want empty (user Dockerfile must be ignored — D4)", unit.Dockerfile)
	}
}

func TestNormalizeApp_DefaultsWhenEmpty(t *testing.T) {
	unit, err := NormalizeApp("api", AppSpec{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unit.Type != "server" {
		t.Errorf("Type = %q, want server (default)", unit.Type)
	}
	if unit.Path != "." {
		t.Errorf("Path = %q, want . (default)", unit.Path)
	}
	if unit.Build != "buildpack" {
		t.Errorf("Build = %q, want buildpack (fixed LoopZeroKit stack always uses the canonical platform Dockerfile — D4)", unit.Build)
	}
	if unit.Dockerfile != "" {
		t.Errorf("Dockerfile = %q, want empty (no user Dockerfile path — D4)", unit.Dockerfile)
	}
	if unit.Port != 8080 {
		t.Errorf("Port = %d, want 8080 (default server port)", unit.Port)
	}
	if len(unit.Secrets) != 0 {
		t.Errorf("Secrets = %v, want empty slice", unit.Secrets)
	}
}

func TestNormalizeApp_WithSecrets(t *testing.T) {
	unit, err := NormalizeApp("api", AppSpec{
		Runtime: "node",
		Port:    3000,
		Secrets: []string{"DATABASE_URL"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unit.Secrets) != 1 || unit.Secrets[0] != "DATABASE_URL" {
		t.Errorf("Secrets = %v, want [DATABASE_URL]", unit.Secrets)
	}
}

// TestNormalizeApp_GoFramework removed — go is not in the fixed LoopZeroKit stack
// (F-dom-2). NormalizeApp Type/Build defaults are covered by the tests above.

func TestNormalizeApp_InvalidType(t *testing.T) {
	_, err := NormalizeApp("api", AppSpec{Type: "invalid"})
	if err == nil {
		t.Error("expected error for invalid type")
	}
}

// Static delivery was removed (T3.3): every app is served by its container. An app that
// still declares `render: static` in loopzero.yaml was built as a container and then routed
// as static to assets nobody uploaded, so it was unreachable. It is refused, with a message
// that says what to change.
func TestNormalizeApp_RefusesARenderingOtherThanServer(t *testing.T) {
	for _, rendering := range []string{"static", "edge"} {
		_, err := NormalizeApp("web", AppSpec{Rendering: rendering})
		if err == nil {
			t.Fatalf("render %q: no error", rendering)
		}
		for _, want := range []string{"app 'web'", "render '" + rendering + "'", "remove render"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("render %q: error %q lacks %q", rendering, err, want)
			}
		}
	}
	for _, rendering := range []string{"", "server"} {
		if _, err := NormalizeApp("web", AppSpec{Rendering: rendering}); err != nil {
			t.Errorf("render %q: %v", rendering, err)
		}
	}
}
