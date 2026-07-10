// Hermetic tests for the archivescriptrepo gate.
//
// Migrated from bash test infra/tests/check-archive-script-repo-root_test.sh
// (M-Migrate2 2026-07-10 M16 T2.2 Wave 1). The bash test has a single
// assertion; the Go migration expands to 5 scenarios covering happy path,
// forbidden pattern detection, canonical 4-dirs-up allow, missing dir,
// non-shell file skip.
package archivescriptrepo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdirall %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writeexec %s: %v", path, err)
	}
}

const canonicalScript = `#!/usr/bin/env bash
# canonical 4-dirs-up post-quarantine form
REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
echo "$REPO_ROOT"
`

const forbiddenScript = `#!/usr/bin/env bash
# WRONG: 3-dirs-up pre-quarantine form — silently targets parent of repo
REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
echo "$REPO_ROOT"
`

const mixedScript = `#!/usr/bin/env bash
# Some scripts have both forms; ONLY the 3-dirs-up form is forbidden.
CORRECT="$(cd "$(dirname "$0")/../../../.." && pwd)"
WRONG="$(cd "$(dirname "$0")/../../.." && pwd)"
`

func TestCheck_CleanRepo_PASS(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, scanRoot, "seed-good.sh"), canonicalScript)

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected nil err, got %v; failures=%v", err, outcome.Failures)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures, got %v", outcome.Failures)
	}
}

func TestCheck_ForbiddenPattern_FAIL(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, scanRoot, "seed-bad.sh"), forbiddenScript)

	outcome, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if len(outcome.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %v", outcome.Failures)
	}
	if !containsSubstring(outcome.Failures[0], "seed-bad.sh") {
		t.Fatalf("expected 'seed-bad.sh' in failure, got %q", outcome.Failures[0])
	}
}

func TestCheck_MixedFile_FailsOnceOnly(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, scanRoot, "mixed.sh"), mixedScript)

	outcome, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	// Exactly one line has the forbidden pattern; expect exactly one failure.
	if len(outcome.Failures) != 1 {
		t.Fatalf("expected 1 failure (only the WRONG line), got %v", outcome.Failures)
	}
}

func TestCheck_MissingDir_PASS(t *testing.T) {
	root := t.TempDir()
	// scanRoot dir not created — should treat as no-content.
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected nil err for missing scan dir, got %v", err)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures for missing scan dir, got %v", outcome.Failures)
	}
}

func TestCheck_NonShellFileSkipped(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, scanRoot, "README.md"), forbiddenScript)

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected nil err (.md skipped), got %v", err)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures for .md file skip, got %v", outcome.Failures)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := cwd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(root, "ROADMAP.md")); err == nil {
			break
		}
		root = filepath.Dir(root)
	}
	if _, err := os.Stat(filepath.Join(root, "ROADMAP.md")); err != nil {
		t.Skipf("could not locate ROADMAP.md from %s (skipping)", cwd)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("real repo violation: %v; failures=%v", err, outcome.Failures)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures against real repo, got %v", outcome.Failures)
	}
}

func containsSubstring(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
