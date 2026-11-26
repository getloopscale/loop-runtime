// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testpyramid

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/testinventory"
)

var repoRoot = qtestutil.MustRepoRoot()

// treeWith builds a tree with the requested count of each kind.
func treeWith(t *testing.T, unit, integ, e2e int) string {
	t.Helper()
	root := t.TempDir()
	mk := func(dir string, n int) {
		for k := 0; k < n; k++ {
			p := filepath.Join(root, dir, "f"+string(rune('a'+k))+"_test.go")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("pkg/u", unit)
	mk("api/tests", integ)
	mk("infra/tests/e2e", e2e)
	return root
}

func freeze(t *testing.T, root string) string {
	t.Helper()
	inv, err := testinventory.Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "baseline.json")
	if err := WriteBaseline(p, inv, "2026-08-17", "test"); err != nil {
		t.Fatal(err)
	}
	return p
}

// A missing baseline is a FAILURE, never a skip: without it the ratchet compares nothing.
func TestMissingBaselineIsAFailure(t *testing.T) {
	root := treeWith(t, 8, 1, 1)
	_, err := violationOf(CheckDerived(root, filepath.Join(root, "does-not-exist.json"), DefaultThresholds()))
	if !errors.Is(err, ErrBaselineMissing) {
		t.Fatalf("a missing baseline must fail loudly; err=%v", err)
	}
}

// The frozen tree passes against itself — without this the ratchet would be born red.
func TestFrozenTreePassesAgainstItself(t *testing.T) {
	root := treeWith(t, 8, 1, 1)
	if _, err := violationOf(CheckDerived(root, freeze(t, root), DefaultThresholds())); err != nil {
		t.Fatalf("the measured tree must pass against its own baseline: %v", err)
	}
}

// More e2e than the baseline is a regression, and a regression blocks.
func TestMoreE2EThanBaselineRegresses(t *testing.T) {
	root := treeWith(t, 8, 1, 1)
	base := freeze(t, root)
	// Add e2e after freezing.
	for _, n := range []string{"z1", "z2", "z3"} {
		p := filepath.Join(root, "infra/tests/e2e", n+"_test.go")
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := violationOf(CheckDerived(root, base, DefaultThresholds()))
	if !errors.Is(err, ErrRegressed) {
		t.Fatalf("raising e2e above the baseline is a regression; err=%v out=%+v", err, out)
	}
	if len(out.Regressions) == 0 {
		t.Fatal("the regression must be named, not just counted")
	}
}

// Improving is not regressing. Without this boundary, the ratchet would punish the fix.
func TestImprovingIsNotARegression(t *testing.T) {
	root := treeWith(t, 8, 1, 1)
	base := freeze(t, root)
	for k := 0; k < 10; k++ {
		p := filepath.Join(root, "pkg/u", "extra"+string(rune('a'+k))+"_test.go")
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := violationOf(CheckDerived(root, base, DefaultThresholds())); err != nil {
		t.Fatalf("more unit tests improve the pyramid and must not fail: %v", err)
	}
}

// The distance to the ADR target is REPORTED and does not block — the target was calibrated
// against a file that declared 0.1% e2e, that is, against fiction.
func TestAspirationGapIsReportedNotBlocking(t *testing.T) {
	root := treeWith(t, 5, 1, 4) // 50/10/40 — far above the e2e target
	out, err := violationOf(CheckDerived(root, freeze(t, root), DefaultThresholds()))
	if err != nil {
		t.Fatalf("distance to the ADR target does not block: %v", err)
	}
	if len(out.AspirationGaps) == 0 {
		t.Fatal("the distance to the target must be reported — silencing it would hide the pending decision")
	}
}

// The real population (B-109): the versioned baseline must match the tree that
// is on disk NOW. This test is what keeps the baseline from becoming the next
// file that describes the past.
func TestRealTreeMatchesTheFrozenBaseline(t *testing.T) {
	base := filepath.Join(repoRoot, "infra/quality/test-pyramid-baseline.json")
	out, err := violationOf(CheckDerived(repoRoot, base, DefaultThresholds()))
	if err != nil {
		t.Fatalf("the real tree regressed relative to the baseline: %v (%+v)", err, out.Regressions)
	}
	if out.Total < 900 {
		t.Fatalf("total=%d — the derivation is seeing less than the whole tree", out.Total)
	}
}

// The e2e-specific distance was RATIFIED by the 2026-09-06 ADR (Option A):
// for a product whose purpose IS deploying, ~9% e2e is genuine coverage — the ratchet
// is the enforcement mechanism, and the ADR D7 ≤5% target is withdrawn for the e2e axis.
// The e2e aspiration gap is now SILENCED; unit and integration are still
// reported (they were not relaxed).
func TestE2EAspirationGapIsSilencedAfterADRAmendment(t *testing.T) {
	root := treeWith(t, 5, 1, 4) // 50/10/40 — e2e far above the D7 target
	out, err := violationOf(CheckDerived(root, freeze(t, root), DefaultThresholds()))
	if err != nil {
		t.Fatalf("the ratchet must not fail the frozen tree: %v", err)
	}
	for _, gap := range out.AspirationGaps {
		if strings.Contains(gap, "e2e") {
			t.Fatalf("the e2e gap should be silenced by the 2026-09-06 ADR D7 amendment; found: %q", gap)
		}
	}
	// Counter-test: the unit gap is still reported (unit=50% < target 70%).
	foundUnit := false
	for _, gap := range out.AspirationGaps {
		if strings.Contains(gap, "unit") {
			foundUnit = true
			break
		}
	}
	if !foundUnit {
		t.Fatal("the unit gap must still be reported — the amendment covers e2e, not the whole pyramid")
	}
}
