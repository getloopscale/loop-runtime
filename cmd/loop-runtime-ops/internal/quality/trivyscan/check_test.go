// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package trivyscan

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeScanner stands in for the trivy process so every exit path is reachable
// without the binary, the vulnerability DB or the network.
type fakeScanner struct {
	output   string
	exitCode int
	err      error

	gotRoot       string
	gotSeverities string
}

func (f *fakeScanner) Scan(_ context.Context, root, severities string) (string, int, error) {
	f.gotRoot, f.gotSeverities = root, severities
	return f.output, f.exitCode, f.err
}

func TestCheck_FailsClosedWhenTrivyIsMissing(t *testing.T) {
	scanner := &fakeScanner{err: ErrScannerMissing}

	got, err := Check(context.Background(), Config{RepoRoot: ".", Scanner: scanner})

	if !errors.Is(err, ErrScannerMissing) {
		t.Fatalf("err = %v, want ErrScannerMissing: an absent scanner must not read as a clean tree", err)
	}
	if got != nil {
		t.Errorf("outcome = %+v, want nil when no scan ran", got)
	}
}

func TestCheck_FailsClosedWhenTrivyDiesWithFatalOnExitOne(t *testing.T) {
	scanner := &fakeScanner{
		output:   "2026-07-30T10:00:00Z\tFATAL\tinit error: DB error: failed to download vulnerability DB",
		exitCode: 1,
	}

	_, err := Check(context.Background(), Config{RepoRoot: ".", Scanner: scanner})

	if !errors.Is(err, ErrScanFailed) {
		t.Fatalf("err = %v, want ErrScanFailed: a FATAL means no scan happened", err)
	}
	if !strings.Contains(err.Error(), "vulnerability DB") {
		t.Errorf("err = %q, want the FATAL line surfaced so the operator sees the cause", err)
	}
}

func TestCheck_FailsClosedOnAnUnexpectedExitCode(t *testing.T) {
	scanner := &fakeScanner{output: "segfault", exitCode: 2}

	_, err := Check(context.Background(), Config{RepoRoot: ".", Scanner: scanner})

	if !errors.Is(err, ErrScanFailed) {
		t.Fatalf("err = %v, want ErrScanFailed for exit code 2", err)
	}
}

func TestCheck_ReportsFindingsOnExitOneWithoutFatal(t *testing.T) {
	scanner := &fakeScanner{output: "go.sum (gomod)\nTotal: 1 (CRITICAL: 1)", exitCode: 1}

	got, err := Check(context.Background(), Config{RepoRoot: ".", Scanner: scanner})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Pass {
		t.Error("Pass = true, want false when trivy reported vulnerabilities")
	}
	if !strings.Contains(got.Reason, "CRITICAL: 1") {
		t.Errorf("Reason = %q, want the findings table", got.Reason)
	}
}

func TestCheck_PassesOnACleanScan(t *testing.T) {
	scanner := &fakeScanner{exitCode: 0}

	got, err := Check(context.Background(), Config{RepoRoot: ".", Scanner: scanner})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Pass {
		t.Errorf("Pass = false, want true on exit 0: %+v", got)
	}
}

func TestCheck_DefaultsSeveritiesAndRoot(t *testing.T) {
	scanner := &fakeScanner{exitCode: 0}

	if _, err := Check(context.Background(), Config{Scanner: scanner}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if scanner.gotSeverities != "CRITICAL,HIGH" {
		t.Errorf("severities = %q, want CRITICAL,HIGH", scanner.gotSeverities)
	}
	if scanner.gotRoot != "." {
		t.Errorf("root = %q, want \".\"", scanner.gotRoot)
	}
}

func TestExecScanner_ReportsScannerMissingForAnAbsentBinary(t *testing.T) {
	scanner := execScanner{binary: "trivy-binary-that-does-not-exist-anywhere"}

	_, _, err := scanner.Scan(context.Background(), ".", "CRITICAL")

	if !errors.Is(err, ErrScannerMissing) {
		t.Fatalf("err = %v, want ErrScannerMissing", err)
	}
}
