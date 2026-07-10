// Hermetic tests for the day30day60cronjobs gate.
//
// Uses "advisory-fallthrough when helm absent" pattern from loopzeroapiopenbao —
// tests that don't need helm run without it; tests that need it verify the
// binary is present and skip otherwise.
//
// Migrated from bash test infra/tests/check-day30-day60-cronjobs_test.sh
// (M-Migrate2 2026-07-10 M16 T3.4 Wave 2).
package day30day60cronjobs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheck_ChartMissing_FAIL(t *testing.T) {
	root := t.TempDir()
	// No infra/helm/loopzero-api dir.
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrChartMissing) {
		t.Fatalf("expected ErrChartMissing, got %v", err)
	}
}

func TestCheck_HelmAbsent_WARN(t *testing.T) {
	root := t.TempDir()
	// Stub chart directory presence (empty is fine — helm is absent below).
	if err := os.MkdirAll(filepath.Join(root, chartPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Sanitize PATH so exec.LookPath("helm") fails.
	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)
	os.Setenv("PATH", "/nonexistent")

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected nil (WARN advisory), got %v", err)
	}
	if !outcome.Warn {
		t.Fatalf("expected Warn=true when helm absent, got Warn=%v", outcome.Warn)
	}
}

func TestCheck_RealRepoWithHelm(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not on PATH — skipping LIVE chart render")
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
		t.Skip("could not locate ROADMAP.md (skipping)")
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("real repo violation: %v; failures=%v", err, outcome.Failures)
	}
	if outcome.Warn {
		t.Log("helm found but chart-render advisory triggered — investigate")
	}
}
