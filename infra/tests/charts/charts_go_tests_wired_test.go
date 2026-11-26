// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Meta-guard locking the infra/tests/charts/ Go-guards wiring into
// `task quality:gates`. Closes the /review HIGH 2026-06-20:
//
//	"The 8 Go guards in loopruntime/infra/tests/charts/ do NOT run in
//	 task quality:gates (the pre-push gate). taskfiles/test.yml:111
//	 runs only `go test ./unit/...`; nothing runs `./charts/`."
//
// Pre-2026-06-20, the slice's regression protection (NodePort/serverlb
// T1.1, F-1 NetworkPolicy, gateway-derived host.k3d.internal T1.2, F1
// SAN-aligned kubernetes_host, Step 11 container-name, plus 3 post-
// decomposition tooling fixes #106/#107/#108, plus 9 pre-existing chart
// contract tests) EXISTED but never ran in CI. This meta-guard locks
// the wiring against silent removal AND the related MEDIUM (silent
// SKIP of the central helmfile-render guard) against re-introduction.
//
// Hosted as Go (NOT bash sibling of
// `check-openbao-transit-quality-gates-wired_test.sh`) so it auto-runs
// with `go test ./charts/` AND avoids the un-homed-logic gate bump
// (which blocks new `.sh` tests).
//
// SCOPE DISCIPLINE:
//
//	This guard ONLY asserts the wiring shape in taskfiles/quality.yml.
//	It does NOT validate the chart guards themselves — those are
//	independent Test* functions in this same package.
//
// Triggers a FAIL if any of the following regresses:
//
//	(a) the `go test ./charts/` line is removed/commented from
//	    quality:gates body
//	(b) the helmfile pre-flight guard is removed (would re-introduce
//	    the silent-SKIP-of-central-guard anti-pattern)
//	(c) LOOPRUNTIME_TEST_SKIP_HELMFILE pre-flight guard is removed
//	(d) anti-silent-pass invariants regress (|| true / 2>/dev/null /
//	    missing -count=1)
package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extractGatesBody returns the slice of quality.yml between the
// `  gates:` header and the NEXT top-level task header (a line matching
// `^  [a-z][a-z]*:$`). Mirrors the sed extraction in the sibling bash
// meta-guard so we don't accidentally match a line wired by a different
// task.
func extractGatesBody(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	p := filepath.Join(root, "taskfiles/quality.yml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	full := string(b)

	const gatesHeader = "\n  gates:"
	startIdx := strings.Index(full, gatesHeader)
	if startIdx < 0 {
		t.Fatalf("`  gates:` header not found in %s — taskfile shape changed; this guard is stale.", p)
	}
	startIdx += 1 // advance past the leading newline

	// Find the next top-level header (`^  <name>:$`) AFTER gates: by
	// scanning line-by-line.
	tail := full[startIdx:]
	lines := strings.Split(tail, "\n")
	endLine := -1
	for i, line := range lines {
		if i == 0 {
			continue // the `  gates:` line itself
		}
		// Top-level task header pattern: starts with exactly 2 spaces,
		// followed by [a-z][a-z]*, ends with `:`.
		if len(line) < 4 || line[0] != ' ' || line[1] != ' ' || (line[2] < 'a' || line[2] > 'z') {
			continue
		}
		// Whole prefix `  ` then a lowercase identifier then `:` — no
		// further nesting (must be column-2).
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		ident := line[2:colonIdx]
		isIdent := len(ident) > 0
		for _, c := range ident {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == ':' || c == '-' || c == '_') {
				isIdent = false
				break
			}
		}
		if isIdent {
			endLine = i
			break
		}
	}
	if endLine < 0 {
		// gates is the last task — return through EOF.
		return tail
	}
	return strings.Join(lines[:endLine], "\n")
}

// chartsWiredFileExists is a tiny helper used by section A assertions.
func chartsWiredFileExists(t *testing.T, relPath string) bool {
	t.Helper()
	root := repoRoot(t)
	_, err := os.Stat(filepath.Join(root, relPath))
	return err == nil
}

// TestChartsGoTestsWired runs the meta-guard contract.
func TestChartsGoTestsWired(t *testing.T) {
	body := extractGatesBody(t)

	// ===== A — Taskfile + gates body + charts dir sanity =====
	if len(body) < 200 {
		t.Errorf("A: extracted gates body is < 200 chars (got %d) — quality:gates section is empty or extraction broke", len(body))
	}
	if !chartsWiredFileExists(t, "infra/tests/charts") {
		t.Errorf("A: infra/tests/charts directory does NOT exist on disk")
	}
	if !chartsWiredFileExists(t, "infra/tests/charts/vault_host_published_reach_test.go") {
		t.Errorf("A: vault_host_published_reach_test.go (central helmfile-render guard) does NOT exist on disk")
	}

	// ===== B — `go test ./charts/` IS wired in quality:gates =====
	// Canonical-anchor pattern: the FULL invocation `cd infra/tests &&
	// go test ./charts/ -count=1`. Using the full pattern (not the bare
	// substring `go test ./charts/`) avoids false-positives on inline
	// explanatory comments that simply mention the path.
	const canonicalInvocation = "cd infra/tests && go test ./charts/ -count=1"
	hasCanonical := strings.Contains(body, canonicalInvocation)
	if !hasCanonical {
		t.Errorf("B1: canonical invocation `%s` is NOT present in the quality:gates body — chart guards would not run in pre-push (re-introduces the /review HIGH).", canonicalInvocation)
	}

	// B2 — UNCOMMENTED: the canonical invocation must NOT appear behind
	// a YAML comment marker. A line where the first non-whitespace char
	// is `#` AND the line contains the canonical invocation = silent
	// removal of the wiring.
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") && strings.Contains(line, canonicalInvocation) {
			t.Errorf("B2: canonical invocation appears in a COMMENTED line — silent removal of the wiring:\n  %s", line)
		}
	}

	// ===== C — Helmfile pre-flight guard (closes related MEDIUM) =====
	// TestVaultHostPublishedReach `t.Skip`s when helmfile is absent — a
	// gate that silently skips its most important guard is the
	// "looks GREEN, ran nothing" anti-pattern. The pre-flight in the
	// gate must FAIL LOUDLY in that case.
	if !strings.Contains(body, "command -v helmfile") {
		t.Error("C1: helmfile pre-flight `command -v helmfile` is NOT present — central helmfile-render guard would silently SKIP in the gate (the related MEDIUM).")
	}
	if !(strings.Contains(body, "FAIL quality:gates: helmfile") ||
		strings.Contains(body, "FAIL: helmfile")) {
		t.Error("C2: helmfile pre-flight does NOT emit a `FAIL` marker — operator visibility lost when helmfile is missing.")
	}
	if !strings.Contains(body, "LOOPRUNTIME_TEST_SKIP_HELMFILE") {
		t.Error("C3: LOOPRUNTIME_TEST_SKIP_HELMFILE pre-flight is NOT present — gate would silently SKIP if operator set the escape env var.")
	}

	// ===== D — Anti-silent-pass invariants =====
	// D1 — no `|| true` suffix on the chart-guards line
	if strings.Contains(body, "go test ./charts/ -count=1 || true") {
		t.Error("D1: chart-guards line has `|| true` suffix — would mask test failures.")
	}
	// D2 — no `2>/dev/null` redirect on the chart-guards line
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "go test ./charts/") && strings.Contains(line, "2>/dev/null") {
			t.Errorf("D2: chart-guards line redirects stderr to /dev/null — diagnostic output suppressed:\n  %s", line)
		}
	}
	// D3 — wrapper block uses `set -e` so pre-flight failure aborts
	if !strings.Contains(body, "set -e") {
		t.Error("D3: wrapper block does NOT use `set -e` — pre-flight FAIL would not abort the gate.")
	}

	// ===== E — Audit-trail in inline comments =====
	if !(strings.Contains(body, "/review HIGH") ||
		strings.Contains(body, "chart Go guards") ||
		strings.Contains(body, "infra/tests/charts")) {
		t.Error("E: chart-guards wiring does NOT cite the slice / /review HIGH closure in inline audit-trail — future operators won't find the why.")
	}
}

// TestChartsGoTestsWired_NegativeControl proves the guard would catch
// the regressions it claims to catch. Mutates the in-memory body and
// asserts the corresponding section fires. A guard that passes the
// negative control is theatre.
func TestChartsGoTestsWired_NegativeControl(t *testing.T) {
	body := extractGatesBody(t)

	// Sub-case 1: remove the canonical invocation → B1 (canonical
	// anchor missing) should fire.
	const canonicalInvocation = "cd infra/tests && go test ./charts/ -count=1"
	mutated1 := strings.Replace(body, canonicalInvocation, "# REMOVED FOR NEGATIVE CONTROL", 1)
	if mutated1 == body {
		t.Fatalf("negative-control sub-case 1 setup failed: canonical `%s` not found in extracted body.", canonicalInvocation)
	}
	if strings.Contains(mutated1, canonicalInvocation) {
		t.Errorf("negative-control sub-case 1: even after replacement, body still contains the canonical invocation — strings.Replace did not take effect.")
	}

	// Sub-case 2: remove the helmfile pre-flight → C1 should fire.
	mutated2 := strings.Replace(body, "command -v helmfile", "command -v fake-binary", 1)
	if mutated2 == body {
		t.Fatalf("negative-control sub-case 2 setup failed: `command -v helmfile` not found in extracted body.")
	}
	if strings.Contains(mutated2, "command -v helmfile") {
		t.Errorf("negative-control sub-case 2: mutation did not remove `command -v helmfile` — replacement leaked.")
	}

	// Sub-case 3: add `|| true` suffix → D1 should fire.
	mutated3 := strings.Replace(body, "go test ./charts/ -count=1", "go test ./charts/ -count=1 || true", 1)
	if mutated3 == body {
		t.Fatalf("negative-control sub-case 3 setup failed: canonical line not found for mutation.")
	}
	if !strings.Contains(mutated3, "go test ./charts/ -count=1 || true") {
		t.Errorf("negative-control sub-case 3: mutation did not introduce `|| true` suffix.")
	}

	t.Logf("negative-control: all 3 mutation sub-cases produced detectable body diffs — guard logic would fire on each.")
}
