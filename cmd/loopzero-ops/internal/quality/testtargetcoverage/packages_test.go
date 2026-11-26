// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testtargetcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

// The granularity moved from module to package — B-114. `runsTests` asked
// "is there ANY `go test` in this module?" and never looked at the pattern, so
// `cd infra/tests && go test ./unit/...` marked the whole module covered while
// `scripts/` (12), `config/` (6) and `performance/` (23) ran in no target at all.

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPatternCovers(t *testing.T) {
	cases := []struct {
		pattern, pkg string
		want         bool
	}{
		{"./...", "unit", true},
		{"./...", "a/b/c", true},
		{"./unit/...", "unit", true},
		{"./unit/...", "unit/sub", true},
		{"./unit/...", "scripts", false},
		// The boundary a raw prefix would get wrong: `unitary` is not under `unit`.
		{"./unit/...", "unitary", false},
		{"./unit", "unit", true},
		{"./unit", "unit/sub", false},
	}
	for _, c := range cases {
		if got := patternCovers(c.pattern, c.pkg); got != c.want {
			t.Errorf("patternCovers(%q, %q) = %v; want %v", c.pattern, c.pkg, got, c.want)
		}
	}
}

// Flags do NOT end the pattern list — they come before the patterns in the real form
// (`go test -race -coverprofile=X ./charts/... ./chaos/...`). What ends it is a
// shell operator. The first version of this test encoded the wrong rule, and it
// returned zero patterns precisely for the target that covers five subtrees.
func TestPatternsSurviveFlagsAndStopAtAShellOperator(t *testing.T) {
	got := patternsFor(`cd infra/tests && go test -race -count=1 -coverprofile="$OUT" ./charts/... ./chaos/... 2>&1 | tail -20`, "infra/tests")
	if len(got) != 2 || got[0] != "./charts/..." || got[1] != "./chaos/..." {
		t.Fatalf("the patterns come AFTER the flags and run up to the shell operator; got %v", got)
	}
}

// And the multi-line block, which is the form in which `quality.yml` declares those five.
func TestMultiLineBlockIsUnderstood(t *testing.T) {
	block := "      - |\n        set -e\n        cd infra/tests\n        # comment\n" +
		"        go test -race -coverprofile=\"$OUT\" \\\n          ./charts/... ./chaos/... 2>&1 | tail -20\n"
	got := patternsFor(block, "infra/tests")
	if len(got) != 2 {
		t.Fatalf("a `cd` on one line and the `go test` on another are still the same command; got %v", got)
	}
}

// The exact shape B-092 exposed: a module with `./sub/...` in one target and another subtree
// with no target at all. It is bullet 3 of the DoD.
func TestModuleWithRestrictedPatternLeavesOtherSubtreeUncovered(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.work", "go 1.26\n")
	writeFile(t, root, "infra/tests/go.mod", "module x\n")
	writeFile(t, root, "infra/tests/unit/u_test.go", "package u\n")
	writeFile(t, root, "infra/tests/scripts/s_test.go", "package s\n")
	writeFile(t, root, "taskfiles/test.yml", "  cmds:\n    - cd infra/tests && go test ./unit/...\n")

	out, err := violationOf(Check(Config{
		RepoRoot: root, Taskfiles: []string{"taskfiles/test.yml"},
		Exemptions: []Exemption{}, Now: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
	}))
	if !errors.Is(err, ErrPackageUncovered) {
		t.Fatalf("a subtree with no target must fail even when the module has another target; err=%v out=%+v", err, out)
	}
	joined := strings.Join(out.UncoveredPackages, " ")
	if !strings.Contains(joined, "scripts") {
		t.Fatalf("the uncovered subtree must be NAMED; got %v", out.UncoveredPackages)
	}
	if strings.Contains(joined, "unit") {
		t.Fatalf("the covered subtree must not appear as uncovered; got %v", out.UncoveredPackages)
	}
}

// `./...` covers everything — the overwhelming majority of modules, and the boundary that keeps
// the change from becoming a rule the repository does not satisfy.
func TestWildcardTargetCoversEveryPackage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.work", "go 1.26\n")
	writeFile(t, root, "m/go.mod", "module m\n")
	writeFile(t, root, "m/a/a_test.go", "package a\n")
	writeFile(t, root, "m/b/c/c_test.go", "package c\n")
	writeFile(t, root, "taskfiles/test.yml", "  cmds:\n    - cd m && go test ./...\n")

	if _, err := violationOf(Check(Config{
		RepoRoot: root, Taskfiles: []string{"taskfiles/test.yml"},
		Exemptions: []Exemption{}, Now: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
	})); err != nil {
		t.Fatalf("`go test ./...` covers every package of the module: %v", err)
	}
}

// A package WITHOUT tests needs no target. Requiring one would turn the gate into a request
// to write tests for everything, which is a different conversation — what it claims is narrower:
// what exists is being run.
func TestPackageWithoutTestsNeedsNoTarget(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.work", "go 1.26\n")
	writeFile(t, root, "m/go.mod", "module m\n")
	writeFile(t, root, "m/a/a_test.go", "package a\n")
	writeFile(t, root, "m/notests/x.go", "package notests\n")
	writeFile(t, root, "taskfiles/test.yml", "  cmds:\n    - cd m && go test ./a/...\n")

	if _, err := violationOf(Check(Config{
		RepoRoot: root, Taskfiles: []string{"taskfiles/test.yml"},
		Exemptions: []Exemption{}, Now: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
	})); err != nil {
		t.Fatalf("a package without tests needs no target: %v", err)
	}
}

// Bullet 2 of the DoD: a subtree that requires a cluster gets in through a NAMED exemption with its reason —
// not by the module's pattern accidentally covering it.
func TestPackageExemptionRequiresAReason(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.work", "go 1.26\n")
	writeFile(t, root, "m/go.mod", "module m\n")
	writeFile(t, root, "m/e2e/e_test.go", "package e2e\n")
	writeFile(t, root, "taskfiles/test.yml", "  cmds:\n    - cd m && go test ./nothing/...\n")

	base := Config{
		RepoRoot: root, Taskfiles: []string{"taskfiles/test.yml"},
		Now: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
	}

	// Without a reason: refused.
	withoutReason := base
	withoutReason.Exemptions = []Exemption{{Module: "m", Packages: []string{"e2e"}, Sunset: "2099-12-31"}}
	if _, err := violationOf(Check(withoutReason)); !errors.Is(err, ErrExemptionWithoutReason) {
		t.Fatalf("a package exemption without a reason must be refused; err=%v", err)
	}

	// With a reason: passes.
	withReason := base
	withReason.Exemptions = []Exemption{{
		Module: "m", Packages: []string{"e2e"}, Sunset: "2099-12-31",
		Reason: "requires a running cluster; runs through the e2e target, not the unit suite",
	}}
	if _, err := violationOf(Check(withReason)); err != nil {
		t.Fatalf("a package exemption with a reason and a sunset must pass: %v", err)
	}
}

// The real population (B-109): the tree that ships must satisfy the new granularity,
// otherwise the change would be a rule the repository itself does not meet.
func TestRealTreeSatisfiesPackageGranularity(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: qtestutil.Root(t)}))
	if err != nil {
		t.Fatalf("the real tree must satisfy package granularity; err=%v\n  uncovered: %v", err, out.UncoveredPackages)
	}
	if len(out.Modules) == 0 {
		t.Fatal("zero modules scanned — the scan verified nothing")
	}
}
