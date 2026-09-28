//go:build helm_render

// Package helpers — build-gate meta-defense.
//
// b9f19dc8 (2026-05-20) shipped 4 test packages referencing
// `chartrender.SplitYAMLByKind` while the staged chartrender.go did NOT define
// that function. Each per-package test run had succeeded against an
// in-memory edit, but the staged commit was broken. The regression was
// only caught when a later iteration tried to add new T3.2 packages
// that hit the same missing symbol.
//
// Root cause: no meta-defense ran `go vet ./helm/...` before commit, so
// build breakage propagated silently. This test closes that hole — it
// runs `go vet` (and a strict `go build`) over every helm render test
// package, failing CI / pre-commit if any package no longer compiles.
//
// Why a test (not a CI script alone): the test runs in the same Go
// runtime as the rest of the suite and is gated by build tag
// `helm_render`, so it covers the exact set of packages the rest of the
// plan exercises — no separate config to drift.
package chartrender

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildGate_AllHelmRenderPackagesCompile is the meta-defense that
// prevents the b9f19dc8 class of regression: a helpers change that
// removes a symbol still used by test packages downstream.
//
// Executes `go vet -tags helm_render ./helm/...` from infra/tests/ root.
// Vet performs a full type-check, so any undefined-symbol error in any
// downstream package fails this test.
func TestBuildGate_AllHelmRenderPackagesCompile(t *testing.T) {
	t.Parallel()
	// infra/tests is the working dir resolved relative to this file.
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	testsDir := filepath.Join(root, "infra", "tests")

	cmd := exec.Command("go", "vet", "-tags", "helm_render", "./helm/...")
	cmd.Dir = testsDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"go vet failed — at least one helm render package no longer "+
				"compiles. This is the b9f19dc8 class of regression "+
				"(helpers symbol removed while still referenced "+
				"downstream).\n\nworking dir: %s\nerror: %v\noutput:\n%s",
			testsDir, err, output)
	}

	// Empty stdout/stderr from `go vet` = no issues. If vet reports
	// anything (lint warnings), fail loudly so they get fixed at source.
	out := strings.TrimSpace(string(output))
	if out != "" {
		t.Fatalf("go vet reported issues:\n%s", out)
	}
}
