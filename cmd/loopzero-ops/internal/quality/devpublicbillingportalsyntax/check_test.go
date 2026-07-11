package devpublicbillingportalsyntax

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// canonical happy-path fixture — passes all 8 assertions.
const happyFixture = `#!/usr/bin/env bash
set -euo pipefail

log() {
  echo "[log] $*"
}

assert() {
  if "$@"; then echo "PASS"; else exit 1; fi
}

curl -s https://billing.stripe.com/v1/billing/portal

exit 0
`

// missing "log() {" — should fail assertion 4.
const missingLogFixture = `#!/usr/bin/env bash
set -euo pipefail

assert() { true; }

curl -s /v1/billing

exit 0
`

// foreign exit code (5) — should fail assertion 7.
const foreignExitFixture = `#!/usr/bin/env bash
set -euo pipefail

log() { true; }
assert() { true; }

curl -s /v1/billing

exit 5
`

func writeFixture(t *testing.T, dir, scriptContent, allowlistContent string) string {
	t.Helper()
	scriptPath := filepath.Join(dir, "infra", "tests", "e2e", "live")
	if err := os.MkdirAll(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptPath, "dev-public-billing-portal_test.sh"),
		[]byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}

	allowPath := filepath.Join(dir, ".claude", "rules")
	if err := os.MkdirAll(allowPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(allowPath, "shell-scripts-allowlist.md"),
		[]byte(allowlistContent), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCheck_HappyPath_PASS(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp, happyFixture,
		"| infra/tests/e2e/live/dev-public-billing-portal_test.sh | ... |\n")

	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("expected nil error, got %v; failures: %v", err, out.Failures)
	}
	if len(out.Failures) != 0 {
		t.Errorf("expected no failures, got %v", out.Failures)
	}
}

func TestCheck_MissingScript_FAIL(t *testing.T) {
	tmp := t.TempDir()
	// no fixture written
	out, err := Check(Config{RepoRoot: tmp})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !containsSubstr(out.Failures, "file exists") {
		t.Errorf("expected 'file exists' failure, got %v", out.Failures)
	}
}

func TestCheck_MissingLogFunc_FAIL(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp, missingLogFixture,
		"| infra/tests/e2e/live/dev-public-billing-portal_test.sh | ... |\n")

	out, err := Check(Config{RepoRoot: tmp})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !containsSubstr(out.Failures, "log function defined") {
		t.Errorf("expected 'log function defined' failure, got %v", out.Failures)
	}
}

func TestCheck_ForeignExitCode_FAIL(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp, foreignExitFixture,
		"| infra/tests/e2e/live/dev-public-billing-portal_test.sh | ... |\n")

	out, err := Check(Config{RepoRoot: tmp})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !containsSubstr(out.Failures, "foreign exit code 5") {
		t.Errorf("expected 'foreign exit code 5' failure, got %v", out.Failures)
	}
}

func TestCheck_NotAllowlisted_FAIL(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp, happyFixture, "| some-other-script.sh | ... |\n")

	out, err := Check(Config{RepoRoot: tmp})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !containsSubstr(out.Failures, "script allowlisted") {
		t.Errorf("expected 'script allowlisted' failure, got %v", out.Failures)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	// Sanity: real repo state passes the gate.
	root := findRepoRoot(t)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Errorf("real-repo check failed: %v; failures: %v", err, out.Failures)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "ROADMAP.md")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("ROADMAP.md not found walking up")
		}
		dir = parent
	}
	t.Fatal("ROADMAP.md not found within 10 parents")
	return ""
}

func containsSubstr(list []string, substr string) bool {
	for _, s := range list {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
