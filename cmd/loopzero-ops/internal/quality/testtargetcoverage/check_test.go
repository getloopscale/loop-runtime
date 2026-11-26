// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testtargetcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

// tree writes a synthetic repo: one go.mod per named module, plus a taskfile.
func tree(t *testing.T, taskfile string, modules ...string) Config {
	t.Helper()
	root := t.TempDir()
	for _, m := range modules {
		dir := filepath.Join(root, filepath.FromSlash(m))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+m+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "taskfiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "taskfiles", "test.yml"), []byte(taskfile), 0o644); err != nil {
		t.Fatal(err)
	}
	return Config{
		RepoRoot:   root,
		Taskfiles:  []string{"taskfiles/test.yml"},
		Exemptions: []Exemption{},
	}
}

func TestCheck_EveryModuleCoveredPasses(t *testing.T) {
	cfg := tree(t, "cmds:\n  - cd a && go test ./...\n  - cd b && go test ./...\n", "a", "b")
	out, err := violationOf(Check(cfg))
	if err != nil {
		t.Fatalf("err = %v; uncovered=%v", err, out.Uncovered)
	}
	if len(out.Covered) != 2 {
		t.Errorf("covered = %v; want both modules", out.Covered)
	}
}

func TestCheck_UncoveredModuleFailsAndIsNamed(t *testing.T) {
	cfg := tree(t, "cmds:\n  - cd a && go test ./...\n", "a", "b")
	out, err := violationOf(Check(cfg))
	if !errors.Is(err, ErrModuleUncovered) {
		t.Fatalf("err = %v; want ErrModuleUncovered", err)
	}
	if len(out.Uncovered) != 1 || out.Uncovered[0] != "b" {
		t.Errorf("uncovered = %v; want [b] — a gate that cannot name WHICH module forces a hunt", out.Uncovered)
	}
}

// The exact false positive that let the defect live: quality:hermetic did
// `cd cmd/loopzero-ops && go build ./...` and was read as covering the module.
func TestCheck_BuildIsNotTest(t *testing.T) {
	cfg := tree(t, "cmds:\n  - cd a && go build ./...\n", "a")
	out, err := violationOf(Check(cfg))
	if !errors.Is(err, ErrModuleUncovered) {
		t.Fatalf("err = %v; `go build` must NOT count as running tests — that is exactly how "+
			"cmd/loopzero-ops looked covered while no test of it ever ran", err)
	}
	if len(out.Uncovered) != 1 {
		t.Errorf("uncovered = %v; want the module", out.Uncovered)
	}
}

// A module outside go.work needs GOWORK=off; the prefix must not hide it.
func TestCheck_GoworkOffPrefixStillCounts(t *testing.T) {
	cfg := tree(t, "cmds:\n  - cd tools/x && GOWORK=off go test ./...\n", "tools/x")
	if _, err := violationOf(Check(cfg)); err != nil {
		t.Fatalf("err = %v; `GOWORK=off go test` is running tests", err)
	}
}

func TestCheck_ExemptionCoversItsModule(t *testing.T) {
	cfg := tree(t, "cmds:\n  - cd a && go test ./...\n", "a", "b")
	cfg.Exemptions = []Exemption{{Module: "b", Reason: "zero *_test.go on disk", Sunset: "2099-12-31"}}
	out, err := violationOf(Check(cfg))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(out.Exempt) != 1 || out.Exempt[0] != "b" {
		t.Errorf("exempt = %v; want [b]", out.Exempt)
	}
}

// An exemption without a reason is the shape this gate exists to refuse: it
// records the outcome of a decision while erasing the decision.
func TestCheck_ExemptionWithoutReasonIsAnError(t *testing.T) {
	cfg := tree(t, "cmds:\n  - cd a && go test ./...\n", "a", "b")
	cfg.Exemptions = []Exemption{{Module: "b", Reason: "  ", Sunset: "2099-12-31"}}
	if _, err := violationOf(Check(cfg)); !errors.Is(err, ErrExemptionWithoutReason) {
		t.Fatalf("err = %v; want ErrExemptionWithoutReason", err)
	}
}

func TestCheck_NoModulesIsAnError(t *testing.T) {
	cfg := tree(t, "cmds:\n  - echo hi\n")
	if _, err := violationOf(Check(cfg)); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("err = %v; a run that found zero modules verified nothing", err)
	}
}

func TestCheck_MissingTaskfileIsAnError(t *testing.T) {
	cfg := tree(t, "cmds:\n  - cd a && go test ./...\n", "a")
	cfg.Taskfiles = []string{"taskfiles/absent.yml"}
	if _, err := violationOf(Check(cfg)); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("err = %v; scanning fewer files than intended shrinks what the gate can see", err)
	}
}

// Against the real tree, which this slice is fixing.
func TestCheck_RealTreeIsFullyCovered(t *testing.T) {
	root := qtestutil.Root(t)
	out, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("err = %v; uncovered=%v", err, out.Uncovered)
	}
	// There were nine; there have been EIGHT since 2026-08-19. `tools/loopzero-dockerfile-lint` was removed
	// (B-157): it validated a user-supplied Dockerfile, and that path went away in T2.2 —
	// the build generates a canonical Dockerfile in-process, so no path invoked it.
	//
	// Lowering the floor is only legitimate when the cause is known and deliberate, and that is why
	// it is written here: a floor that gives way with no recorded reason stops being a floor.
	if len(out.Modules) < 8 {
		t.Errorf("modules = %d (%v); the tree has carried eight since the B-157 removal — a "+
			"scan that found fewer is measuring less than the tree", len(out.Modules), out.Modules)
	}
	for _, want := range []string{"cmd/loopzero-ops"} {
		var found bool
		for _, c := range out.Covered {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is not covered; it is the module this item was filed about", want)
		}
	}
}

// LOW-1: the prefix boundary is the discipline that keeps `operators` from being
// marked covered by `operators/api`'s line. An independent review measured that
// dropping the trailing space survived every other test in this file, so it is
// pinned here in BOTH directions.
func TestCheck_PrefixBoundaryIsRespected(t *testing.T) {
	cfg := tree(t, "cmds:\n  - cd operators/api && go test ./...\n", "operators", "operators/api")
	out, err := violationOf(Check(cfg))
	if !errors.Is(err, ErrModuleUncovered) {
		t.Fatalf("err = %v; `operators` has no target of its own and must NOT be covered by "+
			"`operators/api`'s line. covered=%v", err, out.Covered)
	}
	if len(out.Uncovered) != 1 || out.Uncovered[0] != "operators" {
		t.Errorf("uncovered = %v; want [operators]", out.Uncovered)
	}

	// And the reverse: the parent's line must not cover the child.
	cfg2 := tree(t, "cmds:\n  - cd operators && go test ./...\n", "operators", "operators/api")
	out2, err2 := violationOf(Check(cfg2))
	if !errors.Is(err2, ErrModuleUncovered) {
		t.Fatalf("err = %v; `operators/api` is a separate module and needs its own target. covered=%v",
			err2, out2.Covered)
	}
}

// MEDIUM-2: text that never executes must not count as coverage.
func TestCheck_NonExecutingTextDoesNotCount(t *testing.T) {
	for name, tf := range map[string]string{
		"yaml comment": "cmds:\n  # - cd a && go test ./...\n  - echo hi\n",
		"echoed":       "cmds:\n  - echo \"cd a && go test ./...\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := violationOf(Check(tree(t, tf, "a"))); !errors.Is(err, ErrModuleUncovered) {
				t.Errorf("err = %v; %s does not run anything", err, name)
			}
		})
	}
}

// The mixed line that defeated the build-is-not-test rule: `go test` for a
// DIFFERENT module, later on the same line, marked this one covered.
func TestCheck_GoTestForAnotherModuleDoesNotCount(t *testing.T) {
	cfg := tree(t, "cmds:\n  - cd a && go build ./... && cd ../b && go test ./...\n", "a", "b")
	out, err := violationOf(Check(cfg))
	if !errors.Is(err, ErrModuleUncovered) {
		t.Fatalf("err = %v; `a` is only BUILT here. covered=%v", err, out.Covered)
	}
	// The property under test is that `a` is NOT covered. `b` is uncovered too,
	// because the line writes `cd ../b` and the matcher requires `cd b` — that
	// is incidental to this fixture, and asserting on it would pin an unrelated
	// behaviour into a test about build-vs-test.
	var aUncovered bool
	for _, m := range out.Uncovered {
		if m == "a" {
			aUncovered = true
		}
	}
	if !aUncovered {
		t.Errorf("uncovered = %v; `a` must be there — it is only built, and the `go test` on this "+
			"line belongs to another module", out.Uncovered)
	}
}

// HIGH-1: the read-only reference zone holds cloned peer projects; one with a
// go.mod was being demanded to have a test target.
func TestCheck_PrunedTreesAreNotModules(t *testing.T) {
	cfg := tree(t, "cmds:\n  - cd a && go test ./...\n",
		"a", ".claude/knowledge-base/references/peer", "worktrees/wt/mod", "vendor/x")
	out, err := violationOf(Check(cfg))
	if err != nil {
		t.Fatalf("err = %v; cloned peers and worktrees are not repo modules. uncovered=%v",
			err, out.Uncovered)
	}
	if len(out.Modules) != 1 || out.Modules[0] != "a" {
		t.Errorf("modules = %v; want only [a]", out.Modules)
	}
}

// LOW-4: an exemption with no date outlives its reason, and nothing detects it.
func TestCheck_ExemptionSunsetIsEnforced(t *testing.T) {
	base := func() Config {
		c := tree(t, "cmds:\n  - cd a && go test ./...\n", "a", "b")
		c.Now = time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
		return c
	}
	c := base()
	c.Exemptions = []Exemption{{Module: "b", Reason: "no tests"}}
	if _, err := violationOf(Check(c)); !errors.Is(err, ErrExemptionWithoutSunset) {
		t.Errorf("err = %v; want ErrExemptionWithoutSunset", err)
	}
	c = base()
	c.Exemptions = []Exemption{{Module: "b", Reason: "no tests", Sunset: "2026-01-01"}}
	if _, err := violationOf(Check(c)); !errors.Is(err, ErrExemptionExpired) {
		t.Errorf("err = %v; want ErrExemptionExpired", err)
	}
	c = base()
	c.Exemptions = []Exemption{{Module: "b", Reason: "no tests", Sunset: "2099-12-31"}}
	if _, err := violationOf(Check(c)); err != nil {
		t.Errorf("err = %v; an unexpired exemption with a reason must pass", err)
	}
}
