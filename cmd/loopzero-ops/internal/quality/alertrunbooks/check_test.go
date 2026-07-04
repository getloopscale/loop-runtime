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
	if o.Violations[0] != "prometheusrule-x.yaml:NoRunbook" {
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

func TestMissingRulesDirTolerated(t *testing.T) {
	root := t.TempDir()
	if !Check(cfg(root)).Pass {
		t.Error("missing rules dir should pass")
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
