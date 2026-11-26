// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/cliout"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/exitcode"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

// liveTaskfile is the repository's own taskfiles/quality.yml.
func liveTaskfile() string {
	return filepath.Join(qtestutil.MustRepoRoot(), "taskfiles", "quality.yml")
}

// listTo runs `quality --list` with the given flags into w.
func listTo(w io.Writer, args []string) int {
	return runList(cliout.New(w), cliout.New(io.Discard), args)
}

// Main's own paths — empty args, help, --list, an unknown gate — and which
// stream each writes to.
func TestMain_MetaPaths(t *testing.T) {
	run := func(args ...string) (string, string, int) {
		var out, errb bytes.Buffer
		rc := Main(args, &out, &errb)
		return out.String(), errb.String(), rc
	}
	t.Run("empty args print help to stderr and exit usage", func(t *testing.T) {
		out, errOut, rc := run()
		if rc != 1 || out != "" || errOut != HelpText() {
			t.Errorf("rc=%d stdout=%q stderr-is-help=%v", rc, out, errOut == HelpText())
		}
	})
	for _, flag := range []string{"--help", "-h", "help"} {
		t.Run(flag+" prints help to stdout", func(t *testing.T) {
			out, errOut, rc := run(flag)
			if rc != 0 || errOut != "" || out != HelpText() {
				t.Errorf("rc=%d stderr=%q stdout-is-help=%v", rc, errOut, out == HelpText())
			}
		})
	}
	t.Run("--list delegates to the list command", func(t *testing.T) {
		if _, errOut, rc := run("--list", "--taskfile", liveTaskfile()); rc != 0 {
			t.Errorf("rc=%d stderr=%s", rc, errOut)
		}
	})
	t.Run("an unknown gate names itself and prints help", func(t *testing.T) {
		out, errOut, rc := run("definitely-not-a-gate")
		if rc != 1 || out != "" {
			t.Errorf("rc=%d stdout=%q", rc, out)
		}
		if !strings.HasPrefix(errOut, "unknown gate: definitely-not-a-gate\n") || !strings.Contains(errOut, HelpText()) {
			t.Errorf("stderr = %.80q", errOut)
		}
	})
}

// register refuses a duplicate name and a gate with no check: a duplicate
// would otherwise collapse silently while --list stayed the same.
func TestRegister_RefusesDuplicatesAndGatesWithoutACheck(t *testing.T) {
	existing := Gates()[0]
	for name, g := range map[string]Gate{
		"duplicate": {Name: existing.Name, Summary: "x", Setup: existing.Setup},
		"no setup":  {Name: "gate-with-no-setup", Summary: "x"},
		"no name":   {Summary: "x", Setup: existing.Setup},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("register accepted it")
				}
			}()
			register(g)
		})
	}
	if _, ok := Lookup("gate-with-no-setup"); ok {
		t.Error("a refused gate was registered anyway")
	}
}

// A gate lives in the file named after it, so where a gate is never depends
// on which theme someone filed it under.
func TestEveryGateLivesInTheFileNamedAfterIt(t *testing.T) {
	for _, g := range Gates() {
		file := strings.ReplaceAll(g.Name, "-", "_") + "_gate.go"
		b, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("gate %q: expected its file %s: %v", g.Name, file, err)
			continue
		}
		if !strings.Contains(string(b), "Name:") || !strings.Contains(string(b), `"`+g.Name+`"`) {
			t.Errorf("gate %q is not registered from %s", g.Name, file)
		}
	}
}

// T1 — every registry entry is well-formed: non-empty name + non-empty summary, unique names.
func TestGateRegistry_WellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, g := range Gates() {
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
	if rc := listTo(&buf1, []string{"--names", "--taskfile", tf}); rc != 0 {
		t.Fatalf("list --names rc=%d", rc)
	}
	if rc := listTo(&buf2, []string{"--names", "--taskfile", tf}); rc != 0 {
		t.Fatalf("list --names (2nd) rc=%d", rc)
	}
	if buf1.String() != buf2.String() {
		t.Error("--names output not byte-stable across two runs (oracle invariant broken)")
	}

	got := strings.Split(strings.TrimSpace(buf1.String()), "\n")
	want := make([]string, 0, len(Gates()))
	for _, g := range Gates() {
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
	if rc := listTo(&buf, []string{"--json", "--taskfile", tf}); rc != 0 {
		t.Fatalf("list --json rc=%d", rc)
	}
	var out []struct {
		Name    string   `json:"name"`
		Tasks   []string `json:"tasks"`
		Summary string   `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("--json output not valid JSON: %v\n%s", err, buf.String())
	}
	if len(out) != len(Gates()) {
		t.Errorf("--json len=%d want=%d", len(out), len(Gates()))
	}
	var wired int
	for _, e := range out {
		if e.Name == "" || e.Summary == "" {
			t.Errorf("--json entry missing name/summary: %+v", e)
		}
		if e.Tasks == nil {
			t.Errorf("--json gate %q has null tasks (must be [] not null)", e.Name)
		}
		if len(e.Tasks) > 0 {
			wired++
		}
	}
	if wired != 1 {
		t.Errorf("fixture wires exactly 1 gate; --json reports %d with non-empty tasks", wired)
	}
}

// T2 — default (no --names/--json) emits tab-separated NAME<TAB>TASKS<TAB>SUMMARY.
func TestQualityList_DefaultTabbed(t *testing.T) {
	tf := writeFixtureTaskfile(t, "advisory-lock-scope")
	var buf bytes.Buffer
	if rc := listTo(&buf, []string{"--taskfile", tf}); rc != 0 {
		t.Fatalf("list default rc=%d", rc)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != len(Gates()) {
		t.Fatalf("default output line count=%d want=%d", len(lines), len(Gates()))
	}
	var attributed int
	for _, ln := range lines {
		cols := strings.Split(ln, "\t")
		if len(cols) != 3 {
			t.Errorf("line %q is not 3 tab-separated columns", ln)
		}
		// TASKS column is "-" (no task) or a comma-joined sorted task list.
		if cols[1] == "" {
			t.Errorf("line %q TASKS column is empty (must be '-' or a task list)", ln)
		}
		if cols[1] != "-" {
			attributed++
			if cols[0] == "advisory-lock-scope" && cols[1] != "gates" {
				t.Errorf("advisory-lock-scope TASKS=%q, want 'gates' (fixture task)", cols[1])
			}
		}
	}
	if attributed != 1 {
		t.Errorf("fixture attributes exactly 1 gate to a task; got %d attributed", attributed)
	}
}

// T3 — per-task attribution from a fixture taskfile returns the exact gate→task map.
func TestTaskGateTasks_FromFixture(t *testing.T) {
	tf := writeFixtureTaskfile(t, "advisory-lock-scope", "script-count", "regression-matrix")
	got, err := taskGateTasks(tf)
	if err != nil {
		t.Fatalf("taskGateTasks: %v", err)
	}
	want := map[string][]string{
		"advisory-lock-scope": {"gates"},
		"script-count":        {"gates"},
		"regression-matrix":   {"gates"},
	}
	if len(got) != len(want) {
		t.Errorf("attribution size=%d want=%d (%v)", len(got), len(want), got)
	}
	for gate, tasks := range want {
		if strings.Join(got[gate], ",") != strings.Join(tasks, ",") {
			t.Errorf("gate %q attributed to %v, want %v", gate, got[gate], tasks)
		}
	}
}

// T3 (negative case) — a missing taskfile returns a typed error naming the path,
// never a silent empty map (which would mislabel every gate un-attributed).
func TestTaskGateTasks_MissingFileErrors(t *testing.T) {
	_, err := taskGateTasks(filepath.Join(t.TempDir(), "does-not-exist.yml"))
	if err == nil {
		t.Fatal("taskGateTasks on missing file returned nil error (must fail loud)")
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
	reg := map[string]bool{}
	for _, g := range Gates() {
		reg[g.Name] = true
	}
	gateTasks, err := taskGateTasks(liveTaskfile())
	if err != nil {
		t.Fatalf("taskGateTasks(live): %v", err)
	}

	// Invariant 2: every attributed gate is a real registered gate (catches a
	// taskfile typo invoking a nonexistent gate — prose/comment must NOT count).
	for name := range gateTasks {
		if !reg[name] {
			t.Errorf("taskfile attributes %q which is NOT a registered gate (prose/comment leaked into detection?)", name)
		}
	}
	// Invariant 3: the split is real — some gates are attributed to a task, but
	// not all. If this collapses (0 or all), the oracle's premise is broken.
	if len(gateTasks) == 0 {
		t.Error("no gates attributed to any task — detection is broken (would mislabel everything un-wired)")
	}
	if len(gateTasks) >= len(reg) {
		t.Errorf("all %d gates attributed — the 'un-wired' surface the oracle exists to expose has vanished", len(reg))
	}
	t.Logf("MEASURED oracle split: registered=%d attributed=%d unattributed=%d", len(reg), len(gateTasks), len(reg)-len(gateTasks))
}

// T0.1 (MEET-2026-07-02-002 oracle hardening) — a gate invoked by MORE THAN ONE
// task is attributed to the SORTED set of all invoking tasks; a gate in only one
// task shows only that task. This is the per-task attribution the boolean union
// could not express.
func TestTaskGateTasks_AttributesAcrossTasks(t *testing.T) {
	tf := writeRawTaskfile(t, `version: '3'
tasks:
  gates:
    cmds:
      - /tmp/loopzero-ops quality advisory-lock-scope
      - /tmp/loopzero-ops quality script-count
  diff:
    cmds:
      - /tmp/loopzero-ops quality advisory-lock-scope
      - /tmp/loopzero-ops quality no-new-bash
`)
	got, err := taskGateTasks(tf)
	if err != nil {
		t.Fatalf("taskGateTasks: %v", err)
	}
	// advisory-lock-scope is in both tasks -> sorted [diff, gates].
	if strings.Join(got["advisory-lock-scope"], ",") != "diff,gates" {
		t.Errorf("advisory-lock-scope attributed to %v, want [diff gates]", got["advisory-lock-scope"])
	}
	if strings.Join(got["script-count"], ",") != "gates" {
		t.Errorf("script-count attributed to %v, want [gates]", got["script-count"])
	}
	if strings.Join(got["no-new-bash"], ",") != "diff" {
		t.Errorf("no-new-bash attributed to %v, want [diff]", got["no-new-bash"])
	}
}

// T0.2 (MEET-2026-07-02-002 oracle hardening) — THE positive-control the
// verification-architect required: the oracle MUST fire (output bytes change)
// when a gate is un-wired from `quality:gates` EVEN IF it survives in another
// task. Under the former boolean whole-file union the gate stayed wired=true and
// the output was byte-identical — the silent un-wire the oracle exists to catch.
// This test proves the union gap is closed: attribution changes gates→diff.
func TestOracle_FiresOnUnwireFromGates(t *testing.T) {
	before := writeRawTaskfile(t, `version: '3'
tasks:
  gates:
    cmds:
      - /tmp/loopzero-ops quality advisory-lock-scope
  diff:
    cmds:
      - /tmp/loopzero-ops quality advisory-lock-scope
`)
	// Same file with the gate REMOVED from `quality:gates`, still present in diff.
	after := writeRawTaskfile(t, `version: '3'
tasks:
  gates:
    cmds:
      - echo "advisory-lock-scope no longer enforced in the hard set"
  diff:
    cmds:
      - /tmp/loopzero-ops quality advisory-lock-scope
`)

	render := func(tf string) string {
		var buf bytes.Buffer
		if rc := listTo(&buf, []string{"--taskfile", tf}); rc != 0 {
			t.Fatalf("list rc=%d", rc)
		}
		return buf.String()
	}
	outBefore, outAfter := render(before), render(after)

	if outBefore == outAfter {
		t.Fatal("oracle did NOT fire on un-wire from quality:gates (a gate dropped from the hard " +
			"set but surviving in diff must change the --list bytes — this is the silent-un-wire the " +
			"oracle exists to catch; a boolean union would wrongly stay identical)")
	}
	// Precise assertion: advisory-lock-scope's TASKS cell went from "diff,gates" to "diff".
	tasksCell := func(out string) string {
		for _, ln := range strings.Split(out, "\n") {
			cols := strings.Split(ln, "\t")
			if len(cols) == 3 && cols[0] == "advisory-lock-scope" {
				return cols[1]
			}
		}
		return "<not found>"
	}
	if got := tasksCell(outBefore); got != "diff,gates" {
		t.Errorf("before: advisory-lock-scope TASKS=%q, want diff,gates", got)
	}
	if got := tasksCell(outAfter); got != "diff" {
		t.Errorf("after: advisory-lock-scope TASKS=%q, want diff (un-wired from gates)", got)
	}
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
	got, err := taskGateTasks(tf)
	if err != nil {
		t.Fatalf("taskGateTasks: %v", err)
	}
	if strings.Join(got["advisory-lock-scope"], ",") != "real" {
		t.Errorf("real cmds invocation advisory-lock-scope attributed to %v, want [real]", got["advisory-lock-scope"])
	}
	if _, ok := got["tenant-predicate"]; ok {
		t.Error("gate in a desc: block scalar was wrongly attributed (the oracle must not lie)")
	}
	if _, ok := got["trivy-scan"]; ok {
		t.Error("gate in a # comment was wrongly attributed")
	}
	if len(got) != 1 {
		t.Errorf("attribution should be exactly {advisory-lock-scope}, got %v", got)
	}
}

// T3 — the map form of a cmds item (`- cmd: ...`) is detected too.
func TestTaskGateTasks_MapCmdForm(t *testing.T) {
	tf := writeRawTaskfile(t, `version: '3'
tasks:
  gates:
    cmds:
      - cmd: /tmp/loopzero-ops quality script-count
      - task: some-other-task
`)
	got, err := taskGateTasks(tf)
	if err != nil {
		t.Fatalf("taskGateTasks: %v", err)
	}
	if strings.Join(got["script-count"], ",") != "gates" || len(got) != 1 {
		t.Errorf("map-form cmd not detected correctly, got %v", got)
	}
}

// T3 (negative) — a malformed YAML taskfile returns a typed parse error, not empty.
func TestTaskGateTasks_MalformedYAMLErrors(t *testing.T) {
	tf := writeRawTaskfile(t, "version: '3'\ntasks:\n  gates:\n  cmds: [unbalanced\n")
	_, err := taskGateTasks(tf)
	if err == nil {
		t.Fatal("malformed taskfile returned nil error (must fail loud)")
	}
	if !strings.Contains(err.Error(), "quality.yml") {
		t.Errorf("parse error should name the path, got: %v", err)
	}
}

// T2 (negative) — a missing taskfile means the list could not be computed:
// the command exits incomplete, never with an empty inventory.
func TestQualityList_MissingTaskfileIsIncomplete(t *testing.T) {
	var buf bytes.Buffer
	rc := listTo(&buf, []string{"--names", "--taskfile", filepath.Join(t.TempDir(), "nope.yml")})
	if rc != exitcode.Incomplete {
		t.Errorf("list on missing taskfile rc=%d, want %d", rc, exitcode.Incomplete)
	}
	if buf.Len() != 0 {
		t.Errorf("no gate output expected on error, got %q", buf.String())
	}
}

// T2 (negative) — an unknown flag returns rc 1.
func TestQualityList_BadFlagRC1(t *testing.T) {
	var buf bytes.Buffer
	if rc := listTo(&buf, []string{"--nope"}); rc != 1 {
		t.Errorf("list --nope rc=%d, want 1", rc)
	}
}

// T2 — --json wins over --names when both are passed (documented precedence).
func TestQualityList_JSONWinsOverNames(t *testing.T) {
	tf := writeFixtureTaskfile(t, "advisory-lock-scope")
	var buf bytes.Buffer
	if rc := listTo(&buf, []string{"--json", "--names", "--taskfile", tf}); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var out []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Errorf("--json --names should still emit JSON (json precedence), got: %s", buf.String())
	}
}

// T2 (edge) — a taskfile with zero gate invocations lists every gate with a "-"
// TASKS column (un-attributed).
func TestQualityList_ZeroWiredTaskfile(t *testing.T) {
	tf := writeRawTaskfile(t, "version: '3'\ntasks:\n  noop:\n    cmds:\n      - echo hi\n")
	var buf bytes.Buffer
	if rc := listTo(&buf, []string{"--taskfile", tf}); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		cols := strings.Split(ln, "\t")
		if len(cols) != 3 || cols[1] != "-" {
			t.Errorf("a taskfile with no gate invocations must attribute every gate to '-', got line %q", ln)
		}
	}
	if lines := strings.Count(strings.TrimSpace(buf.String()), "\n") + 1; lines != len(Gates()) {
		t.Errorf("still expected %d gate lines, got %d", len(Gates()), lines)
	}
}

// T4 / MA-3 — the frozen baseline IS the oracle self-check: the current --list
// output (default format, against the live taskfile) MUST byte-match the frozen
// baseline's gate lines. This catches a single silent un-wire (30->29) at unit
// time AND proves the committed baseline is in the oracle's native order (not a
// locale-sorted copy). Re-freeze the baseline via `loopzero-ops quality --list` when
// gates are consciously added/wired.
// oracleBaselinePath is the frozen gate set the oracle compares against. It lives in
// testdata/ — not in docs/operations/baselines/ — because the test re-freezes it on
// every consciously added gate, which is the opposite of what an audit-trail path
// promises. See TestOracle_PathIsNotAuditTrail (B-006).
const oracleBaselinePath = "testdata/gateset.frozen"

func TestOracle_MatchesFrozenBaseline(t *testing.T) {
	const baseline = oracleBaselinePath
	// Absence is a FAILURE, not a skip: the oracle is committed, so a missing file
	// means it was deleted or the path drifted — and a skip would report green while
	// the protection silently vanished. Caught in B-006 when moving the path made the
	// test pass by skipping.
	if _, err := os.Stat(baseline); err != nil {
		t.Fatalf("frozen gate-set oracle missing at %s (%v) — re-freeze with `loopzero-ops quality --list > cmd/loopzero-ops/internal/qualitycli/testdata/gateset.frozen`", baseline, err)
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
	if rc := listTo(&buf, []string{"--taskfile", liveTaskfile()}); rc != 0 {
		t.Fatalf("listTo(live) rc=%d", rc)
	}
	got := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("live --list diverged from the frozen baseline (a gate was added/removed/un-wired, "+
			"or the baseline is stale). Re-freeze with `loopzero-ops quality --list` after a conscious change.\n"+
			"got %d lines, baseline %d lines", len(got), len(want))
	}
}

// TestOracle_PathIsNotAuditTrail keeps the gate-set oracle out of the audit trail.
//
// Regression for B-006. The oracle lived at
// docs/operations/baselines/2026-07-02-m9-gateset.before, and docs/README.md
// classifies operations/baselines/ as audit trail — "History. Preserved
// indefinitely. Never edited to match present state." But TestOracle_MatchesFrozenBaseline
// requires a re-freeze on every consciously added gate, so the file was rewritten in
// 24 commits and grew from 70 gates to 116. A file named `2026-07-02-…before` that has
// been rewritten two dozen times is not a record of 2026-07-02; anyone opening it for
// the July gate set finds today's, and the date in the name makes that especially
// misleading.
//
// The oracle belongs somewhere that declares itself living. testdata/ is where Go puts
// fixtures a test owns and rewrites.
func TestOracle_PathIsNotAuditTrail(t *testing.T) {
	auditTrail := []string{
		"docs/operations/baselines/", "docs/operations/findings/", "docs/operations/evidence/",
		"docs/plans/completed/", "docs/meetings/records/", "docs/reviews/", "docs/handoff/",
		"docs/decisions/",
	}
	for _, dir := range auditTrail {
		if strings.Contains(filepath.ToSlash(oracleBaselinePath), dir) {
			t.Fatalf("the gate-set oracle sits in the audit trail (%s): it is re-frozen on every "+
				"gate addition, so it records the present while its location promises history", dir)
		}
	}
}
