// Hermetic tests for the lokiretentionschema gate.
//
// Migrated from bash test infra/tests/check-loki-retention-schema_test.sh
// (M-Migrate2 2026-07-10 M16 T3.2 Wave 2).
package lokiretentionschema

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writefile: %v", err)
	}
}

const canonical = `deploymentMode: SingleBinary
loki:
  limits_config:
    retention_period: 168h
    retention_stream:
      - selector: '{namespace="secrets-system"}'
        period: 2160h
        priority: 10
`

func TestCheck_HappyPath_PASS(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, lokiValuesRel), canonical)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected nil, got %v; %v", err, outcome.Failures)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures, got %v", outcome.Failures)
	}
}

func TestCheck_MissingFile_FAIL(t *testing.T) {
	root := t.TempDir()
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if len(outcome.Failures) < 1 {
		t.Fatalf("expected ≥ 1 failure, got %v", outcome.Failures)
	}
}

func TestCheck_ParseError_FAIL(t *testing.T) {
	root := t.TempDir()
	// Unterminated bracket → real YAML parse error.
	writeFile(t, filepath.Join(root, lokiValuesRel), "loki: [unclosed\n")
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for parse error, got %v", err)
	}
	if !hasSubstring(outcome.Failures, "YAML parse error") {
		t.Fatalf("expected YAML parse failure, got %v", outcome.Failures)
	}
}

func TestCheck_WrongDeploymentMode_FAIL(t *testing.T) {
	root := t.TempDir()
	broken := `deploymentMode: Distributed
loki:
  limits_config:
    retention_period: 168h
    retention_stream:
      - selector: '{namespace="secrets-system"}'
        period: 2160h
        priority: 10
`
	writeFile(t, filepath.Join(root, lokiValuesRel), broken)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !hasSubstring(outcome.Failures, "deploymentMode changed to") {
		t.Fatalf("expected deploymentMode failure, got %v", outcome.Failures)
	}
}

func TestCheck_PriorityNotInt_FAIL(t *testing.T) {
	root := t.TempDir()
	broken := `deploymentMode: SingleBinary
loki:
  limits_config:
    retention_period: 168h
    retention_stream:
      - selector: '{namespace="secrets-system"}'
        period: 2160h
        priority: "high"
`
	writeFile(t, filepath.Join(root, lokiValuesRel), broken)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !hasSubstring(outcome.Failures, "priority is NOT int") {
		t.Fatalf("expected priority-not-int failure, got %v", outcome.Failures)
	}
}

func TestCheck_WrongPeriod_FAIL(t *testing.T) {
	root := t.TempDir()
	broken := `deploymentMode: SingleBinary
loki:
  limits_config:
    retention_period: 168h
    retention_stream:
      - selector: '{namespace="secrets-system"}'
        period: 720h
        priority: 10
`
	writeFile(t, filepath.Join(root, lokiValuesRel), broken)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !hasSubstring(outcome.Failures, "period NOT 2160h") {
		t.Fatalf("expected period failure, got %v", outcome.Failures)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	root := qtestutil.Root(t)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("real repo violation: %v; %v", err, outcome.Failures)
	}
}

func hasSubstring(failures []string, substr string) bool {
	for _, f := range failures {
		for i := 0; i+len(substr) <= len(f); i++ {
			if f[i:i+len(substr)] == substr {
				return true
			}
		}
	}
	return false
}
