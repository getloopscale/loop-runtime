package oidccredadminloopzero

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheck_Clean_PASS(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "infra/tests/e2e/a.sh"), "curl -u admin@loopzero.example:loopzero-dev-password\n")
	if _, err := Check(Config{RepoRoot: root}); err != nil {
		t.Fatalf("expected clean, got %v", err)
	}
}

func TestCheck_ForbiddenEmail_FAIL(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "infra/tests/e2e/bad.sh"), "curl -u paulo@loopzero.test\n")
	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
}

func TestCheck_ForbiddenPassword_FAIL(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "infra/scripts/local/seed.sh"), "PASSWORD=LoopZeroDev2026!\n")
	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
}

func TestCheck_MissingDirs_PASS(t *testing.T) {
	if _, err := Check(Config{RepoRoot: t.TempDir()}); err != nil {
		t.Fatalf("expected missing dirs to PASS, got %v", err)
	}
}
