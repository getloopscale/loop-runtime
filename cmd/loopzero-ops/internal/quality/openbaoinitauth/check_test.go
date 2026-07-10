// Hermetic tests for the openbaoinitauth gate.
// Migrated from bash test infra/tests/check-openbao-init-auth-mounts_test.sh
// (M-Migrate2 2026-07-10 M16 T3.7 Wave 2).
package openbaoinitauth

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheck_ChartMissing_FAIL(t *testing.T) {
	root := t.TempDir()
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrChartMissing) {
		t.Fatalf("expected ErrChartMissing, got %v", err)
	}
}

func TestCheck_HelmAbsent_WARN(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, chartPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)
	os.Setenv("PATH", "/nonexistent")
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected nil (WARN), got %v", err)
	}
	if !outcome.Warn {
		t.Fatalf("expected Warn=true")
	}
}

func TestCheck_RealRepoWithHelm(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not on PATH — skipping")
	}
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
		t.Skip("could not locate ROADMAP.md")
	}
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("real-repo violation: %v; %v", err, outcome.Failures)
	}
}
