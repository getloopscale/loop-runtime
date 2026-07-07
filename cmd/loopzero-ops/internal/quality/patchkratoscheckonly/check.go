// Package patchkratoscheckonly enforces that
// infra/scripts/local/patch-kratos-dev-public.sh --check-only is a read-only
// alias for --dry-run --skip-restart. The dogfood-login Suite 0 pre-flight
// invokes `patch-kratos-dev-public.sh --check-only` as a Cleanroom-SURVIVAL
// gate — it MUST be read-only (no `kubectl apply`, no rollout restart), else
// the pre-flight would mutate the very Kratos config it validates.
//
// Migrated from infra/tests/check-patch-kratos-check-only-readonly_test.sh
// (M-Migrate2 continuation, 2026-07-07). Hermetic: NO network, NO kubectl —
// pure text walk over the target shell script.
package patchkratoscheckonly

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const targetPath = "infra/scripts/local/patch-kratos-dev-public.sh"

// checkOnlyFlag is the literal alias the pre-flight passes.
const checkOnlyFlag = "--check-only"

// The following regexps mirror the bash guard's assertions bit-for-bit
// (grep -E matches).
var (
	// The --check-only case handler MUST set DRY_RUN=1.
	checkOnlyDryRunRE = regexp.MustCompile(`--check-only\).*DRY_RUN=1`)
	// The --check-only case handler MUST set SKIP_RESTART=1.
	checkOnlySkipRestartRE = regexp.MustCompile(`--check-only\).*SKIP_RESTART=1`)
	// A DRY_RUN guard MUST gate the apply/mutation path.
	dryRunGuardRE = regexp.MustCompile(`DRY_RUN.*(-eq 1|= .?1)`)
)

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome carries the failure summary.
type Outcome struct {
	Failures []string
}

// ErrViolations means at least one assertion failed.
var ErrViolations = errors.New("patch-kratos-dev-public.sh --check-only invariants violated")

// Check runs the six assertions against the target shell script.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	full := filepath.Join(root, targetPath)

	var failures []string

	// A1 target script exists.
	body, err := os.ReadFile(full)
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("target script missing at %s", targetPath)}}, ErrViolations
	}

	// A2 bash -n syntax clean (requires bash on PATH; if bash is missing the
	// check falls through — same tolerance as `command -v bash` gates).
	if _, lookErr := exec.LookPath("bash"); lookErr == nil {
		cmd := exec.Command("bash", "-n", full)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			failures = append(failures, fmt.Sprintf("bash -n syntax check failed for %s: %s", targetPath, strings.TrimSpace(stderr.String())))
		}
	}

	// A3 --check-only flag present anywhere in the script.
	if !bytes.Contains(body, []byte(checkOnlyFlag)) {
		failures = append(failures, fmt.Sprintf("%s: --check-only flag not declared", targetPath))
	}

	// A4 --check-only) case handler sets DRY_RUN=1.
	if !checkOnlyDryRunRE.Match(body) {
		failures = append(failures, fmt.Sprintf("%s: --check-only) case handler does NOT set DRY_RUN=1 (guard against apply mutation)", targetPath))
	}

	// A5 --check-only) case handler sets SKIP_RESTART=1.
	if !checkOnlySkipRestartRE.Match(body) {
		failures = append(failures, fmt.Sprintf("%s: --check-only) case handler does NOT set SKIP_RESTART=1 (guard against rollout mutation)", targetPath))
	}

	// A6 apply/mutation path gated behind a DRY_RUN guard.
	if !dryRunGuardRE.Match(body) {
		failures = append(failures, fmt.Sprintf("%s: no DRY_RUN guard around apply path (expected regex `DRY_RUN.*(-eq 1|= .?1)`)", targetPath))
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}
