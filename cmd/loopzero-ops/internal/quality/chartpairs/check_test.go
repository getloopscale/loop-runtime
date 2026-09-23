// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package chartpairs_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/chartpairs"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

func tree(t *testing.T, infraHelm, charts map[string]string, registry string) string {
	t.Helper()
	root := t.TempDir()
	write := func(base, dir, name string) {
		p := filepath.Join(root, base, dir, "Chart.yaml")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("apiVersion: v2\nname: "+name+"\nversion: 0.1.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for dir, name := range infraHelm {
		write("infra/helm", dir, name)
	}
	for dir, name := range charts {
		write("charts", dir, name)
	}
	p := filepath.Join(root, "infra/quality/chart-pairs.txt")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The registry must list exactly the pairs that exist on disk.
func TestRegistryMatchesDisk(t *testing.T) {
	root := tree(t,
		map[string]string{"loopzero-api": "loopzero-api", "gitea": "gitea"},
		map[string]string{"loopzero-api": "loopzero-api", "charts-only": "charts-only"},
		"# registry\nloopzero-api | loopzero-api | infra/helm bootstraps it; charts/ is published\n")

	out, err := chartpairs.Check(chartpairs.Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("registry matches disk; got %v", err)
	}
	if len(out.Pairs) != 1 || out.Pairs[0] != "loopzero-api" {
		t.Errorf("want [loopzero-api], got %v", out.Pairs)
	}
}

// A new pair without a declaration is how the parity contract ages in silence.
func TestUndeclaredPairIsRefused(t *testing.T) {
	root := tree(t,
		map[string]string{"loopzero-api": "loopzero-api", "loopzero-cloud": "loopzero-cloud"},
		map[string]string{"loopzero-api": "loopzero-api", "loopzero-cloud": "loopzero-cloud"},
		"# registry\nloopzero-api | loopzero-api | note\n")

	out, err := chartpairs.Check(chartpairs.Config{RepoRoot: root})
	if !errors.Is(err, chartpairs.ErrUndeclared) {
		t.Fatalf("want ErrUndeclared, got %v", err)
	}
	if len(out.Undeclared) != 1 || out.Undeclared[0] != "loopzero-cloud" {
		t.Errorf("want loopzero-cloud undeclared, got %v", out.Undeclared)
	}
}

// A declaration that outlived its pair: the defect CLAUDE.md had.
func TestStaleDeclarationIsRefused(t *testing.T) {
	root := tree(t,
		map[string]string{"loopzero-api": "loopzero-api"},
		map[string]string{"loopzero-api": "loopzero-api"},
		"# registry\nloopzero-api | loopzero-api | note\nloopzero-ghost | loopzero-ghost | a pair that does not exist\n")

	out, err := chartpairs.Check(chartpairs.Config{RepoRoot: root})
	if !errors.Is(err, chartpairs.ErrStale) {
		t.Fatalf("want ErrStale, got %v", err)
	}
	if len(out.Stale) != 1 || out.Stale[0] != "loopzero-ghost" {
		t.Errorf("want loopzero-ghost stale, got %v", out.Stale)
	}
}

// A conceptual pair (different chart names) is declared, not discovered.
func TestConceptualPairIsCheckedByDirectory(t *testing.T) {
	registry := "# registry\nadmission-policies | loopzero-admission-policies | conceptual\n"
	root := tree(t,
		map[string]string{"admission-policies": "admission-policies"},
		map[string]string{"loopzero-admission-policies": "loopzero-admission-policies"},
		registry)
	out, err := chartpairs.Check(chartpairs.Config{RepoRoot: root})
	if err != nil || len(out.Pairs) != 1 || out.Pairs[0] != "admission-policies↔loopzero-admission-policies" {
		t.Fatalf("want the conceptual pair present, got %v %v", out.Pairs, err)
	}
	root = tree(t, map[string]string{"admission-policies": "admission-policies"}, map[string]string{"x": "x"}, registry)
	if _, err := chartpairs.Check(chartpairs.Config{RepoRoot: root}); !errors.Is(err, chartpairs.ErrStale) {
		t.Fatalf("a conceptual pair missing its published side is stale; got %v", err)
	}
}

func TestMalformedRegistryLineIsRefused(t *testing.T) {
	root := tree(t, map[string]string{"loopzero-api": "loopzero-api"}, map[string]string{"loopzero-api": "loopzero-api"},
		"loopzero-api | old two-field format\n")
	if _, err := chartpairs.Check(chartpairs.Config{RepoRoot: root}); !errors.Is(err, chartpairs.ErrRegistryMissing) {
		t.Fatalf("want ErrRegistryMissing for a two-field line, got %v", err)
	}
}

// No charts at all is refused: zero with zero must not read as "scanned clean".
func TestNoChartsAtAllIsRefused(t *testing.T) {
	root := t.TempDir()
	if _, err := chartpairs.Check(chartpairs.Config{RepoRoot: root}); !errors.Is(err, chartpairs.ErrNothingScanned) {
		t.Fatalf("want ErrNothingScanned, got %v", err)
	}
}

// The real population of this repository.
func TestRealTree(t *testing.T) {
	out, err := chartpairs.Check(chartpairs.Config{RepoRoot: qtestutil.Root(t)})
	if err != nil {
		t.Fatalf("real tree diverges: %v (pairs=%v undeclared=%v stale=%v)",
			err, out.Pairs, out.Undeclared, out.Stale)
	}
	if len(out.Pairs) == 0 {
		t.Fatal("zero pairs measured — green over nothing")
	}
	t.Logf("real population: %d chart(s) in infra/helm, %d in charts/, pairs: %v",
		out.InfraHelm, out.Charts, out.Pairs)
}
