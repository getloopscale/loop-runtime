// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmoverlapparity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/chartpairs"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// writeChart writes a stub Chart.yaml. helper for tests.
func writeChart(t *testing.T, dir, name, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "apiVersion: v2\nname: " + name + "\nversion: " + version + "\ndescription: test\n"
	if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
}

// fixturePairs are the pairs the fixtures lay out; Check reads the registry
// only when Config.Pairs is nil.
var fixturePairs = []Pair{
	{BootstrapDir: "loopzero-api", PublishedDir: "loopzero-api"},
	{BootstrapDir: "loopzero-cloud", PublishedDir: "loopzero-cloud"},
	{BootstrapDir: "admission-policies", PublishedDir: "loopzero-admission-policies"},
}

func mkRepoWithPair(t *testing.T, bootstrapName, bootstrapVer, publishedName, publishedVer string) string {
	t.Helper()
	root := t.TempDir()
	writeChart(t, filepath.Join(root, "infra/helm", bootstrapName), bootstrapName, bootstrapVer)
	writeChart(t, filepath.Join(root, "charts", publishedName), publishedName, publishedVer)
	return root
}

func TestPass_VersionsEqual(t *testing.T) {
	root := t.TempDir()
	for _, p := range fixturePairs {
		writeChart(t, filepath.Join(root, "infra/helm", p.BootstrapDir), p.BootstrapDir, "0.2.0")
		writeChart(t, filepath.Join(root, "charts", p.PublishedDir), p.PublishedDir, "0.2.0")
	}
	got, err := Check(Config{RepoRoot: root, Pairs: fixturePairs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Pass {
		t.Errorf("Pass=false, expected true. Violations: %v", got.Violations)
	}
	if len(got.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %v", len(got.Violations), got.Violations)
	}
}

func TestPass_BootstrapBehindPublished(t *testing.T) {
	root := t.TempDir()
	for _, p := range fixturePairs {
		writeChart(t, filepath.Join(root, "infra/helm", p.BootstrapDir), p.BootstrapDir, "0.1.0")
		writeChart(t, filepath.Join(root, "charts", p.PublishedDir), p.PublishedDir, "0.3.0")
	}
	got, err := Check(Config{RepoRoot: root, Pairs: fixturePairs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Pass {
		t.Errorf("Pass=false, expected true (bootstrap < published is acceptable). Violations: %v", got.Violations)
	}
}

func TestFail_BootstrapAheadPublished(t *testing.T) {
	root := mkRepoWithPair(t, "loopzero-api", "0.5.0", "loopzero-api", "0.2.0")
	// Other pairs missing -> additional violations; keep them in parity to isolate drift.
	for _, p := range fixturePairs {
		if p.BootstrapDir == "loopzero-api" {
			continue
		}
		writeChart(t, filepath.Join(root, "infra/helm", p.BootstrapDir), p.BootstrapDir, "0.1.0")
		writeChart(t, filepath.Join(root, "charts", p.PublishedDir), p.PublishedDir, "0.1.0")
	}
	got, err := Check(Config{RepoRoot: root, Pairs: fixturePairs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Pass {
		t.Errorf("Pass=true, expected false. Violations: %v", got.Violations)
	}
	if len(got.Violations) != 1 {
		t.Errorf("expected 1 violation, got %d: %v", len(got.Violations), got.Violations)
	}
	v := got.Violations[0]
	if v.BootstrapVersion != "0.5.0" || v.PublishedVersion != "0.2.0" {
		t.Errorf("violation versions wrong: %+v", v)
	}
	if v.Reason != "drift: bootstrap chart leads published" {
		t.Errorf("violation reason wrong: %q", v.Reason)
	}
}

func TestFail_BootstrapChartYamlMissing(t *testing.T) {
	root := t.TempDir()
	// Only create published; bootstrap dir absent
	for _, p := range fixturePairs {
		writeChart(t, filepath.Join(root, "charts", p.PublishedDir), p.PublishedDir, "0.2.0")
	}
	got, err := Check(Config{RepoRoot: root, Pairs: fixturePairs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Pass {
		t.Errorf("Pass=true, expected false")
	}
	if len(got.Violations) != 3 {
		t.Errorf("expected 3 violations (one per pair), got %d", len(got.Violations))
	}
}

func TestFail_PublishedChartYamlMissing(t *testing.T) {
	root := t.TempDir()
	for _, p := range fixturePairs {
		writeChart(t, filepath.Join(root, "infra/helm", p.BootstrapDir), p.BootstrapDir, "0.2.0")
	}
	got, err := Check(Config{RepoRoot: root, Pairs: fixturePairs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Pass {
		t.Errorf("Pass=true, expected false")
	}
	if len(got.Violations) != 3 {
		t.Errorf("expected 3 violations, got %d", len(got.Violations))
	}
}

func TestFail_InvalidSemver(t *testing.T) {
	root := t.TempDir()
	for _, p := range fixturePairs {
		writeChart(t, filepath.Join(root, "infra/helm", p.BootstrapDir), p.BootstrapDir, "garbage")
		writeChart(t, filepath.Join(root, "charts", p.PublishedDir), p.PublishedDir, "0.2.0")
	}
	got, err := Check(Config{RepoRoot: root, Pairs: fixturePairs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Pass {
		t.Errorf("Pass=true, expected false (invalid semver)")
	}
}

func TestPrereleaseSemver_BootstrapPrereleaseBehindRelease(t *testing.T) {
	// EC-10 (SHOULD TEST) — 0.1.0-rc1 < 0.1.0 per semver spec
	root := t.TempDir()
	for _, p := range fixturePairs {
		writeChart(t, filepath.Join(root, "infra/helm", p.BootstrapDir), p.BootstrapDir, "0.1.0-rc1")
		writeChart(t, filepath.Join(root, "charts", p.PublishedDir), p.PublishedDir, "0.1.0")
	}
	got, err := Check(Config{RepoRoot: root, Pairs: fixturePairs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Pass {
		t.Errorf("Pass=false, expected true (0.1.0-rc1 < 0.1.0). Violations: %v", got.Violations)
	}
}

func TestPrereleaseSemver_BootstrapReleaseAheadPrerelease(t *testing.T) {
	root := t.TempDir()
	for _, p := range fixturePairs {
		writeChart(t, filepath.Join(root, "infra/helm", p.BootstrapDir), p.BootstrapDir, "0.1.0")
		writeChart(t, filepath.Join(root, "charts", p.PublishedDir), p.PublishedDir, "0.1.0-rc1")
	}
	got, err := Check(Config{RepoRoot: root, Pairs: fixturePairs})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Pass {
		t.Errorf("Pass=true, expected false (0.1.0 release > 0.1.0-rc1 prerelease)")
	}
}

func TestCompareSemver_TableDriven(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.1.0", "0.2.0", -1},
		{"0.2.0", "0.1.0", 1},
		{"1.0.0", "0.99.99", 1},
		{"0.0.1", "0.0.2", -1},
		{"0.1.0-rc1", "0.1.0", -1},
		{"0.1.0", "0.1.0-rc1", 1},
		{"0.1.0-rc1", "0.1.0-rc2", -1},
		{"1.0.0+build1", "1.0.0+build2", 0},  // build metadata ignored
		{"0.1.0", "0.10.0", -1},              // numeric, not lexical
		{"0.1.0-rc.2", "0.1.0-rc.10", -1},    // semver §11: numeric identifiers compare numerically
		{"0.1.0-alpha", "0.1.0-alpha.1", -1}, // semver §11: fewer fields sort first
	}
	for _, tc := range cases {
		got, err := compareSemver(tc.a, tc.b)
		if err != nil {
			t.Errorf("compareSemver(%q,%q) error: %v", tc.a, tc.b, err)
			continue
		}
		if got != tc.want {
			t.Errorf("compareSemver(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// With no override the pairs come from the registry — the one list the
// chart-pairs gate keeps honest against disk.
func TestDeclaredPairsComeFromTheRegistry(t *testing.T) {
	root := qtestutil.Root(t)
	got, err := declaredPairs(Config{RepoRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	want, err := chartpairs.LoadRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || len(got) != len(want) {
		t.Fatalf("got %d pair(s), registry declares %d", len(got), len(want))
	}
	for i := range want {
		if got[i].BootstrapDir != want[i].Bootstrap || got[i].PublishedDir != want[i].Published {
			t.Errorf("pair %d: got %+v, registry %+v", i, got[i], want[i])
		}
	}
}
