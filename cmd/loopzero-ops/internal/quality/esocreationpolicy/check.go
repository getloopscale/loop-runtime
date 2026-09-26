// Package esocreationpolicy is the iter 20 P0 regression test for the
// infra/helm/loopzero-external-secrets chart (plan 2026-06-02-loopzero-api-vault-
// integration v1.8).
//
// Asserts ExternalSecret CRs render with creationPolicy: Orphan (NOT Owner).
// LIVE-discovered: creationPolicy: Owner + ownerReference + blockOwnerDeletion
// causes K8s GC to cascade-delete K8s Secrets when the ExternalSecret CR is
// deleted (e.g., helmfile sync vaultEnabled=false rollback). Since GC runs
// before ESO controller reconcile, deletionPolicy=Retain has no effect and
// rollback becomes destructive. Fix: creationPolicy: Orphan decouples
// lifecycle; K8s Secrets are preserved on chart uninstall.
//
// 4 assertions:
//
//  1. ≥ 1 ExternalSecret CR rendered.
//  2. No creationPolicy: Owner (regression to Owner is P0 destructive).
//  3. deletionPolicy: Retain count ≥ Orphan count (defense-in-depth).
//  4. Orphan count == ExternalSecret CR count (all CRs carry the policy).
//
// Advisory-fallthrough when helm binary is absent.
//
// Migrated from infra/tests/check-loopzero-external-secrets-creation-policy_test.sh
// (M-Migrate2 2026-07-10 M16 T3.6 Wave 2).
package esocreationpolicy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmrender"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

const chartPath = "infra/helm/loopzero-external-secrets"

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	Failures []string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// ErrViolations signals ≥ 1 assertion failed.
var ErrViolations = errors.New("loopzero-external-secrets creation-policy contract violated")

// ErrChartMissing signals the chart is absent.
var ErrChartMissing = errors.New("loopzero-external-secrets chart missing")

// Check runs the 4 assertions.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	chart := filepath.Join(root, chartPath)
	if _, err := os.Stat(chart); errors.Is(err, fs.ErrNotExist) {
		return Outcome{}, ErrChartMissing
	}
	out, err := helmrender.Helm{Release: "loopzero-external-secrets"}.Render(chart, []string{"envName=test"})
	if errors.Is(err, helmrender.ErrHelmUnavailable) {
		return Outcome{}, err
	}
	if err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("helm template failed: %v", err)}, Violation: ErrViolations}, nil
	}
	rendered := []byte(out)

	var failures []string

	// (1) ≥ 1 ExternalSecret CR rendered.
	esCount := countMatches(rendered, `(?m)^kind: ExternalSecret`)
	if esCount < 1 {
		failures = append(failures, "no ExternalSecret CRs rendered")
	}

	// (2) No creationPolicy: Owner regression.
	ownerCount := countMatches(rendered, `creationPolicy: Owner`)
	orphanCount := countMatches(rendered, `creationPolicy: Orphan`)
	if ownerCount != 0 || orphanCount < 1 {
		failures = append(failures, fmt.Sprintf("P0 REGRESSION: creationPolicy=Owner=%d Orphan=%d (Owner=0 required + Orphan≥1)", ownerCount, orphanCount))
	}

	// (3) deletionPolicy: Retain ≥ Orphan (defense-in-depth).
	if orphanCount >= 1 {
		retainCount := countMatches(rendered, `deletionPolicy: Retain`)
		if retainCount < orphanCount {
			failures = append(failures, fmt.Sprintf("deletionPolicy Retain=%d < Orphan=%d (defense-in-depth gap)", retainCount, orphanCount))
		}
	}

	// (4) Orphan count == ExternalSecret CR count.
	if orphanCount != esCount {
		failures = append(failures, fmt.Sprintf("Orphan policy count %d != ExternalSecret CR count %d (some CRs missing policy)", orphanCount, esCount))
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

func countMatches(content []byte, pattern string) int {
	return len(regexp.MustCompile(pattern).FindAll(content, -1))
}
