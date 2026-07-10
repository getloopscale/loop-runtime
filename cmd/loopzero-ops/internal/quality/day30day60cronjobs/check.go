// Package day30day60cronjobs enforces T10.1-T10.4 of plan
// 2026-06-02-openbao-adoption v1.3.
//
// Hermetic test for Day 30 verify + Day 60 AES delete CronJobs. Renders
// infra/helm/loopzero-api chart with different --set overrides and asserts:
//
//   - Disabled-by-default: both crons absent when vault.*.enabled=false
//     (default off).
//   - Day 30 enabled: cron name + schedule "0 3 1 * *" + EC-12 filter flag
//     + concurrencyPolicy: Forbid + security hardening (runAsNonRoot +
//     readOnlyRootFilesystem).
//   - Day 60 enabled: cron name + --dry-run=true + --shred-aes-key=false
//     (safety defaults for irreversible ops) + migrate-to-transit subcmd +
//     same security hardening.
//
// Same "advisory-fallthrough when helm absent" pattern as loopzeroapiopenbao.
//
// Migrated from infra/tests/check-day30-day60-cronjobs_test.sh
// (M-Migrate2 2026-07-10 M16 T3.4 Wave 2). Shape 2 (helm-template).
package day30day60cronjobs

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

const chartPath = "infra/helm/loopzero-api"

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	Warn     bool // helm not on PATH (advisory-only)
	Failures []string
}

// ErrViolations signals ≥ 1 assertion failed.
var ErrViolations = errors.New("day30-day60-cronjobs hermetic contract violated")

// ErrChartMissing signals the loopzero-api chart directory is absent.
var ErrChartMissing = errors.New("loopzero-api chart missing")

// Check runs the 3 chart renders and validates assertions.
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

	renderOff, err := helmTemplate(full, nil)
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("helm template (off) failed: %v", err)}}, ErrViolations
	}
	renderDay30, err := helmTemplate(full, []string{"vault.day30VerifyCron.enabled=true"})
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("helm template (day30) failed: %v", err)}}, ErrViolations
	}
	renderDay60, err := helmTemplate(full, []string{"vault.day60AesDeleteCron.enabled=true"})
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("helm template (day60) failed: %v", err)}}, ErrViolations
	}

	var failures []string

	// Disabled-by-default: neither cron rendered.
	if regexp.MustCompile(`name: loopzero-secrets-day30-verify`).Match(renderOff) {
		failures = append(failures, "Day 30 cron rendered when enabled=false (gate broken)")
	}
	if regexp.MustCompile(`name: loopzero-secrets-day60-aes-delete`).Match(renderOff) {
		failures = append(failures, "Day 60 cron rendered when enabled=false (gate broken)")
	}

	// Day 30 enabled: 4 assertions.
	if !regexp.MustCompile(`name: loopzero-secrets-day30-verify`).Match(renderDay30) {
		failures = append(failures, "Day 30 cron NOT rendered when enabled=true")
	}
	if !regexp.MustCompile(`schedule: "0 3 1 \* \*"`).Match(renderDay30) {
		failures = append(failures, "Day 30 default schedule not '0 3 1 * *'")
	}
	if !regexp.MustCompile(`--filter-deleted-tenants`).Match(renderDay30) {
		failures = append(failures, "Day 30 EC-12 --filter-deleted-tenants flag MISSING")
	}
	if !regexp.MustCompile(`concurrencyPolicy: Forbid`).Match(renderDay30) {
		failures = append(failures, "Day 30 concurrencyPolicy: Forbid MISSING")
	}

	// Day 60 enabled: 4 assertions.
	if !regexp.MustCompile(`name: loopzero-secrets-day60-aes-delete`).Match(renderDay60) {
		failures = append(failures, "Day 60 cron NOT rendered when enabled=true")
	}
	if !regexp.MustCompile(`--dry-run=true`).Match(renderDay60) {
		failures = append(failures, "Day 60 default --dry-run=true MISSING (safety)")
	}
	if !regexp.MustCompile(`--shred-aes-key=false`).Match(renderDay60) {
		failures = append(failures, "Day 60 default --shred-aes-key=false MISSING (safety)")
	}
	if !regexp.MustCompile(`migrate-to-transit`).Match(renderDay60) {
		failures = append(failures, "Day 60 command DOES NOT invoke migrate-to-transit subcmd")
	}

	// Both crons: security hardening.
	for _, r := range []struct {
		name    string
		content []byte
	}{{"day30", renderDay30}, {"day60", renderDay60}} {
		if !regexp.MustCompile(`runAsNonRoot: true`).Match(r.content) {
			failures = append(failures, fmt.Sprintf("%s cron: runAsNonRoot: true MISSING", r.name))
		}
		if !regexp.MustCompile(`readOnlyRootFilesystem: true`).Match(r.content) {
			failures = append(failures, fmt.Sprintf("%s cron: readOnlyRootFilesystem: true MISSING", r.name))
		}
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}

// helmTemplate runs `helm template loopzero-api <chart> [--set K=V]…` and returns stdout.
func helmTemplate(chartDir string, sets []string) ([]byte, error) {
	args := []string{"template", "loopzero-api", chartDir}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	cmd := exec.Command("helm", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm %v: %v\n%s", args, err, stderr.String())
	}
	return stdout.Bytes(), nil
}
