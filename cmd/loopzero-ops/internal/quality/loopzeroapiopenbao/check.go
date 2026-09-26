// Package loopzeroapiopenbao enforces T6.1 of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2 — the loopzero-api install wires the
// AppRole secret_id from K8s Secret `loopzero-openbao-approle` via envFrom.secretRef
// with optional=true, and never as an inline value.
//
// Defense against the canonical drift mode: an inline-literal secret_id
// reintroduced via `kubectl set env` on a hot-fix branch.
//
// # What is checked, and where
//
// The install is charts/loopzero-api with charts/loopzero-api/values-bootstrap.yaml —
// the one loopzero-api chart Helmfile installs. Until 2026-09-25 this gate rendered
// the bootstrap copy infra/helm/loopzero-api, whose template hard-coded the mount
// and carried the plan and runbook citations in its comments. That copy was
// deleted (docs/decisions.md, "One chart per bootstrap/published pair"); the
// mount is now a typed secrets.additionalSources entry, so:
//
//   - the RENDER is checked for the mount, its optionality and the absence of an
//     inline secret_id (helm renders the chart with the bootstrap values);
//   - the VALUES SOURCE is checked for the plan and runbook citations, because
//     helm does not render values comments.
//
// Migrated from infra/tests/helm/loopzero-api-openbao-approle-envfrom_test.sh
// (M-Migrate2 continuation 2026-07-07). Renders the chart via `helm template`
// subprocess and asserts on the output. Same Anti-Pattern #8 tradeoff as
// shellcheck-advisory: helm is the only tool that renders the chart; the
// subprocess boundary is legitimate.
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

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// chartPath is the loopzero-api chart; valuesPath the values Helmfile installs it with.
const (
	chartPath  = "charts/loopzero-api"
	valuesPath = "charts/loopzero-api/values-bootstrap.yaml"
)

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
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation, Skip when helm is not on
// PATH (the gate is advisory then and rendered nothing), and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	switch {
	case o.Violation != nil:
		return verdict.Fail
	case o.Warn:
		return verdict.Skip
	}
	return verdict.Pass
}

// ErrViolations means the rendered chart is missing the canonical envFrom wiring.
var ErrViolations = errors.New("loopzero-api rendered chart is missing the canonical loopzero-openbao-approle envFrom wiring")

// ErrChartMissing means the chart directory itself is absent.
var ErrChartMissing = errors.New("loopzero-api chart missing")

// inlineValueRE catches an `- name: OPENBAO_VAULT_SECRET_ID` env entry that
// carries a literal `value:` on the very next non-comment line — the exact
// anti-pattern the guard closes.
// approleNameRE matches the Secret name line, quoted or bare.
var approleNameRE = regexp.MustCompile(`(?m)name:\s*"?loopzero-openbao-approle"?\s*$`)

var inlineValueRE = regexp.MustCompile(`(?m)^\s*-\s*name:\s*OPENBAO_VAULT_SECRET_ID\s*\n\s*value:`)

// Check renders the chart and applies the envFrom contract to the output.
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
	valuesFile := filepath.Clean(filepath.Join(root, valuesPath))
	source, err := os.ReadFile(valuesFile)
	if errors.Is(err, fs.ErrNotExist) {
		return Outcome{}, fmt.Errorf("%w: %s holds the install this gate checks", ErrChartMissing, valuesPath)
	}
	if err != nil {
		return Outcome{}, fmt.Errorf("read %s: %w", valuesPath, err)
	}
	if _, err := exec.LookPath("helm"); err != nil {
		return Outcome{Warn: true}, nil
	}

	// helm template loopzero-api <chart> -f <bootstrap values>. --dependency-update
	// resolves the local loopzero-common library the chart's templates include.
	cmd := exec.Command("helm", "template", "loopzero-api", full, "-f", valuesFile, "--dependency-update") // #nosec G204 -- both paths are constants of this gate joined to the repo root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("helm template failed: %v\n%s", err, stderr.String())}, Violation: ErrViolations}, nil
	}
	failures := append(checkRendered(stdout.String()), checkSource(string(source))...)
	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

// checkRendered applies the envFrom contract to a rendered chart and returns
// one failure per broken assertion.
func checkRendered(rendered string) []string {
	var failures []string
	assertContains := func(desc, needle string) {
		if !strings.Contains(rendered, needle) {
			failures = append(failures, fmt.Sprintf("MISSING: %s (needle `%s`)", desc, needle))
		}
	}

	// A — envFrom block contains the new secretRef
	// A name renders quoted when it comes from values (loopzero-common quotes every
	// name it emits) and bare when a template spells it, so both spellings count.
	if !approleNameRE.MatchString(rendered) {
		failures = append(failures, "MISSING: A1 envFrom references loopzero-openbao-approle secret (needle `name: loopzero-openbao-approle`)")
	}

	// A2 — loopzero-openbao-approle secretRef must be optional=true.
	// Extract the window around the loopzero-openbao-approle secret name and grep
	// for `optional: true` — bash equivalent: awk '/name: loopzero-openbao-approle/,/optional:/' | grep -q 'optional: true'.
	if !hasOptionalTrueAfter(rendered, approleNameRE) {
		failures = append(failures, "MISSING: A2 loopzero-openbao-approle secretRef is not `optional: true`")
	}

	// C — anti-hardcode gate: no inline `value:` following OPENBAO_VAULT_SECRET_ID
	if inlineValueRE.MatchString(rendered) {
		failures = append(failures, "FORBIDDEN: C1 inline `value:` after `name: OPENBAO_VAULT_SECRET_ID` (secret_id must flow via envFrom, never inline)")
	}

	// D — sibling envFrom entries still render
	assertContains("D1 loopzero-service-auth envFrom still present", "name: loopzero-service-auth")

	return failures
}

// checkSource applies the traceability assertions to the values source that
// declares the mount, and returns one failure per broken assertion.
func checkSource(source string) []string {
	var failures []string
	if !strings.Contains(source, "2026-06-10-j22-openbao-transit-eso-sync") {
		failures = append(failures, fmt.Sprintf("MISSING: B1 %s does not cite plan 2026-06-10-j22-openbao-transit-eso-sync", valuesPath))
	}
	// The needle used to be `openbao-transit-cutover-runbook.md`, consolidated into
	// `docs/runbooks.md` (section `Transit cutover`). Requiring a deleted file name asks
	// the chart to point at what nobody can open; this requires the path a reader follows.
	if !strings.Contains(source, "docs/runbooks.md") {
		failures = append(failures, fmt.Sprintf("MISSING: B2 %s does not point at the cutover runbook (docs/runbooks.md)", valuesPath))
	}
	return failures
}

// hasOptionalTrueAfter searches the window from the first line matching
// anchor down to the next non-empty line whose leading whitespace is equal
// to or less than anchor's indent (i.e. the next same-or-outer YAML key).
// Returns true if `optional: true` appears in that window.
func hasOptionalTrueAfter(text string, anchor *regexp.Regexp) bool {
	lines := strings.Split(text, "\n")
	anchorIdx := -1
	anchorIndent := 0
	for i, line := range lines {
		if !anchor.MatchString(line) {
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
