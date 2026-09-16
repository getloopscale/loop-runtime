// Package loopzeroapiopenbao enforces T6.1 of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2 — the loopzero-api chart
// unconditionally wires the AppRole secret_id from K8s Secret
// `loopzero-openbao-approle` via envFrom.secretRef with optional=true.
//
// Defense against the canonical drift mode (inline-literal secret_id
// reintroduced via `kubectl set env` on a hot-fix branch) — caught
// canonically by feedback_no_hardcode_gate_2026_06_09 +
// feedback_secret_canonical_no_hardcode.
//
// Migrated from infra/tests/helm/loopzero-api-openbao-approle-envfrom_test.sh
// (M-Migrate2 continuation 2026-07-07). Renders the loopzero-api chart via
// `helm template` subprocess and asserts on the output. Same Anti-Pattern
// #8 tradeoff as shellcheck-advisory: helm is the only tool that renders
// the chart; the subprocess boundary is legitimate.
package loopzeroapiopenbao

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const chartPath = "infra/helm/loopzero-api"

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	// Warn is true when helm is not on PATH (advisory-only fallthrough —
	// mirrors the same pattern as ory-kratos-lifespan's optional helmfile branch).
	Warn     bool
	Failures []string
}

// ErrViolations means the rendered chart is missing the canonical envFrom wiring.
var ErrViolations = errors.New("loopzero-api rendered chart is missing the canonical loopzero-openbao-approle envFrom wiring")

// ErrChartMissing means the chart directory itself is absent.
var ErrChartMissing = errors.New("loopzero-api chart missing")

// inlineValueRE catches an `- name: OPENBAO_VAULT_SECRET_ID` env entry that
// carries a literal `value:` on the very next non-comment line — the exact
// anti-pattern the guard closes.
var inlineValueRE = regexp.MustCompile(`(?m)^\s*-\s*name:\s*OPENBAO_VAULT_SECRET_ID\s*\n\s*value:`)

// Check runs the 6 rendered-chart assertions.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	full := filepath.Join(root, chartPath)
	if _, err := os.Stat(full); errors.Is(err, fs.ErrNotExist) {
		return Outcome{}, ErrChartMissing
	}
	if _, err := exec.LookPath("helm"); err != nil {
		return Outcome{Warn: true}, nil
	}

	// helm template loopzero-api <chart> -f <chart>/values.yaml
	valuesFile := filepath.Join(full, "values.yaml")
	cmd := exec.Command("helm", "template", "loopzero-api", full, "-f", valuesFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("helm template failed: %v\n%s", err, stderr.String())}}, ErrViolations
	}
	rendered := stdout.String()

	var failures []string
	assertContains := func(desc, needle string) {
		if !strings.Contains(rendered, needle) {
			failures = append(failures, fmt.Sprintf("MISSING: %s (needle `%s`)", desc, needle))
		}
	}

	// A — envFrom block contains the new secretRef
	assertContains("A1 envFrom references loopzero-openbao-approle secret", "name: loopzero-openbao-approle")

	// A2 — loopzero-openbao-approle secretRef must be optional=true.
	// Extract the window around the loopzero-openbao-approle secret name and grep
	// for `optional: true` — bash equivalent: awk '/name: loopzero-openbao-approle/,/optional:/' | grep -q 'optional: true'.
	if !hasOptionalTrueAfter(rendered, "name: loopzero-openbao-approle") {
		failures = append(failures, "MISSING: A2 loopzero-openbao-approle secretRef is not `optional: true`")
	}

	// B — comment block cites plan + runbook
	assertContains("B1 chart comment cites plan 2026-06-10-j22-openbao-transit-eso-sync", "2026-06-10-j22-openbao-transit-eso-sync")
	// O needle era `openbao-transit-cutover-runbook.md`, consolidado em `docs/runbooks.md`
	// (secao `Transit cutover`). Exigir um nome de arquivo apagado pede ao chart que aponte
	// para o que ninguem pode abrir; este exige o caminho que um leitor segue hoje.
	assertContains("B2 chart comment points at the cutover runbook", "docs/runbooks.md")

	// C — anti-hardcode gate: no inline `value:` following OPENBAO_VAULT_SECRET_ID
	if inlineValueRE.MatchString(rendered) {
		failures = append(failures, "FORBIDDEN: C1 inline `value:` after `name: OPENBAO_VAULT_SECRET_ID` (secret_id must flow via envFrom, never inline)")
	}

	// D — sibling envFrom entries still render
	assertContains("D1 loopzero-service-auth envFrom still present", "name: loopzero-service-auth")

	// E — memory pointer cited
	assertContains("E1 chart comment references the secret-canonical memory", "feedback_secret_canonical_no_hardcode")

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}

// hasOptionalTrueAfter searches the window from the first line matching
// anchor down to the next non-empty line whose leading whitespace is equal
// to or less than anchor's indent (i.e. the next same-or-outer YAML key).
// Returns true if `optional: true` appears in that window.
func hasOptionalTrueAfter(text, anchor string) bool {
	lines := strings.Split(text, "\n")
	anchorIdx := -1
	anchorIndent := 0
	for i, line := range lines {
		if !strings.Contains(line, anchor) {
			continue
		}
		anchorIdx = i
		anchorIndent = len(line) - len(strings.TrimLeft(line, " \t"))
		break
	}
	if anchorIdx < 0 {
		return false
	}
	for j := anchorIdx + 1; j < len(lines); j++ {
		l := lines[j]
		if strings.TrimSpace(l) == "" {
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " \t"))
		if indent < anchorIndent && !strings.HasPrefix(strings.TrimSpace(l), "-") {
			return false
		}
		if strings.Contains(l, "optional: true") {
			return true
		}
	}
	return false
}
