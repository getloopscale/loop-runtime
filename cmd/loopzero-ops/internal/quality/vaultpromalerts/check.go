// Package vaultpromalerts enforces T8.4 of plan 2026-06-02-openbao-adoption v1.3.
//
// Hermetic contract for PrometheusRule vault-audit-alerts. Asserts:
//
//   - infra/helm/monitoring/manifests/prometheusrule-vault-audit.yaml parses
//     cleanly as YAML.
//   - All 4 P1 alerts declared: VaultAuthFailureRateHigh,
//     VaultPolicyDenialRateHigh, VaultSealedOrDown, VaultAuditLogStalled.
//   - All alerts have severity=critical (no non-critical severities present).
//   - All 4 alerts carry runbook_url annotation.
//   - All 4 alerts have team=platform-security label (consistent routing).
//   - All 4 alerts have domain=vault label.
//   - VaultAuditLogStalled carries invariant: "audit-log-must-flow" label.
//
// Migrated from infra/tests/check-vault-prom-alerts_test.sh
// (M-Migrate2 2026-07-10 M16 T3.3 Wave 2). Shape 2 mix (text + YAML).
package vaultpromalerts

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
	"gopkg.in/yaml.v3"
)

const ruleFileRel = "infra/helm/monitoring/manifests/prometheusrule-vault-audit.yaml"

var expectedAlerts = []string{
	"VaultAuthFailureRateHigh",
	"VaultPolicyDenialRateHigh",
	"VaultSealedOrDown",
	"VaultAuditLogStalled",
}

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the aggregated result (canonical M-Migrate2 shape per blueprint ADR D1).
type Outcome struct {
	Failures []string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// ErrViolations signals ≥ 1 assertion failed.
var ErrViolations = errors.New("vault-prom-alerts hermetic contract violated")

// Check runs the assertions.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	rulePath := filepath.Join(root, ruleFileRel)
	content, err := readFile(rulePath)
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("PrometheusRule file missing: %s", ruleFileRel)}, Violation: ErrViolations}, nil
	}

	var failures []string

	// (1) YAML parses cleanly.
	var any yaml.Node
	if err := yaml.Unmarshal(content, &any); err != nil {
		failures = append(failures, fmt.Sprintf("YAML parse error: %v", err))
	}

	// (2) All 4 P1 alerts present (word-boundary anchored to avoid FooBar
	// matching the FooBar prefix inside FooBarBaz).
	for _, alert := range expectedAlerts {
		re := regexp.MustCompile(fmt.Sprintf(`alert:\s+%s\b`, regexp.QuoteMeta(alert)))
		if !re.Match(content) {
			failures = append(failures, fmt.Sprintf("alert %s MISSING", alert))
		}
	}

	// (3) All severities are critical (no non-critical present).
	severityRE := regexp.MustCompile(`(?m)^\s+severity:\s+(\S+)\s*$`)
	nonCritical := map[string]struct{}{}
	for _, m := range severityRE.FindAllStringSubmatch(string(content), -1) {
		if m[1] != "critical" {
			nonCritical[m[1]] = struct{}{}
		}
	}
	if len(nonCritical) > 0 {
		names := make([]string, 0, len(nonCritical))
		for k := range nonCritical {
			names = append(names, k)
		}
		failures = append(failures, fmt.Sprintf("non-critical severities found: %s", strings.Join(names, ",")))
	}

	// (4) runbook_url annotation count ≥ 4.
	if count := countMatches(content, `runbook_url:`); count < 4 {
		failures = append(failures, fmt.Sprintf("expected ≥ 4 runbook_url annotations, got %d", count))
	}

	// (5) team: platform-security count ≥ 4.
	if count := countMatches(content, `team:\s+platform-security`); count < 4 {
		failures = append(failures, fmt.Sprintf("expected ≥ 4 team=platform-security, got %d", count))
	}

	// (6) domain: vault count ≥ 4.
	if count := countMatches(content, `domain:\s+vault`); count < 4 {
		failures = append(failures, fmt.Sprintf("expected ≥ 4 domain=vault, got %d", count))
	}

	// (7) VaultAuditLogStalled invariant label.
	invariantRE := regexp.MustCompile(`invariant:\s+"audit-log-must-flow"`)
	if !invariantRE.Match(content) {
		failures = append(failures, "VaultAuditLogStalled MISSING invariant label")
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

// countMatches returns the number of regex matches in content.
func countMatches(content []byte, pattern string) int {
	return len(regexp.MustCompile(pattern).FindAll(content, -1))
}

func readFile(path string) ([]byte, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, err
	} else if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	return os.ReadFile(path)
}
