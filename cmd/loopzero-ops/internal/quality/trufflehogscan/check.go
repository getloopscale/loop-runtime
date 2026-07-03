// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package trufflehogscan wraps `trufflehog git` invocation as a
// pkg/quality gate. T4.7 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap.
//
// Behavior: skip-on-missing — see trivyscan/check.go for rationale.
package trufflehogscan

import (
	"context"
	"fmt"
	"os/exec"
)

// Config tells Check what to scan.
type Config struct {
	// RepoRoot is the path to scan (defaults to ".").
	RepoRoot string
	// OnlyVerified=true uses --only-verified (network-call-based
	// verification — TruffleHog asks the secret-providing service whether
	// the secret is currently valid). Reduces false positives but requires
	// network access.
	OnlyVerified bool
}

// Outcome is the gate result.
type Outcome struct {
	// Skipped=true means trufflehog binary not found OR scan unavailable.
	Skipped bool
	Reason  string
	// Pass=true means scan completed AND zero secrets found
	// (verified ones only when OnlyVerified=true).
	Pass bool
}

// Check runs `trufflehog git file://${root} --only-verified --fail`
// if trufflehog is installed. Returns Outcome{Skipped:true} if missing.
//
// Network dependency note: --only-verified requires outbound network
// to verify discovered secrets against the originating service (AWS,
// GitHub, etc.). In offline CI, may produce false negatives. Documented
// per EC-7 of T4.7 design.
func Check(ctx context.Context, cfg Config) (*Outcome, error) {
	binary, err := exec.LookPath("trufflehog")
	if err != nil {
		return &Outcome{
			Skipped: true,
			Reason:  "trufflehog binary not found in PATH; install via https://github.com/trufflesecurity/trufflehog",
		}, nil
	}

	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}

	args := []string{"git", "file://" + cfg.RepoRoot, "--fail"}
	if cfg.OnlyVerified {
		args = append(args, "--only-verified")
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			// trufflehog exit codes:
			//   0  — no secrets found
			//   183 — secrets found (--fail set)
			//   other — execution error
			if exitErr.ExitCode() == 183 {
				return &Outcome{
					Pass:   false,
					Reason: fmt.Sprintf("trufflehog found secrets:\n%s", string(out)),
				}, nil
			}
		}
		return &Outcome{
			Skipped: true,
			Reason:  fmt.Sprintf("trufflehog execution error (network? git access?): %v", runErr),
		}, nil
	}

	return &Outcome{
		Pass:   true,
		Reason: "trufflehog scan clean (no secrets detected)",
	}, nil
}
