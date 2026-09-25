// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package loopzeroapiopenbao

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// TestCheck_MissingChartFails asserts the sentinel error when chart dir absent.
func TestCheck_MissingChartFails(t *testing.T) {
	root := t.TempDir()
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrChartMissing) {
		t.Fatalf("expected ErrChartMissing, got %v", err)
	}
}

// TestCheck_MissingHelmWarns asserts advisory-only fallthrough when helm absent.
func TestCheck_MissingHelmWarns(t *testing.T) {
	root := t.TempDir()
	// Create the chart dir so we get past ErrChartMissing.
	if err := os.MkdirAll(filepath.Join(root, chartPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, chartPath, "values.yaml"), []byte("stub: true\n"), 0o644); err != nil {
		t.Fatalf("write values: %v", err)
	}
	t.Setenv("PATH", "/nonexistent")
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected advisory fallthrough, got err=%v", err)
	}
	if !outcome.Warn {
		t.Fatalf("expected Warn=true when helm absent, got %+v", outcome)
	}
}

// TestCheck_RealRepoPassesOrAdvisoryFallthrough covers both worlds:
// operator machines with helm see the full render assertion; CI without
// helm sees advisory fallthrough. Both are legitimate.
func TestCheck_RealRepoPassesOrAdvisoryFallthrough(t *testing.T) {
	abs := qtestutil.RootFile(t, chartPath)
	outcome, checkErr := Check(Config{RepoRoot: abs})
	if checkErr != nil {
		// RootFile asserted the chart exists, so every error here — including
		// ErrChartMissing — is a real disagreement, never a reason to pass.
		// When helm renders and fails, the diagnostic must be a real contract violation
		// (a false-PASS regression the gate should catch). Surface it clearly.
		t.Fatalf("real chart failed rendered-envFrom contract. err=%v failures=%v", checkErr, outcome.Failures)
	}
	if outcome.Warn {
		t.Log("helm not installed — advisory-only fallthrough (matches operator-time gate posture)")
	}
}

// TestCheck_HelmAbsentIsAdvisoryWhenChartExists deliberately re-asserts the
// two-arm behavior once more to lock the contract: when helm is not on PATH
// but the chart exists, the check MUST NOT return ErrViolations.
func TestCheck_HelmAbsentIsAdvisoryWhenChartExists(t *testing.T) {
	if _, err := exec.LookPath("helm"); err == nil {
		t.Skip("helm is installed; this test simulates the missing-helm branch and is redundant here")
	}
	abs := qtestutil.RootFile(t, chartPath)
	outcome, checkErr := Check(Config{RepoRoot: abs})
	if checkErr != nil {
		t.Fatalf("chart exists but helm absent — must be advisory fallthrough, got err=%v", checkErr)
	}
	if !outcome.Warn {
		t.Fatalf("expected Warn=true, got %+v", outcome)
	}
}

// renderedContract is the smallest render that satisfies every assertion.
const renderedContract = `# plan 2026-06-10-j22-openbao-transit-eso-sync, see docs/runbooks.md
envFrom:
  - secretRef:
      name: loopzero-service-auth
  - secretRef:
      name: loopzero-openbao-approle
      optional: true
`

// The chart is read by people who clone the repository. A citation of an
// assistant's private memory resolves for nobody, so the gate must not demand one.
func TestCheckRendered_NoAssistantMemoryCitationRequired(t *testing.T) {
	if failures := checkRendered(renderedContract); len(failures) != 0 {
		t.Fatalf("a render without a private-memory citation was refused: %v", failures)
	}
}

func TestCheckRendered_InlineSecretIDIsForbidden(t *testing.T) {
	rendered := renderedContract + "env:\n  - name: OPENBAO_VAULT_SECRET_ID\n    value: s3cr3t\n"
	failures := checkRendered(rendered)
	if len(failures) != 1 {
		t.Fatalf("want exactly the inline-value failure, got %v", failures)
	}
}

func TestCheckRendered_ApproleNotOptionalFails(t *testing.T) {
	rendered := strings.Replace(renderedContract, "optional: true", "optional: false", 1)
	if failures := checkRendered(rendered); len(failures) != 1 {
		t.Fatalf("want exactly the optional=true failure, got %v", failures)
	}
}
