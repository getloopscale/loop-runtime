// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package lintcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

func treeWith(t *testing.T, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range paths {
		abs := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("module x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestEveryDirectoryWithGoModIsAModule(t *testing.T) {
	root := treeWith(t, "api/go.mod", "pkg/go.mod", "cmd/loop-runtime-ops/go.mod", "operators/api/go.mod")
	mods, err := DiscoverModules(root)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if len(mods) != 4 {
		t.Fatalf("want 4 modules, got %d: %v", len(mods), mods)
	}
	// Sorted, so that the output and the baseline are deterministic.
	if mods[0] != "api" || mods[len(mods)-1] != "pkg" {
		t.Fatalf("the order must be stable: %v", mods)
	}
}

// The hand-written list was the root cause: five modules were left out of it and nobody noticed,
// because the gate stayed green. Deriving from disk is the fix — but the sweep must ignore
// what is not our code, otherwise it turns into noise.
func TestVendorAndNodeModulesAreIgnored(t *testing.T) {
	root := treeWith(t,
		"api/go.mod",
		"api/vendor/github.com/x/go.mod",
		"web/node_modules/pkg/go.mod",
		"infra/pulumi/node_modules/y/go.mod",
	)
	mods, err := DiscoverModules(root)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if len(mods) != 1 || mods[0] != "api" {
		t.Fatalf("a third-party dependency is not our module: %v", mods)
	}
}

// Absence is a FAILURE, never "no modules, all fine".
func TestZeroModulesFails(t *testing.T) {
	if _, err := DiscoverModules(t.TempDir()); !errors.Is(err, ErrNoModules) {
		t.Fatalf("zero modules swept must fail; err=%v", err)
	}
}

// The real population: the measurement that motivated this slice counted EIGHT.
func TestRealTreeFindsTheModules(t *testing.T) {
	mods, err := DiscoverModules(repoRoot)
	if err != nil {
		t.Fatalf("real tree: %v", err)
	}
	if len(mods) < 8 {
		t.Fatalf("the real tree has at least 8 modules; found %d: %v", len(mods), mods)
	}
	found := map[string]bool{}
	for _, m := range mods {
		found[m] = true
	}
	for _, expected := range []string{"api", "pkg", "operators", "operators/api", "cmd/loop-runtime-ops", "infra/tests", "infra/scripts"} {
		if !found[expected] {
			t.Errorf("module %q was not discovered — it is one of those `task lint` did not look at", expected)
		}
	}
}
