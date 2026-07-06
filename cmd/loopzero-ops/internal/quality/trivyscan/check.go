// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package trivyscan wraps `trivy fs` invocation as a pkg/quality gate.
// T4.7 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md.
//
// Behavior: skip-on-missing — if trivy binary is absent (operator hasn't
// installed locally), the gate reports Skipped=true rather than failing.
// This avoids cascade failures when running `task quality:gates` em
// dev environments without security tooling. Real PR-time enforcement
// requires the chosen CI platform (T0.2 of plan) to provide trivy.
package trivyscan

import (
	"context"
	"fmt"
	"os/exec"
)

// Config tells Check what to scan + which severities count as fail.
type Config struct {
	// RepoRoot is the path to scan (passed as `trivy fs <root>`).
	RepoRoot string
	// Severities filters which findings count (e.g., "CRITICAL,HIGH").
	// Empty defaults to "CRITICAL,HIGH" per CLAUDE.md supply-chain.
	Severities string
}

// Outcome is the gate result.
type Outcome struct {
	// Skipped=true means trivy binary not found OR scan unavailable.
	// Operators should install trivy + re-run to enforce.
	Skipped bool
	// Reason for skip OR pass/fail summary.
	Reason string
	// Pass=true means scan completed AND zero matching-severity findings.
	Pass bool
}

// Check runs `trivy fs --severity ${SEV} --exit-code 1 <repo>` if trivy
// is installed. Returns Outcome with Skipped=true if trivy missing.
//
// Skip-on-missing rationale: pkg/quality gates are designed to run em
// developer laptops + CI alike. trivy is a 100MB+ download with vuln DB
// sync requirements; not all dev machines have it. Skipping here logs
// the absence but does NOT fail the local gate run. PR-time enforcement
// happens in the real CI platform (T0.2 of plan), which IS expected
// to have trivy provisioned.
func Check(ctx context.Context, cfg Config) (*Outcome, error) {
	binary, err := exec.LookPath("trivy")
	if err != nil {
		return &Outcome{
			Skipped: true,
			Reason:  "trivy binary not found in PATH; install via https://aquasecurity.github.io/trivy",
		}, nil
	}

	severities := cfg.Severities
	if severities == "" {
		severities = "CRITICAL,HIGH"
	}
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}

	// Use --quiet to suppress progress output; --exit-code 1 makes trivy
	// fail on matching-severity findings.
	cmd := exec.CommandContext(ctx, binary, "fs",
		"--quiet",
		"--severity", severities,
		"--exit-code", "1",
		"--scanners", "vuln",
		cfg.RepoRoot,
	)

	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		// exit 1 means findings present; any other exit means trivy itself failed
		if exitErr, ok := runErr.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return &Outcome{
				Pass:   false,
				Reason: fmt.Sprintf("trivy fs found vulnerabilities (severity=%s):\n%s", severities, string(out)),
			}, nil
		}
		return &Outcome{
			Skipped: true,
			Reason:  fmt.Sprintf("trivy execution error (DB sync? network?): %v", runErr),
		}, nil
	}

	return &Outcome{
		Pass:   true,
		Reason: fmt.Sprintf("trivy fs clean (severity=%s)", severities),
	}, nil
}
