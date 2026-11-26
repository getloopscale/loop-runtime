// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package webhookcanonicalpath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCheck_CleanRepo_PASS(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "infra/tests/e2e/example_test.sh"), "curl /api/v1/webhooks/github\n")
	if _, err := violationOf(Check(Config{RepoRoot: root})); err != nil {
		t.Fatalf("expected clean repo to PASS, got %v", err)
	}
}

func TestCheck_ForbiddenPattern_FAIL(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "infra/tests/e2e/bad_test.sh"), "curl /engine/api/v1/webhooks/github\n")
	out, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if len(out.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(out.Violations), out.Violations)
	}
}

func TestCheck_AllowlistedComment_PASS(t *testing.T) {
	root := t.TempDir()
	// Comment referencing the pattern with a marker word should be allowed.
	writeFile(t, filepath.Join(root, "infra/tests/e2e/audit_test.sh"),
		"# guard: legacy /engine/api/v1/webhooks/github must never appear in code\n")
	if _, err := violationOf(Check(Config{RepoRoot: root})); err != nil {
		t.Fatalf("expected allowlisted comment to PASS, got %v", err)
	}
}

// One scan dir may be removed by a strangler wave; both gone means the gate read
// nothing, and a scan over nothing is not a pass.
func TestCheck_AllDirsMissing_IsNothingScanned(t *testing.T) {
	root := t.TempDir()
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("expected ErrNothingScanned when no scan dir exists, got %v", err)
	}
}

func TestCheck_OneDirMissing_StillScansTheOther(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "infra/scripts/local/b.sh"), "curl /engine/api/v1/webhooks/github\n")
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrViolations) {
		t.Fatalf("expected the remaining dir to be scanned, got %v", err)
	}
}

func TestCheck_TwoDirs_TwoViolations(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "infra/tests/e2e/a_test.sh"), "curl /engine/api/v1/webhooks/github\n")
	writeFile(t, filepath.Join(root, "infra/scripts/local/b.sh"), "curl /engine/api/v1/webhooks/github\n")
	out, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if len(out.Violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %v", len(out.Violations), out.Violations)
	}
}
