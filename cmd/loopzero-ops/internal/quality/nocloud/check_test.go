// Hermetic tests for the nocloud gate.
//
// Migrated from bash test infra/tests/check-no-cloud-dev-default_test.sh
// (M-Migrate2 2026-07-10 M16 T2.3 Wave 1). The bash test has a single
// assertion + a 2-stage filter; Go migration expands to 6 per-scenario
// tests covering the exception rule.
package nocloud

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdirall %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writeexec %s: %v", path, err)
	}
}

const cleanScript = `#!/usr/bin/env bash
# Uses canonical hostname — no cloud-dev default.
API="${API:-https://app-dev.getloopscale.com}"
echo "$API"
`

const forbiddenScript = `#!/usr/bin/env bash
# WRONG: default value points at cloud-dev (Caddy 301 trap).
API="${API:-https://cloud-dev.getloopscale.com/api/v1/projects}"
echo "$API"
`

const webhookAllowedScript = `#!/usr/bin/env bash
# EC-1 absorption: webhook handler is handler-direct — allowed.
WEBHOOK="${WEBHOOK_URL:-https://cloud-dev.getloopscale.com/api/v1/webhooks/github}"
curl -X POST "$WEBHOOK"
`

const mixedScript = `#!/usr/bin/env bash
# Two lines: one violation + one allowed webhook exception.
BAD="${API:-https://cloud-dev.getloopscale.com/api/v1/projects}"
OK="${WEBHOOK:-https://cloud-dev.getloopscale.com/api/v1/webhooks/github}"
`

func TestCheck_CleanRepo_PASS(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, "archive/canonical-tests-v1/scripts/local/seed.sh"), cleanScript)
	writeExec(t, filepath.Join(root, "infra/tests/e2e/e2e.sh"), cleanScript)
	writeExec(t, filepath.Join(root, "infra/scripts/local/setup.sh"), cleanScript)

	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected nil, got %v; failures=%v", err, outcome.Failures)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures, got %v", outcome.Failures)
	}
}

func TestCheck_ForbiddenPattern_FAIL(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, "infra/scripts/local/bad.sh"), forbiddenScript)

	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if len(outcome.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %v", outcome.Failures)
	}
}

func TestCheck_WebhookHandlerException_PASS(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, "infra/scripts/local/webhook.sh"), webhookAllowedScript)

	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected nil err (webhook exception), got %v; failures=%v", err, outcome.Failures)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures (EC-1 exception), got %v", outcome.Failures)
	}
}

func TestCheck_MixedFile_OnlyReportsViolation(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, "infra/tests/e2e/mixed.sh"), mixedScript)

	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for mixed file, got %v", err)
	}
	if len(outcome.Failures) != 1 {
		t.Fatalf("expected exactly 1 failure (only non-webhook line), got %v", outcome.Failures)
	}
}

// Every scan root absent means nothing was read, which is not a clean result.
func TestCheck_AllScanRootsMissing_IsNothingScanned(t *testing.T) {
	if _, err := violationOf(Check(Config{RepoRoot: t.TempDir()})); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("expected ErrNothingScanned when no scan root exists, got %v", err)
	}
}

func TestCheck_ViolationInEachOfThreeRoots(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, "archive/canonical-tests-v1/scripts/local/bad1.sh"), forbiddenScript)
	writeExec(t, filepath.Join(root, "infra/tests/e2e/bad2.sh"), forbiddenScript)
	writeExec(t, filepath.Join(root, "infra/scripts/local/bad3.sh"), forbiddenScript)

	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if len(outcome.Failures) != 3 {
		t.Fatalf("expected 3 failures (one per root), got %v", outcome.Failures)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	root := qtestutil.Root(t)

	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("real repo violation: %v; failures=%v", err, outcome.Failures)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures against real repo, got %v", outcome.Failures)
	}
}
