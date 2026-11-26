// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package orykratoslifespan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "infra", "helm", "ory-kratos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "values.yaml.gotmpl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

const canonical = `# Ory Kratos values — plan kratos-self-service-rate-limit-systemic v1.1
selfservice:
  flows:
    login:
      # T2.2 plan kratos-self-service-rate-limit-systemic — defense pair with edge rate_limit
      lifespan: 10m
      ui_url: "{{ .Values.publicUIURL }}/login"
    registration:
      # T2.2 plan kratos-self-service-rate-limit-systemic — tight window
      lifespan: 10m
      ui_url: "{{ .Values.publicUIURL }}/register"
    recovery:
      # T2.2 plan kratos-self-service-rate-limit-systemic — email-bomb defense
      lifespan: 15m
      ui_url: "{{ .Values.publicUIURL }}/recovery"
`

func TestCheck_CanonicalPasses(t *testing.T) {
	root := writeFixture(t, canonical)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS, got err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_MissingFileFails(t *testing.T) {
	root := t.TempDir()
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
}

func TestCheck_WrongLoginLifespanFails(t *testing.T) {
	body := strings.Replace(canonical, "lifespan: 10m\n      ui_url: \"{{ .Values.publicUIURL }}/login\"", "lifespan: 1h\n      ui_url: \"{{ .Values.publicUIURL }}/login\"", 1)
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when login lifespan wrong, got %v", err)
	}
}

func TestCheck_MissingRecoveryLifespanFails(t *testing.T) {
	body := strings.Replace(canonical, "    recovery:\n      # T2.2 plan kratos-self-service-rate-limit-systemic — email-bomb defense\n      lifespan: 15m\n      ui_url: \"{{ .Values.publicUIURL }}/recovery\"\n", "", 1)
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when recovery section missing, got %v", err)
	}
}

func TestCheck_MissingPlanSlugCitationFails(t *testing.T) {
	// Strip all plan slug references (drops attribution — the traceability contract).
	body := strings.ReplaceAll(canonical, "kratos-self-service-rate-limit-systemic", "SOMETHING-ELSE")
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when plan slug not cited, got %v", err)
	}
}

func TestCheck_FileTooLongFails(t *testing.T) {
	body := canonical
	for i := 0; i < maxLoC+10; i++ {
		body += "# padding line\n"
	}
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when file exceeds LoC budget, got %v", err)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	abs := qtestutil.RootFile(t, valuesPath)
	outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
	if checkErr != nil {
		t.Fatalf("real values.yaml.gotmpl must satisfy invariant. err=%v failures=%v", checkErr, outcome.Failures)
	}
}
