// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// switchCaseGates parses the dispatch switch in quality_subcmd.go and returns
// the set of gate names it dispatches (excluding the --help/-h/help meta cases
// and the default). This is the authoritative gate set the registry must match.
func switchCaseGates(t *testing.T) map[string]bool {
	t.Helper()
	src, err := os.ReadFile("quality_subcmd.go")
	if err != nil {
		t.Fatalf("read quality_subcmd.go: %v", err)
	}
	caseRe := regexp.MustCompile(`(?m)^\s*case "([a-z0-9-]+)":`)
	meta := map[string]bool{"--help": true, "-h": true, "help": true, "--list": true}
	out := map[string]bool{}
	for _, m := range caseRe.FindAllStringSubmatch(string(src), -1) {
		name := m[1]
		if meta[name] {
			continue
		}
		out[name] = true
	}
	return out
}

// T1 / T4 — the registry must cover exactly the switch case set (drift guard).
func TestGateRegistry_CoversAllSwitchCases(t *testing.T) {
	sw := switchCaseGates(t)
	reg := gateRegistrySet()

	var missing, extra []string
	for name := range sw {
		if !reg[name] {
			missing = append(missing, name)
		}
	}
	for name := range reg {
		if !sw[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("registry MISSING gates present in the dispatch switch: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("registry has EXTRA gates absent from the dispatch switch: %v", extra)
	}
	if len(sw) != len(reg) {
		t.Errorf("gate count mismatch: switch=%d registry=%d", len(sw), len(reg))
	}
}

// T1 — every registry entry is well-formed: non-empty name + non-empty summary, unique names.
func TestGateRegistry_WellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, g := range gateRegistry {
		if g.Name == "" {
			t.Error("registry entry with empty Name")
		}
		if strings.TrimSpace(g.Summary) == "" {
			t.Errorf("gate %q has empty Summary", g.Name)
		}
		if seen[g.Name] {
			t.Errorf("duplicate gate name in registry: %q", g.Name)
		}
		seen[g.Name] = true
	}
}

// T2 — --names emits the sorted name set, one per line, byte-stable across runs.
func TestQualityList_NamesSortedStable(t *testing.T) {
	tf := writeFixtureTaskfile(t, "advisory-lock-scope", "script-count")

	var buf1, buf2 bytes.Buffer
	if rc := runQualityListTo(&buf1, []string{"--names", "--taskfile", tf}); rc != 0 {
		t.Fatalf("runQualityList --names rc=%d", rc)
	}
	if rc := runQualityListTo(&buf2, []string{"--names", "--taskfile", tf}); rc != 0 {
		t.Fatalf("runQualityList --names (2nd) rc=%d", rc)
	}
	if buf1.String() != buf2.String() {
		t.Error("--names output not byte-stable across two runs (oracle invariant broken)")
	}

	got := strings.Split(strings.TrimSpace(buf1.String()), "\n")
	want := make([]string, 0, len(gateRegistry))
	for _, g := range gateRegistry {
		want = append(want, g.Name)
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("--names not sorted registry set.\n got=%v\nwant=%v", got, want)
	}
}

// T2 — --json parses to the expected shape with one entry per gate.
func TestQualityList_JSONShape(t *testing.T) {
	tf := writeFixtureTaskfile(t, "advisory-lock-scope")
	var buf bytes.Buffer
	if rc := runQualityListTo(&buf, []string{"--json", "--taskfile", tf}); rc != 0 {
		t.Fatalf("runQualityList --json rc=%d", rc)
	}
	var out []struct {
		Name    string `json:"name"`
		Wired   bool   `json:"wired"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("--json output not valid JSON: %v\n%s", err, buf.String())
	}
	if len(out) != len(gateRegistry) {
		t.Errorf("--json len=%d want=%d", len(out), len(gateRegistry))
	}
	var wired int
	for _, e := range out {
		if e.Name == "" || e.Summary == "" {
			t.Errorf("--json entry missing name/summary: %+v", e)
		}
		if e.Wired {
			wired++
		}
	}
	if wired != 1 {
		t.Errorf("fixture wired exactly 1 gate; --json reports %d wired", wired)
	}
}

// T2 — default (no --names/--json) emits tab-separated NAME<TAB>WIRED<TAB>SUMMARY.
func TestQualityList_DefaultTabbed(t *testing.T) {
	tf := writeFixtureTaskfile(t, "advisory-lock-scope")
	var buf bytes.Buffer
	if rc := runQualityListTo(&buf, []string{"--taskfile", tf}); rc != 0 {
		t.Fatalf("runQualityList default rc=%d", rc)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != len(gateRegistry) {
		t.Fatalf("default output line count=%d want=%d", len(lines), len(gateRegistry))
	}
	for _, ln := range lines {
		cols := strings.Split(ln, "\t")
		if len(cols) != 3 {
			t.Errorf("line %q is not 3 tab-separated columns", ln)
		}
		if cols[1] != "wired" && cols[1] != "unwired" {
			t.Errorf("line %q WIRED column is %q, want wired|unwired", ln, cols[1])
		}
	}
}

// T3 — wired detection from a fixture taskfile returns the exact invoked set.
func TestTaskWiredSet_FromFixture(t *testing.T) {
	tf := writeFixtureTaskfile(t, "advisory-lock-scope", "script-count", "regression-matrix")
	got, err := taskWiredSet(tf)
	if err != nil {
		t.Fatalf("taskWiredSet: %v", err)
	}
	want := map[string]bool{"advisory-lock-scope": true, "script-count": true, "regression-matrix": true}
	if len(got) != len(want) {
		t.Errorf("wired set size=%d want=%d (%v)", len(got), len(want), got)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("wired set missing %q", k)
		}
	}
}

// T3 (negative case) — a missing taskfile returns a typed error naming the path,
// never a silent empty set (which would mislabel every gate unwired).
func TestTaskWiredSet_MissingFileErrors(t *testing.T) {
	_, err := taskWiredSet(filepath.Join(t.TempDir(), "does-not-exist.yml"))
	if err == nil {
		t.Fatal("taskWiredSet on missing file returned nil error (must fail loud)")
	}
	if !strings.Contains(err.Error(), "does-not-exist.yml") {
		t.Errorf("error should name the path, got: %v", err)
	}
}

// T4 — oracle surfaces a real registered-vs-wired split against the live taskfile.
//
// This guard asserts STRUCTURAL invariants that must always hold; the EXACT
// byte-stable set (registered=70 / wired=30 as of 2026-07-02) is locked by the
// operational baseline (`loopzero-ops quality --list | sort > /tmp/m9-gateset.before`),
// not by a magic number here — so adding/wiring a gate does not spuriously break
// this unit test, while a silent un-wire is caught by the MA-3 baseline diff.
func TestOracle_SurfacesRegisteredVsWiredSplit(t *testing.T) {
	const liveTaskfile = "../../taskfiles/quality.yml"
	if _, err := os.Stat(liveTaskfile); err != nil {
		t.Skipf("live taskfile not found (%v); reality guard requires repo checkout", err)
	}
	reg := gateRegistrySet()
	wired, err := taskWiredSet(liveTaskfile)
	if err != nil {
		t.Fatalf("taskWiredSet(live): %v", err)
	}

	// Invariant 1: registry set size equals the registry slice (no dup collapse).
	if len(reg) != len(gateRegistry) {
		t.Errorf("registry set=%d but slice=%d — duplicate gate name collapsed the set", len(reg), len(gateRegistry))
	}
	// Invariant 2: every wired gate is a real registered gate (catches a taskfile
	// typo invoking a nonexistent gate — a prose/comment mention must NOT count).
	for name := range wired {
		if !reg[name] {
			t.Errorf("taskfile wires %q which is NOT a registered gate (prose/comment leaked into detection?)", name)
		}
	}
	// Invariant 3: the split is real — some gates are wired, but not all. If this
	// ever collapses (0 wired, or all wired), the oracle's premise is broken.
	if len(wired) == 0 {
		t.Error("no gates detected as wired — detection is broken (would mislabel everything unwired)")
	}
	if len(wired) >= len(reg) {
		t.Errorf("all %d gates reported wired — the 'unwired' surface the oracle exists to expose has vanished", len(reg))
	}
	t.Logf("MEASURED oracle split: registered=%d wired=%d unwired=%d", len(reg), len(wired), len(reg)-len(wired))
}

// writeFixtureTaskfile writes a minimal, valid taskfile invoking the given gate
// names under a `cmds:` block and returns its path.
func writeFixtureTaskfile(t *testing.T, wiredGates ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("version: '3'\ntasks:\n  gates:\n    cmds:\n")
	for _, g := range wiredGates {
		b.WriteString("      - /tmp/loopzero-ops quality " + g + "\n")
	}
	p := filepath.Join(t.TempDir(), "quality.yml")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write fixture taskfile: %v", err)
	}
	return p
}

// writeRawTaskfile writes exact YAML content and returns its path.
func writeRawTaskfile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "quality.yml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write raw taskfile: %v", err)
	}
	return p
}

// T3 (structural) — a gate mentioned in a `desc:` block scalar (or a comment) is
// NOT a real invocation and MUST NOT be reported wired. Regression guard for the
// review finding that a line-anchored regex mislabeled desc-block gates wired.
func TestTaskWiredSet_IgnoresDescBlockAndComments(t *testing.T) {
	tf := writeRawTaskfile(t, `version: '3'
tasks:
  docs:
    desc: |
      Example: - /tmp/loopzero-ops quality tenant-predicate
    cmds:
      # commented: /tmp/loopzero-ops quality trivy-scan
      - echo "no real gate here"
  real:
    cmds:
      - /tmp/loopzero-ops quality advisory-lock-scope
`)
	got, err := taskWiredSet(tf)
	if err != nil {
		t.Fatalf("taskWiredSet: %v", err)
	}
	if !got["advisory-lock-scope"] {
		t.Error("real cmds invocation advisory-lock-scope not detected")
	}
	if got["tenant-predicate"] {
		t.Error("gate in a desc: block scalar was wrongly reported wired (the oracle must not lie)")
	}
	if got["trivy-scan"] {
		t.Error("gate in a # comment was wrongly reported wired")
	}
	if len(got) != 1 {
		t.Errorf("wired set should be exactly {advisory-lock-scope}, got %v", got)
	}
}

// T3 — the map form of a cmds item (`- cmd: ...`) is detected too.
func TestTaskWiredSet_MapCmdForm(t *testing.T) {
	tf := writeRawTaskfile(t, `version: '3'
tasks:
  gates:
    cmds:
      - cmd: /tmp/loopzero-ops quality script-count
      - task: some-other-task
`)
	got, err := taskWiredSet(tf)
	if err != nil {
		t.Fatalf("taskWiredSet: %v", err)
	}
	if !got["script-count"] || len(got) != 1 {
		t.Errorf("map-form cmd not detected correctly, got %v", got)
	}
}

// T3 (negative) — a malformed YAML taskfile returns a typed parse error, not empty.
func TestTaskWiredSet_MalformedYAMLErrors(t *testing.T) {
	tf := writeRawTaskfile(t, "version: '3'\ntasks:\n  gates:\n  cmds: [unbalanced\n")
	_, err := taskWiredSet(tf)
	if err == nil {
		t.Fatal("malformed taskfile returned nil error (must fail loud)")
	}
	if !strings.Contains(err.Error(), "quality.yml") {
		t.Errorf("parse error should name the path, got: %v", err)
	}
}

// T2 (negative) — runQualityListTo returns rc 1 on a missing taskfile (error
// plumbed through the list command, not only through taskWiredSet).
func TestQualityList_MissingTaskfileRC1(t *testing.T) {
	var buf bytes.Buffer
	rc := runQualityListTo(&buf, []string{"--names", "--taskfile", filepath.Join(t.TempDir(), "nope.yml")})
	if rc != 1 {
		t.Errorf("runQualityListTo on missing taskfile rc=%d, want 1", rc)
	}
	if buf.Len() != 0 {
		t.Errorf("no gate output expected on error, got %q", buf.String())
	}
}

// T2 (negative) — an unknown flag returns rc 1.
func TestQualityList_BadFlagRC1(t *testing.T) {
	var buf bytes.Buffer
	if rc := runQualityListTo(&buf, []string{"--nope"}); rc != 1 {
		t.Errorf("runQualityListTo --nope rc=%d, want 1", rc)
	}
}

// T2 — --json wins over --names when both are passed (documented precedence).
func TestQualityList_JSONWinsOverNames(t *testing.T) {
	tf := writeFixtureTaskfile(t, "advisory-lock-scope")
	var buf bytes.Buffer
	if rc := runQualityListTo(&buf, []string{"--json", "--names", "--taskfile", tf}); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var out []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Errorf("--json --names should still emit JSON (json precedence), got: %s", buf.String())
	}
}

// T2 (edge) — a taskfile with zero gate invocations lists all gates unwired.
func TestQualityList_ZeroWiredTaskfile(t *testing.T) {
	tf := writeRawTaskfile(t, "version: '3'\ntasks:\n  noop:\n    cmds:\n      - echo hi\n")
	var buf bytes.Buffer
	if rc := runQualityListTo(&buf, []string{"--taskfile", tf}); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if strings.Contains(buf.String(), "\twired\t") {
		t.Error("a taskfile with no gate invocations must report every gate unwired")
	}
	if lines := strings.Count(strings.TrimSpace(buf.String()), "\n") + 1; lines != len(gateRegistry) {
		t.Errorf("still expected %d gate lines, got %d", len(gateRegistry), lines)
	}
}

// T4 / MA-3 — the frozen baseline IS the oracle self-check: the current --list
// output (default format, against the live taskfile) MUST byte-match the frozen
// baseline's gate lines. This catches a single silent un-wire (30->29) at unit
// time AND proves the committed baseline is in the oracle's native order (not a
// locale-sorted copy). Re-freeze the baseline via `loopzero-ops quality --list` when
// gates are consciously added/wired.
func TestOracle_MatchesFrozenBaseline(t *testing.T) {
	const baseline = "../../docs/operations/baselines/2026-07-02-m9-gateset.before"
	const liveTaskfile = "../../taskfiles/quality.yml"
	if _, err := os.Stat(baseline); err != nil {
		t.Skipf("frozen baseline not found (%v)", err)
	}
	raw, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	// Baseline gate lines = non-comment, non-blank lines.
	var want []string
	for _, ln := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		want = append(want, ln)
	}

	var buf bytes.Buffer
	if rc := runQualityListTo(&buf, []string{"--taskfile", liveTaskfile}); rc != 0 {
		t.Fatalf("runQualityListTo(live) rc=%d", rc)
	}
	got := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("live --list diverged from the frozen baseline (a gate was added/removed/un-wired, "+
			"or the baseline is stale). Re-freeze with `loopzero-ops quality --list` after a conscious change.\n"+
			"got %d lines, baseline %d lines", len(got), len(want))
	}
}
