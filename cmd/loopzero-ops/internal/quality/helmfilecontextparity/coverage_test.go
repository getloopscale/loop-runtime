// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmfilecontextparity

import (
	"fmt"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

// The scanned scope is DECLARED — `Taskfile.yml` + `taskfiles/*.yml` — and the
// script invokers stay out for a measured reason, written in `defaultGlobs`.
// This test pins the denominator so it does not shrink in silence: a scan that
// starts reaching less keeps reporting PASS, and the PASS starts meaning less
// without anything warning (B-078).
//
// The number is measured, not chosen. It was 24 on 2026-08-18. On 2026-09-25
// it is 15: the per-cell steps in Taskfile.yml became one step driven by the
// declared cell inventory, and the cell admission releases moved into the cell
// helmfile, so Taskfile.yml went from 12 `--kube-context` lines to 3 while
// taskfiles/*.yml kept 13 (a raw grep reads 25 then and 16 now — the same one
// line of difference on both sides).
func TestDeclaredScopeDenominatorDoesNotShrink(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: qtestutil.Root(t)}))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	total := out.ComparedCount + out.SkippedParameterised
	if total < 15 {
		t.Errorf("only %d invocation(s) reached; the declared scope carried 15 on "+
			"2026-09-25 — either the scan shrank, or invocations left the tree "+
			"without this floor being re-measured", total)
	}
}

// The fraction must appear in a form nobody mistakes for coverage: a PASS over
// 4 of 28 invocations is not a PASS over 28 of 28, and the previous text gave
// both numbers without ever saying one is out of the other.
func TestSummaryStatesTheFractionNotJustTheCounts(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: qtestutil.Root(t)}))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	total := out.ComparedCount + out.SkippedParameterised
	// The assertion must demand the FRACTION, not any " of " — the previous
	// text already said "out of static reach", and a loose check would pass on
	// that phrase with no fraction present. That is what happened to the first
	// version of this test.
	want := fmt.Sprintf("%d of %d", out.ComparedCount, total)
	if !strings.Contains(out.Summary(), want) {
		t.Errorf("the summary does not say %q — without the fraction, a PASS over %d of %d reads as coverage:\n%s",
			want, out.ComparedCount, total, out.Summary())
	}
}
