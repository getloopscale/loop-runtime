// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package alertrunbooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rulesRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, DefaultRulesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

// cfg uses an empty exception list so tests are independent of the real default.
func cfg(root string) Config {
	return Config{Root: root, ExceptionList: []string{}}
}

func TestAlertWithRunbookURLPasses(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "groups:\n- rules:\n  - alert: GoodAlert\n    annotations:\n      runbook_url: https://docs/x\n",
	})
	if !Check(cfg(root)).Pass {
		t.Error("alert with runbook_url should pass")
	}
}

func TestAlertWithLegacyRunbookPasses(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: LegacyAlert\n    annotations:\n      runbook: see-wiki\n",
	})
	if !Check(cfg(root)).Pass {
		t.Error("legacy runbook: annotation should pass")
	}
}

func TestAlertMissingRunbookFails(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: NoRunbook\n    expr: up == 0\n",
	})
	o := Check(cfg(root))
	if o.Pass || len(o.Violations) != 1 {
		t.Errorf("expected 1 violation, got %v", o.Violations)
	}
	// The format started carrying the directory on 2026-09-06 (B-067). This test pinned
	// the basename alone, and that was exactly the shape that made two mirrored sites
	// indistinguishable in the report — see TestViolationDistinguishesMirroredDirectories.
	if o.Violations[0] != DefaultRulesDirs[0]+"/prometheusrule-x.yaml:NoRunbook" {
		t.Errorf("violation format wrong: %q", o.Violations[0])
	}
}

func TestExceptionListPasses(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: KnownGap\n    expr: up == 0\n",
	})
	o := Check(Config{Root: root, ExceptionList: []string{"KnownGap"}})
	if !o.Pass {
		t.Errorf("allowlisted alert should pass, got %v", o.Violations)
	}
}

func TestNonPrometheusRuleFileIgnored(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		// Filename lacks "prometheusrule" → not scanned.
		"servicemonitor.yaml": "spec:\n  - alert: Ignored\n    expr: up == 0\n",
	})
	if !Check(cfg(root)).Pass {
		t.Error("non-prometheusrule files must be ignored")
	}
}

func TestNonYAMLIgnored(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.txt": "spec:\n  - alert: Ignored\n    expr: up == 0\n",
	})
	if !Check(cfg(root)).Pass {
		t.Error(".txt files must be ignored")
	}
}

func TestMultipleAlertsMixed(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: Has\n    annotations:\n      runbook_url: u\n" +
			"  - alert: Missing1\n    expr: up == 0\n" +
			"  - alert: Missing2\n    expr: up == 1\n",
	})
	o := Check(cfg(root))
	if o.Pass || len(o.Violations) != 2 {
		t.Errorf("expected 2 violations (Missing1, Missing2), got %v", o.Violations)
	}
}

func TestRunbookBeyondLookaheadStillFails(t *testing.T) {
	// runbook_url placed >1500 chars into the block → outside the window → fail.
	pad := strings.Repeat("    # filler comment line\n", 80) // ~2000 chars
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: FarRunbook\n    expr: up == 0\n" + pad + "    runbook_url: late\n",
	})
	if Check(cfg(root)).Pass {
		t.Error("runbook beyond the 1500-char lookahead must still fail")
	}
}

// Finding NO rules directory is a FAILURE, and this test asserted the opposite until
// 2026-08-19 — it was called `TestMissingRulesDirTolerated` and required `Pass`.
//
// Tolerating it was "green over nothing": a gate that finds no directory at all reports
// exactly the same verdict as one that scanned everything and found no defect. That is
// the shape B-040 and B-127 name, and it was here.
//
// Tolerance for an INDIVIDUAL directory remains — the test trees hold one of the four —
// but zero found no longer passes.
func TestNoRulesDirectoryFailsInsteadOfGreen(t *testing.T) {
	root := t.TempDir()
	out := Check(cfg(root))
	if out.Pass {
		t.Error("a scan with NO rules directory passed — green over nothing is not green")
	}
	if out.DirsScanned != 0 {
		t.Errorf("DirsScanned=%d in an empty tree", out.DirsScanned)
	}
}

// The counterpart: ONE directory present among the four is enough, otherwise every test
// tree (which creates only one) would fail for the wrong reason.
func TestOneDirectoryPresentAmongTheFourIsEnough(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"prometheusrule-x.yaml": "spec:\n  - alert: LoopZeroLockContention\n    expr: up == 0\n",
	})
	out := Check(Config{Root: root})
	if out.DirsScanned != 1 {
		t.Errorf("wanted 1 directory scanned, got %d", out.DirsScanned)
	}
	if !out.Pass {
		t.Errorf("one valid directory should be enough: %+v", out.Violations)
	}
}

func TestDefaultExceptionListApplied(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		// A default-exception alert without runbook → passes via defaults.
		"prometheusrule-x.yaml": "spec:\n  - alert: LoopZeroLockContention\n    expr: up == 0\n",
	})
	if !Check(Config{Root: root}).Pass { // nil ExceptionList → defaults
		t.Error("default exception list should permit LoopZeroLockContention")
	}
}

// Two scanned directories contain the SAME file name — `infra/helm/...` and the
// `charts/...` that ArgoCD delivers are mirrored copies, and 9 such pairs exist in the real
// tree (measured 2026-09-06). As long as the violation carries only the BASENAME, the two
// sites produce IDENTICAL strings: whoever reads the report does not know which of the two
// files to open, and fixing one leaves the other failing with a line indistinguishable
// from the one just fixed.
func TestViolationDistinguishesMirroredDirectories(t *testing.T) {
	root := t.TempDir()
	body := "spec:\n  - alert: LoopZeroMirrored\n    expr: up == 0\n"
	for _, d := range []string{DefaultRulesDirs[0], DefaultRulesDirs[1]} {
		dir := filepath.Join(root, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prometheusrule-x.yaml"), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", d, err)
		}
	}
	out := Check(cfg(root))
	if len(out.Violations) != 2 {
		t.Fatalf("wanted 2 sites in violation, got %d: %v", len(out.Violations), out.Violations)
	}
	if out.Violations[0] == out.Violations[1] {
		t.Errorf("the two sites are indistinguishable: both %q", out.Violations[0])
	}
	for _, v := range out.Violations {
		if !strings.Contains(v, "/") {
			t.Errorf("violation %q does not name the directory", v)
		}
	}
}

// The report counts SITES, and distinct alerts are fewer than sites whenever an alert
// lives in both copies. Saying "N alerts without a runbook" when N counts the same alert
// twice is the mistake the gate exists not to make: an inflated number read as debt.
func TestDistinctAlertsDoNotCountTheMirrorTwice(t *testing.T) {
	root := t.TempDir()
	body := "spec:\n  - alert: LoopZeroMirrored\n    expr: up == 0\n"
	for _, d := range []string{DefaultRulesDirs[0], DefaultRulesDirs[1]} {
		dir := filepath.Join(root, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prometheusrule-x.yaml"), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", d, err)
		}
	}
	out := Check(cfg(root))
	if got := out.DistinctAlerts(); got != 1 {
		t.Errorf("wanted 1 distinct alert across 2 sites, got %d", got)
	}
}
