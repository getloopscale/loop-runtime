// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package trivyscan wraps `trivy fs` invocation as a quality gate.
//
// The gate fails CLOSED. A missing binary, a scan that dies with a FATAL, and any
// exit code trivy does not document as "done" are returned as typed errors, never
// as a pass or a skip: each of them produces exactly the silence a clean tree
// produces, so reporting them as green would make the gate unable to fail on its
// own breakage. trufflehogscan closed the same hole for secrets after a live
// credential survived it; this package follows the same contract.
package trivyscan

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// The two ways a scan can fail to establish a verdict. Both are typed so the
// caller can never collapse "could not verify" into "nothing found".
var (
	// ErrScannerMissing means trivy is not installed where the gate runs.
	ErrScannerMissing = errors.New("trivy not found in PATH — install it (https://aquasecurity.github.io/trivy); without it a clean tree and an unscanned tree look identical")
	// ErrScanFailed means trivy ran and could not complete the scan.
	ErrScanFailed = errors.New("trivy could not complete the scan — this is not a vulnerability finding, and not a pass")
)

const defaultSeverities = "CRITICAL,HIGH"

// Scanner runs the vulnerability scan. It is the seam that keeps the binary, the
// vulnerability DB and the network out of the unit tests.
//
// It returns the combined output and the process exit code. A non-nil error means
// the process could not be run at all and must wrap ErrScannerMissing or
// ErrScanFailed.
type Scanner interface {
	Scan(ctx context.Context, root, severities string) (output string, exitCode int, err error)
}

// Config tells Check what to scan and which severities count as a failure.
type Config struct {
	// RepoRoot is the path to scan (passed as `trivy fs <root>`). Empty means ".".
	RepoRoot string
	// Severities filters which findings count (e.g. "CRITICAL,HIGH").
	// Empty defaults to "CRITICAL,HIGH".
	Severities string
	// Scanner defaults to the real trivy process.
	Scanner Scanner
}

// Outcome is the verdict of a scan that completed.
type Outcome struct {
	// Pass is true when the scan completed with zero matching-severity findings.
	Pass bool
	// Reason is the pass summary or the findings table.
	Reason string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Check scans cfg.RepoRoot and returns the verdict. It returns an error wrapping
// ErrScannerMissing or ErrScanFailed whenever no complete scan happened.
func Check(ctx context.Context, cfg Config) (*Outcome, error) {
	severities := cfg.Severities
	if severities == "" {
		severities = defaultSeverities
	}
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	scanner := cfg.Scanner
	if scanner == nil {
		scanner = execScanner{}
	}

	out, exitCode, err := scanner.Scan(ctx, root, severities)
	if err != nil {
		return nil, err
	}
	return classifyExit(out, exitCode, severities)
}

// classifyExit maps trivy's exit code to a verdict. `--exit-code 1` makes trivy
// exit 1 on matching findings, but trivy also exits 1 on a FATAL, so exit 1 is a
// finding only when the output carries no FATAL line (see classifyExitOne).
func classifyExit(out string, exitCode int, severities string) (*Outcome, error) {
	switch exitCode {
	case 0:
		return &Outcome{Pass: true, Reason: fmt.Sprintf("trivy fs clean (severity=%s)", severities)}, nil
	case 1:
		if fatal, detail := classifyExitOne(out); fatal {
			return nil, fmt.Errorf("%w (severity=%s): %s", ErrScanFailed, severities, detail)
		}
		return &Outcome{
			Pass:   false,
			Reason: fmt.Sprintf("trivy fs found vulnerabilities (severity=%s):\n%s", severities, out),
		}, nil
	default:
		return nil, fmt.Errorf("%w (severity=%s): exit code %d: %s", ErrScanFailed, severities, exitCode, out)
	}
}

// execScanner runs the real trivy process.
type execScanner struct {
	// binary defaults to "trivy".
	binary string
}

func (e execScanner) Scan(ctx context.Context, root, severities string) (string, int, error) {
	bin := e.binary
	if bin == "" {
		bin = "trivy"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %w", ErrScannerMissing, err)
	}

	// --quiet suppresses progress output; --exit-code 1 makes trivy fail on
	// matching-severity findings.
	// #nosec G204 -- path comes from exec.LookPath over a fixed binary name; the
	// remaining arguments are the operator's own flags.
	cmd := exec.CommandContext(ctx, path, "fs",
		"--quiet",
		"--severity", severities,
		"--exit-code", "1",
		"--scanners", "vuln",
		root,
	)
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		return string(out), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return string(out), exitErr.ExitCode(), nil
	}
	return string(out), 0, fmt.Errorf("%w: %w", ErrScanFailed, runErr)
}
