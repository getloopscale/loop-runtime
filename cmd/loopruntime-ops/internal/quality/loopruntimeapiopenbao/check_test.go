// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package loopruntimeapiopenbao

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

// TestCheck_MissingChartFails asserts the sentinel error when chart dir absent.
func TestCheck_MissingChartFails(t *testing.T) {
	root := t.TempDir()
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrChartMissing) {
		t.Fatalf("expected ErrChartMissing, got %v", err)
	}
}

// A chart without the bootstrap values has no install to check: the contract
// lives in those values, so their absence is the chart missing its install.
func TestCheck_MissingBootstrapValuesFails(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, chartPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrChartMissing) {
		t.Fatalf("expected ErrChartMissing for a chart without %s, got %v", valuesPath, err)
	}
}

// TestCheck_MissingHelmWarns asserts advisory-only fallthrough when helm absent.
func TestCheck_MissingHelmWarns(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, chartPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, valuesPath), []byte("stub: true\n"), 0o644); err != nil {
		t.Fatalf("write values: %v", err)
	}
	t.Setenv("PATH", "/nonexistent")
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
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
	outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
	if checkErr != nil {
		// RootFile asserted the chart exists, so every error here — including
		// ErrChartMissing — is a real disagreement, never a reason to pass.
		t.Fatalf("real chart failed the envFrom contract. err=%v failures=%v", checkErr, outcome.Failures)
	}
	if outcome.Warn {
		t.Log("helm not installed — advisory-only fallthrough (matches operator-time gate posture)")
	}
}

// TestCheck_HelmAbsentIsAdvisoryWhenChartExists re-asserts the two-arm
// behaviour: when helm is not on PATH but the chart exists, the check MUST NOT
// return ErrViolations.
func TestCheck_HelmAbsentIsAdvisoryWhenChartExists(t *testing.T) {
	if _, err := exec.LookPath("helm"); err == nil {
		t.Skip("helm is installed; this test simulates the missing-helm branch and is redundant here")
	}
	abs := qtestutil.RootFile(t, chartPath)
	outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
	if checkErr != nil {
		t.Fatalf("chart exists but helm absent — must be advisory fallthrough, got err=%v", checkErr)
	}
	if !outcome.Warn {
		t.Fatalf("expected Warn=true, got %+v", outcome)
	}
}

// renderedContract is the smallest render that satisfies every render assertion.
const renderedContract = `envFrom:
  - secretRef:
      name: loopruntime-service-auth
  - secretRef:
      name: loopruntime-openbao-approle
      optional: true
`

// sourceContract is the smallest values source that satisfies the traceability
// assertions: the plan and the runbook a reader follows.
const sourceContract = `# plan 2026-06-10-j22-openbao-transit-eso-sync, see docs/runbooks.md
secrets:
  additionalSources: []
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

// Helm does not render values comments, so the traceability the old template
// carried in its comments is checked on the values source that now declares
// the mount.
func TestCheckSource_CitesPlanAndRunbook(t *testing.T) {
	if failures := checkSource(sourceContract); len(failures) != 0 {
		t.Fatalf("a source citing the plan and the runbook was refused: %v", failures)
	}
	if failures := checkSource("secrets: {}\n"); len(failures) != 2 {
		t.Fatalf("a source citing neither must fail both assertions, got %v", failures)
	}
}
