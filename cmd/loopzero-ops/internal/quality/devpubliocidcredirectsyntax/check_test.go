package devpubliocidcredirectsyntax

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const happyFixture = `#!/usr/bin/env bash
set -euo pipefail

log() { echo "[log] $*"; }
assert() { if "$@"; then echo PASS; else exit 1; fi; }
test_provider() { curl -s /self-service/login/browser; }

# google oidc provider
curl -s /self-service/methods/oidc/callback

exit 0
`

const missingTestProviderFixture = `#!/usr/bin/env bash
set -euo pipefail

log() { true; }
assert() { true; }

curl -s /self-service/login/browser
curl -s /self-service/methods/oidc/callback

exit 0
`

func writeFixture(t *testing.T, dir, scriptContent, allowlistContent string) {
	t.Helper()
	scriptPath := filepath.Join(dir, "infra", "tests", "e2e")
	if err := os.MkdirAll(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptPath, "dev-public-oidc-redirect_test.sh"),
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
}

func TestCheck_HappyPath_PASS(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp, happyFixture,
		"| infra/tests/e2e/dev-public-oidc-redirect_test.sh | ... |\n")

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
	out, err := Check(Config{RepoRoot: tmp})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !containsSubstr(out.Failures, "file exists") {
		t.Errorf("expected 'file exists' failure, got %v", out.Failures)
	}
}

func TestCheck_MissingTestProvider_FAIL(t *testing.T) {
	tmp := t.TempDir()
	writeFixture(t, tmp, missingTestProviderFixture,
		"| infra/tests/e2e/dev-public-oidc-redirect_test.sh | ... |\n")

	out, err := Check(Config{RepoRoot: tmp})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if !containsSubstr(out.Failures, "test_provider function defined") {
		t.Errorf("expected 'test_provider function defined' failure, got %v", out.Failures)
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
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "ROADMAP.md")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("ROADMAP.md not found")
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
