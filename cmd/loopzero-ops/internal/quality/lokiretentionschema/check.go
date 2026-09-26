// Package lokiretentionschema is the trio-compatibility regression test for
// the T8.2 Loki retention_stream schema — validates that
// infra/helm/loki/values.yaml is structurally valid YAML AND that the
// retention_stream block conforms to Loki SingleBinary schema:
//
//   - deploymentMode: SingleBinary (cleanroom compatibility)
//   - loki.limits_config.retention_period: 168h (7 days for non-vault NS)
//   - loki.limits_config.retention_stream: array with exactly 1 entry
//   - retention_stream[0].selector matches '{namespace="secrets-system"}'
//   - retention_stream[0].period: 2160h (90 days forensic)
//   - retention_stream[0].priority: int (Loki schema requirement)
//
// Migrated from infra/tests/check-loki-retention-schema_test.sh
// (M-Migrate2 2026-07-10 M16 T3.2 Wave 2). Shape 2 (YAML structural
// inspection). Uses gopkg.in/yaml.v3 (already vendored) — plan D2 permits
// this via rung 4 (already-installed dependency) of parsimony ladder.
package lokiretentionschema

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qgshell"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
	"gopkg.in/yaml.v3"
)

const lokiValuesRel = "infra/helm/loki/values.yaml"

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
var ErrViolations = errors.New("loki-retention-schema hermetic contract violated")

// retentionStreamEntry is one element of loki.limits_config.retention_stream.
// Priority is decoded as `any` (not *int) so a string value doesn't cause a
// hard yaml.Unmarshal error; the check inspects the underlying type instead.
type retentionStreamEntry struct {
	Selector string `yaml:"selector"`
	Period   string `yaml:"period"`
	Priority any    `yaml:"priority"`
}

// lokiValues represents the subset of the values.yaml we need to inspect.
type lokiValues struct {
	DeploymentMode string `yaml:"deploymentMode"`
	Loki           struct {
		LimitsConfig struct {
			RetentionPeriod string                 `yaml:"retention_period"`
			RetentionStream []retentionStreamEntry `yaml:"retention_stream"`
		} `yaml:"limits_config"`
	} `yaml:"loki"`
}

// Check validates YAML structure + retention_stream schema conformance.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	lokiPath := filepath.Join(root, lokiValuesRel)

	var failures []string

	content, err := qgshell.StatAndRead(lokiPath)
	if err != nil {
		failures = append(failures, fmt.Sprintf("loki values.yaml MISSING: %s", lokiValuesRel))
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}

	// Assertion 1: YAML parses cleanly.
	var vals lokiValues
	if err := yaml.Unmarshal(content, &vals); err != nil {
		failures = append(failures, fmt.Sprintf("YAML parse error: %v", err))
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}

	// Assertion 2: exactly 1 retention_stream entry.
	streams := vals.Loki.LimitsConfig.RetentionStream
	if len(streams) != 1 {
		failures = append(failures, fmt.Sprintf("Wrong retention_stream count: got %d, want 1", len(streams)))
	} else {
		s := streams[0]
		// Assertion 3: selector matches secrets-system NS.
		if s.Selector != `{namespace="secrets-system"}` {
			failures = append(failures, fmt.Sprintf("Selector does NOT match secrets-system NS: got %q", s.Selector))
		}
		// Assertion 4: period: 2160h.
		if s.Period != "2160h" {
			failures = append(failures, fmt.Sprintf("period NOT 2160h: got %q", s.Period))
		}
		// Assertion 6: priority is int — Loki schema rejects non-int on startup.
		if _, ok := s.Priority.(int); !ok {
			failures = append(failures, "retention_stream[0].priority is NOT int — Loki rejects on startup")
		}
	}

	// Assertion 5: default retention_period: 168h.
	if vals.Loki.LimitsConfig.RetentionPeriod != "168h" {
		failures = append(failures, fmt.Sprintf("Default retention_period drifted: got %q, want 168h", vals.Loki.LimitsConfig.RetentionPeriod))
	}

	// Assertion 7: deploymentMode: SingleBinary.
	if vals.DeploymentMode != "SingleBinary" {
		failures = append(failures, fmt.Sprintf("deploymentMode changed to %q — cleanroom may break", vals.DeploymentMode))
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}
