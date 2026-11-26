// Package lokivaultretention enforces T8.5 of plan
// 2026-06-02-openbao-adoption v1.4 — the Loki vault-audit archive CronJob.
//
// T8.2 (the per-namespace retention rule: 168h default, 2160h for
// secrets-system) is owned by lokiretentionschema alone, which decodes the
// values structurally; this gate used to re-assert the same four facts by
// regex, so changing the policy meant editing two gates in lockstep.
//
// T8.5 — Archive CronJob:
//
//	infra/helm/monitoring/manifests/cronjob-loki-vault-audit-archive.yaml
//	renders correctly with: kind: CronJob, schedule="0 4 1 * *",
//	concurrencyPolicy: Forbid, security hardening, DO Spaces env vars,
//	logcli + mc invocation pattern targeting secrets-system NS.
//
// Pure file-content regex — no helm-template shell-out; the manifest is
// committed in the repo tree.
//
// Migrated from infra/tests/check-loki-vault-retention_test.sh
// (M-Migrate2 2026-07-10 M16 T3.1 Wave 2). Shape 2 (text-content assertion)
// per blueprint Q7 canonical mapping.
package lokivaultretention

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qgshell"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

const archiveCronRel = "infra/helm/monitoring/manifests/cronjob-loki-vault-audit-archive.yaml"

// assertion is one regex expectation against a file's content.
type assertion struct {
	label string
	re    *regexp.Regexp
}

// t85Assertions: Archive CronJob YAML expectations.
var t85Assertions = []assertion{
	{`CronJob kind declared`, regexp.MustCompile(`kind: CronJob`)},
	{`schedule = 04:00 UTC day 1 monthly`, regexp.MustCompile(`schedule: "0 4 1 \* \*"`)},
	{`concurrencyPolicy: Forbid (no parallel runs)`, regexp.MustCompile(`concurrencyPolicy: Forbid`)},
	{`runAsNonRoot hardening present`, regexp.MustCompile(`runAsNonRoot: true`)},
	{`capabilities drop: ALL`, regexp.MustCompile(`drop: \["ALL"\]`)},
	{`logcli query invocation present`, regexp.MustCompile(`logcli query`)},
	// mc cp requires TWO patterns (compound AND) — evaluated in-line.
	// AWS creds env vars require TWO patterns (compound AND) — evaluated in-line.
	// Loki-archive-s3-creds Secret reference.
	{`S3 creds Secret referenced (loki-archive-s3-creds)`, regexp.MustCompile(`(?m)^\s+name: loki-archive-s3-creds`)},
	{`logcli query targets secrets-system NS`, regexp.MustCompile(`namespace=secrets-system`)},
}

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the aggregated result (canonical M-Migrate2 shape per blueprint ADR D1).
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
var ErrViolations = errors.New("loki-vault-retention hermetic contract violated")

// Check runs the T8.5 assertions and returns (Outcome, error).
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	var failures []string

	// T8.5: read archive CronJob YAML
	cronPath := filepath.Join(root, archiveCronRel)
	cronBytes, cronErr := qgshell.StatAndRead(cronPath)
	if cronErr != nil {
		failures = append(failures, fmt.Sprintf("cronjob-loki-vault-audit-archive.yaml MISSING: %s", archiveCronRel))
	} else {
		for _, a := range t85Assertions {
			if !a.re.Match(cronBytes) {
				failures = append(failures, fmt.Sprintf("T8.5: %s", a.label))
			}
		}
		// Compound assertions requiring 2 patterns (AND semantics).
		if !regexp.MustCompile(`mc cp`).Match(cronBytes) || !regexp.MustCompile(`loopzero-audit-logs-`).Match(cronBytes) {
			failures = append(failures, "T8.5: mc cp to DO Spaces bucket")
		}
		if !regexp.MustCompile(`name: AWS_ACCESS_KEY_ID`).Match(cronBytes) || !regexp.MustCompile(`name: AWS_SECRET_ACCESS_KEY`).Match(cronBytes) {
			failures = append(failures, "T8.5: S3 creds env vars wired via secretKeyRef")
		}
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}
