// Hermetic tests for the esochartsanity gate.
// Migrated from bash test infra/tests/check-loopzero-external-secrets-chart-sanity_test.sh
// (M-Migrate2 2026-07-10 M16 T3.5 Wave 2).
package esochartsanity

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/helmrender"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

func TestCheck_ChartMissing_FAIL(t *testing.T) {
	root := t.TempDir()
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrChartMissing) {
		t.Fatalf("expected ErrChartMissing, got %v", err)
	}
}

// Without helm the gate cannot render what it asserts on; that is an error,
// never an advisory pass (quality/helmrender's policy).
func TestCheck_HelmAbsent_IsErrHelmUnavailable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, chartPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("PATH", "/nonexistent")
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, helmrender.ErrHelmUnavailable) {
		t.Fatalf("want helmrender.ErrHelmUnavailable, got %v", err)
	}
}

func TestCheck_RealRepoWithHelm(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not on PATH — skipping LIVE render")
	}
	root := qtestutil.Root(t)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("real-repo violation: %v; %v", err, outcome.Failures)
	}
}
