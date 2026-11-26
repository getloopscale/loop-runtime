// Hermetic tests for the vaultpromalerts gate.
// Migrated from bash test infra/tests/check-vault-prom-alerts_test.sh
// (M-Migrate2 2026-07-10 M16 T3.3 Wave 2).
package vaultpromalerts

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writefile: %v", err)
	}
}

// canonicalRuleBody has all 4 alerts each with severity/runbook/team/domain
// labels, and VaultAuditLogStalled carries the invariant marker.
const canonicalRuleBody = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: vault-audit-alerts
spec:
  groups:
    - name: vault
      rules:
        - alert: VaultAuthFailureRateHigh
          expr: rate(vault_auth_failure[5m]) > 5
          labels:
            severity: critical
            team: platform-security
            domain: vault
          annotations:
            runbook_url: https://runbooks/vault-auth-failure
        - alert: VaultPolicyDenialRateHigh
          expr: rate(vault_policy_denial[5m]) > 5
          labels:
            severity: critical
            team: platform-security
            domain: vault
          annotations:
            runbook_url: https://runbooks/vault-policy-denial
        - alert: VaultSealedOrDown
          expr: vault_sealed == 1
          labels:
            severity: critical
            team: platform-security
            domain: vault
          annotations:
            runbook_url: https://runbooks/vault-sealed
        - alert: VaultAuditLogStalled
          expr: absent_over_time(vault_audit_log{}[10m])
          labels:
            severity: critical
            team: platform-security
            domain: vault
            invariant: "audit-log-must-flow"
          annotations:
            runbook_url: https://runbooks/vault-audit-stalled
`

func TestCheck_HappyPath_PASS(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ruleFileRel), canonicalRuleBody)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected nil, got %v; %v", err, outcome.Failures)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures, got %v", outcome.Failures)
	}
}

func TestCheck_MissingFile_FAIL(t *testing.T) {
	root := t.TempDir()
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !hasSubstring(outcome.Failures, "PrometheusRule file missing") {
		t.Fatalf("expected missing-file failure, got %v", outcome.Failures)
	}
}

func TestCheck_MissingAlert_FAIL(t *testing.T) {
	root := t.TempDir()
	// Remove VaultSealedOrDown from canonical body.
	broken := canonicalRuleBody
	broken = replaceFirst(broken, "- alert: VaultSealedOrDown", "- alert: DifferentAlert")
	writeFile(t, filepath.Join(root, ruleFileRel), broken)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !hasSubstring(outcome.Failures, "VaultSealedOrDown MISSING") {
		t.Fatalf("expected VaultSealedOrDown failure, got %v", outcome.Failures)
	}
}

func TestCheck_NonCriticalSeverity_FAIL(t *testing.T) {
	root := t.TempDir()
	broken := replaceFirst(canonicalRuleBody, "severity: critical\n            team: platform-security\n            domain: vault\n          annotations:\n            runbook_url: https://runbooks/vault-auth-failure",
		"severity: warning\n            team: platform-security\n            domain: vault\n          annotations:\n            runbook_url: https://runbooks/vault-auth-failure")
	writeFile(t, filepath.Join(root, ruleFileRel), broken)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !hasSubstring(outcome.Failures, "non-critical severities") {
		t.Fatalf("expected non-critical failure, got %v", outcome.Failures)
	}
}

func TestCheck_MissingInvariant_FAIL(t *testing.T) {
	root := t.TempDir()
	// Remove invariant marker.
	broken := replaceFirst(canonicalRuleBody, "invariant: \"audit-log-must-flow\"\n", "")
	writeFile(t, filepath.Join(root, ruleFileRel), broken)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !hasSubstring(outcome.Failures, "invariant label") {
		t.Fatalf("expected invariant failure, got %v", outcome.Failures)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	root := qtestutil.Root(t)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("real repo violation: %v; %v", err, outcome.Failures)
	}
}

// replaceFirst replaces the first occurrence of old with new in s.
func replaceFirst(s, old, new string) string {
	i := indexOf(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func hasSubstring(failures []string, substr string) bool {
	for _, f := range failures {
		if indexOf(f, substr) >= 0 {
			return true
		}
	}
	return false
}
