// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Regression-lock: every Taskfile call to bootstrap-cell-cc.sh passes a --cell
// value the script accepts.
//
// The 1:1 cell unification of 2026-08-09 narrowed the script's accepted cells
// to `0|1`, while taskfiles/drill.yml kept calling it with `--cell default` and
// `--cell 2`. The script exits 1 on those, and drill Phase 5 runs under
// `set -e`, so the drill aborted before its teardown and left a paid stack up
// until the watchdog destroyed it. Nothing went red, because nothing compared
// the callers with the callee.
//
// The accepted set is READ from the script's own `case "$CELL" in` arm rather
// than restated here: a second copy of the list would drift exactly the way the
// callers did.
package repoinvariants

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

const bootstrapCellScript = "infra/helmfile/scripts/bootstrap-cell-cc.sh"

// reCellCaseArm matches the first arm of `case "$CELL" in`, e.g. `  0|1) ;;`.
var reCellCaseArm = regexp.MustCompile(`case "\$CELL" in\s*\n\s*([A-Za-z0-9_|-]+)\)`)

// reCellInvocation matches `bootstrap-cell-cc.sh ... --cell <value>` on one line.
var reCellInvocation = regexp.MustCompile(`bootstrap-cell-cc\.sh\b[^\n]*?--cell\s+([^\s]+)`)

func acceptedBootstrapCells(t *testing.T) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(qtestutil.MustRepoRoot(), bootstrapCellScript))
	if err != nil {
		t.Fatalf("read %s: %v", bootstrapCellScript, err)
	}
	m := reCellCaseArm.FindSubmatch(body)
	if m == nil {
		t.Fatalf("%s: no `case \"$CELL\" in` arm found — the guard cannot learn which cells the script accepts", bootstrapCellScript)
	}
	accepted := map[string]bool{}
	for _, cell := range strings.Split(string(m[1]), "|") {
		accepted[cell] = true
	}
	return accepted
}

func TestTaskfileBootstrapCellCallsUseCellsTheScriptAccepts(t *testing.T) {
	accepted := acceptedBootstrapCells(t)

	var calls int
	for _, tf := range taskfilesToScan {
		body, err := os.ReadFile(filepath.Join(qtestutil.MustRepoRoot(), tf))
		if err != nil {
			t.Fatalf("read %s: %v", tf, err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue // a comment describing the call, not a call
			}
			m := reCellInvocation.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			calls++
			cell := m[1]
			if strings.Contains(cell, "{{") || strings.HasPrefix(cell, "$") {
				continue // templated: resolved at run time, not ours to judge
			}
			if !accepted[cell] {
				t.Errorf("%s:%d calls bootstrap-cell-cc.sh --cell %s; the script accepts only %v and exits 1",
					tf, i+1, cell, sortedCellNames(accepted))
			}
		}
	}
	if calls == 0 {
		t.Fatal("found no bootstrap-cell-cc.sh --cell call in any Taskfile; the guard is measuring nothing")
	}
}

func sortedCellNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
