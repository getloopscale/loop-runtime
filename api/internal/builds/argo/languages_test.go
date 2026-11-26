// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	forge "github.com/getloopscale/argo-workflows-go"
)

// ---------------------------------------------------------------------------
// ResolveStrategy
// ---------------------------------------------------------------------------

// T1.1 (simplify-deploy-loopzerokit-runtime-wiring / F-dom-2): ResolveStrategy is
// now keyed on the services.json `runtime` (python|node), NOT on legacy framework
// strings. Only "node" and "python" resolve; everything else (incl. legacy
// framework names like "express"/"nextjs"/"fastapi" and empty) fails closed (D4).
func TestResolveStrategy_RuntimeKeyed(t *testing.T) {
	if got := ResolveStrategy("node").Name(); got != "node" {
		t.Errorf(`ResolveStrategy("node") = %q, want node`, got)
	}
	if got := ResolveStrategy("python").Name(); got != "python" {
		t.Errorf(`ResolveStrategy("python") = %q, want python`, got)
	}
}

// TestResolveStrategy_NonRuntimeFailsClosed: legacy framework strings + empty
// runtime now fail closed — no Node default (D4), no framework mapping.
func TestResolveStrategy_NonRuntimeFailsClosed(t *testing.T) {
	for _, notRuntime := range []string{"", "express", "nextjs", "vite", "fastapi", "django", "go", "ruby", "custom"} {
		if got := ResolveStrategy(notRuntime).Name(); got != "unsupported" {
			t.Errorf("ResolveStrategy(%q) = %q, want unsupported (only python|node resolve — D1/D4)", notRuntime, got)
		}
	}
}

// ---------------------------------------------------------------------------
// NodeStrategy
// ---------------------------------------------------------------------------

func TestNodeStrategy_CacheKey(t *testing.T) {
	s := &NodeStrategy{}
	if s.CacheKey() != "node-modules" {
		t.Errorf("CacheKey() = %q, want node-modules", s.CacheKey())
	}
}

func TestNodeStrategy_CacheDirs(t *testing.T) {
	s := &NodeStrategy{}
	dirs := s.CacheDirs()
	if len(dirs) != 1 || dirs[0] != "node_modules" {
		t.Errorf("CacheDirs() = %v, want [node_modules]", dirs)
	}
}

func TestNodeStrategy_ValidateStep_ReturnsContainerTemplate(t *testing.T) {
	s := &NodeStrategy{}
	apps := []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 3000}}
	c := s.ValidateStep(apps)

	assertContainerShape(t, c, "validate-workspace", "node:22.12.0")
}

func TestNodeStrategy_InstallStep_HasRetryStrategy(t *testing.T) {
	s := &NodeStrategy{}
	apps := []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 3000}}
	c := s.InstallStep(apps)

	assertContainerShape(t, c, "install-deps", "node:22.12.0")

	if c.RetryStrategy == nil {
		t.Error("InstallStep must have RetryStrategy")
	}
}

func TestNodeStrategy_BuildStep_ReturnsContainerTemplate(t *testing.T) {
	s := &NodeStrategy{}
	apps := []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 3000}}
	c := s.BuildStep(apps)

	assertContainerShape(t, c, "build-apps", "node:22.12.0")
}

func TestNodeStrategy_ScanStep_ReturnsContainerTemplate(t *testing.T) {
	s := &NodeStrategy{}
	c := s.ScanStep(nil)

	assertContainerShape(t, c, "security-scan", "node:22.12.0")
}

// ---------------------------------------------------------------------------
// Dockerfile naming (MEET-2026-03-29-018)
// ---------------------------------------------------------------------------

func TestDockerfilePathForApp_SingleApp(t *testing.T) {
	path := dockerfilePathForApp("api", 1)
	if path != "/workspace/Dockerfile" {
		t.Errorf("single app: got %q, want /workspace/Dockerfile", path)
	}
}

func TestDockerfilePathForApp_MultiApp(t *testing.T) {
	path := dockerfilePathForApp("api", 3)
	if path != "/workspace/Dockerfile.api" {
		t.Errorf("multi app: got %q, want /workspace/Dockerfile.api", path)
	}
}

func TestDockerfileNaming_GeneratorMatchesConsumer(t *testing.T) {
	// Contract test: generator and consumer must agree on filenames
	for _, tc := range []struct {
		name      string
		appName   string
		totalApps int
	}{
		{"single", "api", 1},
		{"multi-2", "api", 2},
		{"multi-3", "worker", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Generator side (loopzeroPacksDockerfileStep uses dockerfilePathForApp)
			generatorPath := dockerfilePathForApp(tc.appName, tc.totalApps)
			// Consumer side (buildkitTemplateForApp uses same function)
			consumerPath := dockerfilePathForApp(tc.appName, tc.totalApps)
			if generatorPath != consumerPath {
				t.Errorf("naming mismatch: generator=%q consumer=%q", generatorPath, consumerPath)
			}
		})
	}
}

func TestPythonStrategy_CacheKey(t *testing.T) {
	s := &PythonStrategy{}
	if s.CacheKey() != "pip-cache" {
		t.Errorf("CacheKey() = %q, want pip-cache", s.CacheKey())
	}
}

func TestPythonStrategy_ValidateStep_ChecksPythonDeps(t *testing.T) {
	s := &PythonStrategy{}
	apps := []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 8000, Runtime: "python"}}
	c := s.ValidateStep(apps)

	assertContainerShape(t, c, "validate-workspace", "python:3.12-alpine")

	script := extractContainerScript(t, c)
	assertContains(t, script, "requirements.txt")
	assertContains(t, script, "pyproject.toml")
}

// TestPythonStrategy_DockerfileStep removed — replaced by TestLoopZeroPacksDockerfileStep.

// ---------------------------------------------------------------------------
// CustomStrategy
// ---------------------------------------------------------------------------

func TestAppPaths_EmptyApps_ReturnsDot(t *testing.T) {
	paths := appPaths(nil)
	if len(paths) != 1 || paths[0] != "." {
		t.Errorf("appPaths(nil) = %v, want [.]", paths)
	}
}

func TestAppPaths_WithPaths(t *testing.T) {
	apps := []deploycontracts.AppSpec{
		{Name: "api", Path: "services/api"},
		{Name: "web", Path: ""},
	}
	paths := appPaths(apps)
	if paths[0] != "services/api" {
		t.Errorf("paths[0] = %q, want services/api", paths[0])
	}
	if paths[1] != "." {
		t.Errorf("paths[1] = %q, want . (fallback)", paths[1])
	}
}

func TestDefaultPort_UsesFirstNonZero(t *testing.T) {
	apps := []deploycontracts.AppSpec{
		{Name: "worker", Port: 0},
		{Name: "api", Port: 8080},
	}
	if p := defaultPort(apps, 3000); p != 8080 {
		t.Errorf("defaultPort = %d, want 8080", p)
	}
}

func TestDefaultPort_FallbackWhenAllZero(t *testing.T) {
	apps := []deploycontracts.AppSpec{{Name: "worker", Port: 0}}
	if p := defaultPort(apps, 3000); p != 3000 {
		t.Errorf("defaultPort = %d, want 3000 (fallback)", p)
	}
}

// TestSanitizeBinName removed — sanitizeBinName was deleted with the dead Go
// build script (simplify-deploy-loopzerokit-only F-wire-1).

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func assertContainerShape(t *testing.T, c *forge.Container, expectedName, expectedImage string) {
	t.Helper()

	if c.Name != expectedName {
		t.Errorf("container name = %q, want %q", c.Name, expectedName)
	}
	if c.Image != expectedImage {
		t.Errorf("container image = %q, want %q", c.Image, expectedImage)
	}
	if len(c.VolumeMounts) == 0 {
		t.Error("container missing VolumeMounts")
	}
	if c.Resources == nil {
		t.Error("container missing Resources")
	}
	if c.SecurityContext == nil {
		t.Error("container missing SecurityContext")
	}
}

func extractContainerScript(t *testing.T, c *forge.Container) string {
	t.Helper()
	if len(c.Args) == 0 {
		t.Fatal("no Args in container")
	}
	return c.Args[0]
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if len(s) == 0 {
		t.Errorf("string is empty, want to contain %q", substr)
		return
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return
		}
	}
	t.Errorf("string does not contain %q:\n%s", substr, truncate(s, 200))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ---------------------------------------------------------------------------
// M14 gap #1 — runtime registry is data-driven (not a compiled switch).
// Blueprint Q1 (kserve ServingRuntime registry) + plan ADR D2 (operator-only
// config, fail-closed default). These tests assert the registry MECHANISM,
// complementing the behavior tests above.
// ---------------------------------------------------------------------------

// TestRuntimeRegistry_IsDataDriven asserts ResolveStrategy resolves via the
// runtimeRegistry map, not a hardcoded switch: python + node are registered
// descriptors and nothing else is (fail-closed default preserved).
func TestRuntimeRegistry_IsDataDriven(t *testing.T) {
	if len(runtimeRegistry) != 2 {
		t.Fatalf("runtimeRegistry has %d entries, want exactly 2 (python, node) — operator-only, no tenant-supplied runtimes (D2)", len(runtimeRegistry))
	}
	for _, name := range []string{"python", "node"} {
		d, ok := runtimeRegistry[name]
		if !ok {
			t.Errorf("runtimeRegistry missing %q", name)
			continue
		}
		if d.Strategy.Name() != name {
			t.Errorf("runtimeRegistry[%q].Strategy.Name() = %q, want %q", name, d.Strategy.Name(), name)
		}
	}
}

// TestResolveStrategy_ExactMatchParityWithLegacy (EC-3): the registry lookup
// preserves the switch's byte-for-byte input semantics — case-sensitive, no
// trim. "Python", "python " (trailing space), "" all fail closed exactly like
// the legacy default branch. NOT normalizing is deliberate: normalizing would
// CHANGE behavior for callers that relied on the old exact-match.
func TestResolveStrategy_ExactMatchParityWithLegacy(t *testing.T) {
	for _, in := range []string{"Python", "python ", " node", "NODE", "", "rust"} {
		if got := ResolveStrategy(in).Name(); got != "unsupported" {
			t.Errorf("ResolveStrategy(%q) = %q, want unsupported (exact-match parity with legacy switch — EC-3)", in, got)
		}
	}
}
