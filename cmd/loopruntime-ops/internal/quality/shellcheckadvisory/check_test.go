// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package shellcheckadvisory

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

func writeScript(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCheck_EmptyRepoIsNothingScanned(t *testing.T) {
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck not installed; advisory fallthrough tested separately")
	}
	if _, err := violationOf(Check(Config{RepoRoot: t.TempDir()})); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("an empty tree is a scan that read nothing; err=%v", err)
	}
}

func TestCheck_CleanScriptPasses(t *testing.T) {
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck not installed")
	}
	root := t.TempDir()
	writeScript(t, root, "infra/scripts/local/hello.sh", "#!/usr/bin/env bash\nset -euo pipefail\necho \"hello\"\n")
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS for clean fixture, got err=%v diagnostic=%q", err, outcome.Diagnostic)
	}
	if outcome.ScannedFiles != 1 {
		t.Fatalf("expected 1 file scanned, got %d", outcome.ScannedFiles)
	}
}

func TestCheck_ViolationInFixtureFails(t *testing.T) {
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck not installed")
	}
	root := t.TempDir()
	// Real shellcheck violation not in the exclude list: SC2148 (missing shebang).
	// The script MUST match one of the canonical globs so it gets picked up.
	writeScript(t, root, "infra/scripts/local/bad.sh", "echo hi\n")
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for SC2148 (missing shebang), got %v", err)
	}
}

func TestCheck_MissingShellcheckWarns(t *testing.T) {
	// Simulate "shellcheck not on PATH" by resetting PATH to /nonexistent.
	// t.Setenv restores automatically at test end.
	t.Setenv("PATH", "/nonexistent")
	root := t.TempDir()
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected fallthrough WARN, got err=%v", err)
	}
	if !outcome.Warn {
		t.Fatalf("expected Warn=true when shellcheck absent, got %+v", outcome)
	}
}

func TestCheck_ExcludeCodesTolerated(t *testing.T) {
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck not installed")
	}
	root := t.TempDir()
	// SC2086 (double-quote to prevent globbing) IS in the exclude list — must PASS.
	writeScript(t, root, "infra/scripts/local/tolerated.sh", "#!/usr/bin/env bash\nset -euo pipefail\nARG=$1\necho $ARG\n")
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("SC2086 should be excluded, got err=%v diagnostic=%q", err, outcome.Diagnostic)
	}
}

func TestCheck_RealRepoPassesOrIsAdvisoryFallthrough(t *testing.T) {
	abs := qtestutil.RootFile(t, filepath.Join("infra", "scripts", "local"))
	outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
	if checkErr != nil {
		t.Fatalf("real repo shellcheck must be clean OR advisory-only. err=%v diagnostic=%q", checkErr, outcome.Diagnostic)
	}
}
