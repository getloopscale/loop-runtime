// Package devpublicbillingportalsyntax is a hermetic contract test for
// infra/tests/e2e/live/dev-public-billing-portal_test.sh.
//
// Reproduces the 8 assertions from the original bash test
// (infra/tests/check-dev-public-billing-portal-syntax_test.sh) as
// pure-logic Go:
//
//  1. file exists                                    (os.Stat)
//  2. bash -n syntax OK                              (exec.Command "bash" "-n")
//  3. set -euo pipefail strict mode declared         (regexp)
//  4. log function defined                           (regexp)
//  5. assert function defined                        (regexp)
//  6. Stripe billing portal endpoint referenced      (regexp)
//  7. Exit codes 0/1/2 only (no foreign codes)       (regexp + set-difference)
//  8. Script name allowlisted                        (bytes.Contains)
//
// Migrated from infra/tests/check-dev-public-billing-portal-syntax_test.sh
// (M-Migrate2 2026-07-10 M20 T2.1 Wave A gate 1/8). Follows M16 T2.1
// sprintsyntax canonical shape verbatim per M20 blueprint ADR-D1.
// Requires bash on PATH.
package devpublicbillingportalsyntax

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	scriptRel    = "infra/tests/e2e/live/dev-public-billing-portal_test.sh"
	allowlistRel = ".claude/rules/shell-scripts-allowlist.md"
	allowlistTag = "infra/tests/e2e/live/dev-public-billing-portal_test.sh"
)

var (
	strictModeRE   = regexp.MustCompile(`(?m)^set -euo pipefail`)
	logFuncRE      = regexp.MustCompile(`(?m)^log\(\) \{`)
	assertFuncRE   = regexp.MustCompile(`(?m)^assert\(\) \{`)
	billingRE      = regexp.MustCompile(`billing|/v1/billing|stripe`)
	exitCodeRE     = regexp.MustCompile(`(?m)^exit ([0-9]+)`)
	allowedExits   = map[string]bool{"0": true, "1": true, "2": true}
)

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the aggregated result. Failures collect-all pattern per M16 canonical.
type Outcome struct {
	Failures []string
}

// ErrViolations signals ≥ 1 assertion failed.
var ErrViolations = errors.New("check-dev-public-billing-portal-syntax hermetic contract violated")

// Check runs the 8 assertions and returns (Outcome, error).
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	script := filepath.Join(root, scriptRel)
	allowlist := filepath.Join(root, allowlistRel)

	var failures []string

	// 1. file exists + read content
	scriptBytes, existsErr := statAndRead(script)
	if existsErr != nil {
		failures = append(failures, fmt.Sprintf("file exists: %s missing", scriptRel))
	}

	// 2. bash -n syntax OK
	if existsErr == nil {
		if err := runQuiet("bash", "-n", script); err != nil {
			failures = append(failures, fmt.Sprintf("bash -n %s: %v", scriptRel, err))
		}
	}

	// 3-6 pattern assertions on script content
	if scriptBytes != nil {
		content := string(scriptBytes)

		if !strictModeRE.MatchString(content) {
			failures = append(failures, "set -euo pipefail strict mode: not declared at line start")
		}
		if !logFuncRE.MatchString(content) {
			failures = append(failures, "log function defined: `log() {` not found at line start")
		}
		if !assertFuncRE.MatchString(content) {
			failures = append(failures, "assert function defined: `assert() {` not found at line start")
		}
		if !billingRE.MatchString(content) {
			failures = append(failures, "Stripe billing portal probe: no reference to billing|/v1/billing|stripe")
		}

		// 7. Exit codes 0/1/2 only
		matches := exitCodeRE.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if !allowedExits[m[1]] {
				failures = append(failures, fmt.Sprintf(
					"exit 0/1/2 contract: found foreign exit code %s (allowed: 0, 1, 2)", m[1]))
				break // report only the first foreign code
			}
		}
	}

	// 8. Script name allowlisted
	allowBytes, err := statAndRead(allowlist)
	if err != nil {
		failures = append(failures, fmt.Sprintf("allowlist file exists: %s missing", allowlistRel))
	} else if !strings.Contains(string(allowBytes), allowlistTag) {
		failures = append(failures, fmt.Sprintf(
			"script allowlisted: %q not found in %s", allowlistTag, allowlistRel))
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}

func statAndRead(path string) ([]byte, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func runQuiet(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
