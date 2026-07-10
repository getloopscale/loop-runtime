// Package lokivaultretention enforces T8.2 + T8.5 of plan
// 2026-06-02-openbao-adoption v1.4.
//
// T8.2 — Loki per-namespace retention:
//
//	infra/helm/loki/values.yaml has retention_stream with
//	selector='{namespace="vault-system"}' period=2160h (90 days)
//	AND default retention_period remains 168h (7 days for other NS).
//
// T8.5 — Archive CronJob:
//
//	infra/helm/monitoring/manifests/cronjob-loki-vault-audit-archive.yaml
//	renders correctly with: kind: CronJob, schedule="0 4 1 * *",
//	concurrencyPolicy: Forbid, security hardening, DO Spaces env vars,
//	logcli + mc invocation pattern targeting vault-system NS.
//
// Total 14 assertions (4 T8.2 + 10 T8.5). Pure file-content regex — no
// helm-template shell-out; both files already committed in the repo tree.
//
// Migrated from infra/tests/check-loki-vault-retention_test.sh
// (M-Migrate2 2026-07-10 M16 T3.1 Wave 2). Shape 2 (text-content assertion)
// per blueprint Q7 canonical mapping.
package lokivaultretention

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

const (
	lokiValuesRel  = "infra/helm/loki/values.yaml"
	archiveCronRel = "infra/helm/monitoring/manifests/cronjob-loki-vault-audit-archive.yaml"
)

// assertion is one regex expectation against a file's content.
type assertion struct {
	label string
	re    *regexp.Regexp
}

// t82Assertions: Loki values.yaml expectations.
var t82Assertions = []assertion{
	{`default retention_period: 168h preserved`, regexp.MustCompile(`retention_period: 168h`)},
	{`retention_stream array declared`, regexp.MustCompile(`retention_stream:`)},
	{`selector matches vault-system NS`, regexp.MustCompile(`namespace=\\?"vault-system\\?"`)},
	{`period: 2160h (90 days) configured`, regexp.MustCompile(`period: 2160h`)},
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
	{`logcli query targets vault-system NS`, regexp.MustCompile(`namespace=vault-system`)},
}

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the aggregated result (canonical M-Migrate2 shape per blueprint ADR D1).
type Outcome struct {
	Failures []string
}

// ErrViolations signals ≥ 1 assertion failed.
var ErrViolations = errors.New("loki-vault-retention hermetic contract violated")

// Check runs the 14 assertions and returns (Outcome, error).
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	var failures []string

	// T8.2: read Loki values.yaml
	lokiPath := filepath.Join(root, lokiValuesRel)
	lokiBytes, lokiErr := readFile(lokiPath)
	if lokiErr != nil {
		failures = append(failures, fmt.Sprintf("loki values.yaml MISSING: %s", lokiValuesRel))
	} else {
		for _, a := range t82Assertions {
			if !a.re.Match(lokiBytes) {
				failures = append(failures, fmt.Sprintf("T8.2: %s", a.label))
			}
		}
	}

	// T8.5: read archive CronJob YAML
	cronPath := filepath.Join(root, archiveCronRel)
	cronBytes, cronErr := readFile(cronPath)
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
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}

// readFile returns the file bytes or an error (including fs.ErrNotExist for missing).
func readFile(path string) ([]byte, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, err
	} else if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	return os.ReadFile(path)
}
