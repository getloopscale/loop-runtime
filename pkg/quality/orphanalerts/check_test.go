// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package orphanalerts

import (
	"os"
	"path/filepath"
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

func TestNoOrphanPasses(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"alerts.yaml": "groups:\n- rules:\n  - alert: RealAlert\n    expr: up == 0\n",
	})
	if !Check(Config{Root: root}).Pass {
		t.Error("real alert (no vector(0)) should pass")
	}
}

func TestOrphanFails(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"alerts.yaml": "groups:\n- rules:\n  - alert: GhostAlert\n    expr: vector(0) > 1\n",
	})
	o := Check(Config{Root: root})
	if o.Pass || len(o.Violations) != 1 {
		t.Errorf("expected 1 orphan violation, got %v", o.Violations)
	}
}

func TestOrphanInExceptionListPasses(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"alerts.yaml": "  - alert: KnownOrphan\n    expr: vector(0) > 1\n",
	})
	o := Check(Config{Root: root, ExceptionList: []string{"KnownOrphan"}})
	if !o.Pass {
		t.Errorf("allowlisted orphan should pass, got %v", o.Violations)
	}
}

func TestOrphanWithoutAlertNameFlagged(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		// vector(0) with no `alert:` within 5 lines above → name-undetermined violation.
		"alerts.yaml": "x\nx\nx\nx\nx\nx\n    expr: vector(0) > 1\n",
	})
	o := Check(Config{Root: root})
	if o.Pass || len(o.Violations) != 1 {
		t.Errorf("expected 1 name-undetermined violation, got %v", o.Violations)
	}
}

func TestAlertNameWindowRespects5Lines(t *testing.T) {
	// alert: declared 6 lines above the expr → out of the 5-line window → name not found.
	root := rulesRepo(t, map[string]string{
		"a.yaml": "  - alert: FarAway\n    f1\n    f2\n    f3\n    f4\n    f5\n    expr: vector(0) > 1\n",
	})
	o := Check(Config{Root: root})
	// name not found within window → still a violation (different message).
	if o.Pass {
		t.Error("orphan with alert name outside 5-line window must still fail")
	}
}

func TestNearestAlertNameWins(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"a.yaml": "  - alert: First\n    expr: up == 0\n  - alert: Second\n    expr: vector(0) > 1\n",
	})
	o := Check(Config{Root: root})
	if o.Pass {
		t.Fatal("expected violation for Second")
	}
	if want := "Second"; !contains(o.Violations[0], want) {
		t.Errorf("violation should name %q, got %q", want, o.Violations[0])
	}
}

func TestMultipleFilesAndViolations(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"a.yaml": "  - alert: A\n    expr: vector(0) > 1\n",
		"b.yaml": "  - alert: B\n    expr: vector(0) > 1\n",
		"c.yaml": "  - alert: C\n    expr: up == 0\n", // not orphan
	})
	o := Check(Config{Root: root})
	if o.Pass || len(o.Violations) != 2 {
		t.Errorf("expected 2 violations across files, got %v", o.Violations)
	}
}

func TestNonYAMLIgnored(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"notes.txt": "  - alert: Z\n    expr: vector(0) > 1\n",
	})
	if !Check(Config{Root: root}).Pass {
		t.Error(".txt files in rules dir should be ignored")
	}
}

func TestMissingRulesDirTolerated(t *testing.T) {
	root := t.TempDir() // no infra/helm/monitoring/manifests
	if !Check(Config{Root: root}).Pass {
		t.Error("missing rules dir should pass (nothing to check)")
	}
}

func TestExtraSpacingInExpr(t *testing.T) {
	root := rulesRepo(t, map[string]string{
		"a.yaml": "  - alert: Spaced\n    expr:   vector(0)  >  1\n",
	})
	if Check(Config{Root: root}).Pass {
		t.Error("expr with extra spacing should still match the orphan pattern")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
