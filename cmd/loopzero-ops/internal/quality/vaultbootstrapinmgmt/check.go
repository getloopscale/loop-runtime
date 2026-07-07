// Package vaultbootstrapinmgmt is a hermetic contract test for the
// `task k3d:vault:bootstrap-in-mgmt` shortcut (Option (c) of cleanroom
// iter 23 cross-cluster reachability Open Question).
//
// Locks that the shortcut canonical-mirrors the upstream
// `task k3d:vault:bootstrap` task with EXACTLY two well-known differences:
//  1. context k3d-loopzero-vault → k3d-loopzero-mgmt (everywhere)
//  2. pre-flight error message references `task setup` instead of
//     `task k3d:vault:create`
//
// Defense against drift: if upstream `task k3d:vault:bootstrap` adds a new
// helmfile selector or kubectl wait step, this test fails so we remember
// to mirror the change.
//
// Migrated from infra/tests/check-vault-bootstrap-in-mgmt-task_test.sh
// (M-Migrate2 continuation 2026-07-07). Hermetic — no kubectl, no helmfile
// install — pure text walk on taskfiles/k3d.yml.
package vaultbootstrapinmgmt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const taskfilePath = "taskfiles/k3d.yml"

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	Failures []string
}

// ErrViolations means at least one assertion failed.
var ErrViolations = errors.New("k3d:vault:bootstrap-in-mgmt shortcut contract violated")

// Check runs the 15+ assertions from the bash guard.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	full := filepath.Join(root, taskfilePath)
	body, err := os.ReadFile(full)
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("taskfile missing at %s: %v", taskfilePath, err)}}, ErrViolations
	}
	text := string(body)

	var failures []string
	assert := func(desc string, ok bool) {
		if !ok {
			failures = append(failures, fmt.Sprintf("FAIL: %s", desc))
		}
	}

	// A — both tasks registered
	assert("A1 task k3d:vault:bootstrap-in-mgmt registered",
		regexp.MustCompile(`(?m)^  k3d:vault:bootstrap-in-mgmt:$`).MatchString(text))
	assert("A2 task k3d:vault:status-in-mgmt sibling registered",
		regexp.MustCompile(`(?m)^  k3d:vault:status-in-mgmt:$`).MatchString(text))

	// Extract the two task bodies (upstream + shortcut).
	upstream := extractTaskBody(text, "  k3d:vault:bootstrap:", "  k3d:vault:bootstrap-in-mgmt:")
	shortcut := extractTaskBody(text, "  k3d:vault:bootstrap-in-mgmt:", "  k3d:vault:status-in-mgmt:")

	upstreamHelmfiles := countMatches(upstream, `helmfile -f `)
	shortcutHelmfiles := countMatches(shortcut, `helmfile -f `)
	upstreamWaits := countLines(upstream, regexp.MustCompile(`kubectl --context .* wait`))
	shortcutWaits := countLines(shortcut, regexp.MustCompile(`kubectl --context .* wait`))

	// B — helmfile selector chain mirrors upstream (no drift)
	assert(fmt.Sprintf("B1 shortcut has same number of helmfile syncs as upstream (upstream=%d shortcut=%d)", upstreamHelmfiles, shortcutHelmfiles),
		upstreamHelmfiles == shortcutHelmfiles && upstreamHelmfiles > 0)
	assert(fmt.Sprintf("B2 shortcut has same number of kubectl waits as upstream (upstream=%d shortcut=%d)", upstreamWaits, shortcutWaits),
		upstreamWaits == shortcutWaits && upstreamWaits > 0)

	for _, selector := range []string{"cert-manager", "cluster-issuer", "openbao-cert", "openbao"} {
		assert(fmt.Sprintf("B-%s shortcut syncs --selector name=%s", selector, selector),
			strings.Contains(shortcut, fmt.Sprintf("selector name=%s sync", selector)))
	}

	// C — context swap is total
	assert("C1 shortcut body uses kube-context k3d-loopzero-mgmt somewhere",
		strings.Contains(shortcut, "kube-context k3d-loopzero-mgmt"))
	assert("C2 shortcut body does NOT reference k3d-loopzero-vault (no copy-paste drift)",
		!strings.Contains(shortcut, "k3d-loopzero-vault"))

	// D — pre-flight error message
	assert("D1 shortcut pre-flight references 'task setup'",
		strings.Contains(shortcut, "task setup"))
	assert("D2 shortcut pre-flight does NOT reference 'task k3d:vault:create' (mgmt comes from task setup)",
		!strings.Contains(shortcut, "task k3d:vault:create"))

	// E — desc block cites runbook + iter 23 audit
	assert("E1 desc cites docs/operations/openbao-transit-cutover-runbook.md",
		strings.Contains(shortcut, "openbao-transit-cutover-runbook.md"))
	assert("E2 desc cites option (c) iter 23",
		strings.Contains(shortcut, "Option (c)"))
	assert("E3 desc names the trade-off (loses control-plane/secret-storage isolation)",
		strings.Contains(shortcut, "isolation"))
	assert("E4 desc warns prod must use canonical k3d:vault:bootstrap",
		strings.Contains(shortcut, "prod cutover MUST"))

	// F — anti-patterns
	assert("F1 no Co-Authored-By trailer references",
		!regexp.MustCompile(`(?i)Co-Authored-By`).MatchString(shortcut))
	assert("F2 no --no-verify flag in cmds",
		!strings.Contains(shortcut, "--no-verify"))

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}

// extractTaskBody returns text from the line beginning with startPrefix up to
// (but not including) the line beginning with endPrefix. If startPrefix is
// not found, returns "". If endPrefix is not found, returns from start to EOF.
// Ports the bash awk range `/^X$/,/^Y$/`.
func extractTaskBody(text, startPrefix, endPrefix string) string {
	lines := strings.Split(text, "\n")
	var out []string
	in := false
	for _, l := range lines {
		if !in {
			if strings.HasPrefix(l, startPrefix) {
				in = true
				out = append(out, l)
			}
			continue
		}
		if strings.HasPrefix(l, endPrefix) {
			break
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// countMatches returns the number of substring occurrences of needle in body.
// Ports `grep -c "needle"` — line-based, so we count lines that contain it.
func countMatches(body, needle string) int {
	n := 0
	for _, l := range strings.Split(body, "\n") {
		if strings.Contains(l, needle) {
			n++
		}
	}
	return n
}

// countLines returns the number of lines matching re. Ports `grep -c -E`.
func countLines(body string, re *regexp.Regexp) int {
	n := 0
	for _, l := range strings.Split(body, "\n") {
		if re.MatchString(l) {
			n++
		}
	}
	return n
}
