// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// The Python security scan is a build gate: generate-dockerfile depends on it
// so a vulnerable dependency stops the build. These tests execute the emitted
// script under `sh` with stub tools on PATH, so the gate's exit status is
// measured rather than read from the text.

// stubBin writes an executable shell script named name into dir.
func stubBin(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

// runPythonScan runs the scan over a workspace holding one app with a
// requirements.txt, with only the stub bin directory and the system shell
// tools on PATH. It returns the exit code and the combined output.
func runPythonScan(t *testing.T, bin string) (int, string) {
	t.Helper()
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "api"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "api", "requirements.txt"), []byte("flask==0.12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Wrapped exactly as buildContainer wraps it, so the shell sees what the
	// build container runs.
	script := wrapWithLoopZeroMarkers("security-scan",
		pythonScanScript(workspace, []deploycontracts.AppSpec{{Name: "api", Path: "api"}}))
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + t.TempDir()}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var ee *exec.ExitError
	if asExitError(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("scan did not run: %v\n%s", err, out)
	return 0, ""
}

func TestPythonScanFailsTheBuildWhenPipAuditFindsVulnerabilities(t *testing.T) {
	// Arrange: pip-audit reports a vulnerability (exit 1), as it does on a finding.
	bin := t.TempDir()
	stubBin(t, bin, "pip-audit", `echo "flask 0.12 PYSEC-2018-66"; exit 1`)

	// Act
	rc, out := runPythonScan(t, bin)

	// Assert
	if rc == 0 {
		t.Fatalf("a vulnerable dependency passed the security gate:\n%s", out)
	}
}

func TestPythonScanFailsTheBuildWhenTheScannerCannotBeInstalled(t *testing.T) {
	// Arrange: no pip-audit, and creating its virtualenv fails.
	bin := t.TempDir()
	stubBin(t, bin, "python3", `exit 1`)
	stubBin(t, bin, "python", `exit 1`)

	// Act
	rc, out := runPythonScan(t, bin)

	// Assert: an unavailable scanner is not a clean scan.
	if rc == 0 {
		t.Fatalf("the security gate passed although no scanner ran:\n%s", out)
	}
}

func TestPythonScanAuditsTheAppRequirementsAndPassesWhenClean(t *testing.T) {
	// Arrange: pip-audit records its arguments and finds nothing.
	bin := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	stubBin(t, bin, "pip-audit", `echo "$@" >> `+argsFile+`; exit 0`)

	// Act
	rc, out := runPythonScan(t, bin)

	// Assert
	if rc != 0 {
		t.Fatalf("clean scan failed with %d:\n%s", rc, out)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("pip-audit was never invoked: %v\n%s", err, out)
	}
	if !strings.Contains(string(got), "-r") || !strings.Contains(string(got), filepath.Join("api", "requirements.txt")) {
		t.Errorf("pip-audit audited %q, not the app's requirements.txt", strings.TrimSpace(string(got)))
	}
}
