// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package devpublicenv

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "infra", "helmfile", "environments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dev-public.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

const canonical = `# dev-public.yaml canonical shape
publicAuthUrl: "https://auth-dev.loopzero.example"
publicCloudUrl: "https://app-dev.loopzero.example"
uiBaseUrl: "https://app-dev.loopzero.example"
dashboardOrigin: "https://app-dev.loopzero.example"
cookieDomain: "loopzero.example"
corsAllowedOrigins:
  - "https://app-dev.loopzero.example"
allowedReturnUrls:
  - "https://app-dev.loopzero.example"
  - "https://app-dev.loopzero.example/"
stripe:
  webhookSecretRef: loopzero-stripe-webhook-dev
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

func TestCheck_LocalhostLeakFails(t *testing.T) {
	body := canonical + "\nlegacyKratosUrl: http://localhost:4455\n"
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for localhost leak, got %v", err)
	}
}

func TestCheck_LegacyNonDevURLFails(t *testing.T) {
	body := canonical + "\nlegacyRedirectUrl: https://app.loopzero.example\n"
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for legacy non-dev URL, got %v", err)
	}
}

func TestCheck_DevSuffixEnvVarFails(t *testing.T) {
	body := canonical + "\nenv:\n  - name: LOOPZERO_GITHUB_CLIENT_ID_DEV\n    value: xxx\n"
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for _DEV env var override, got %v", err)
	}
}

func TestCheck_MissingCookieDomainFails(t *testing.T) {
	body := strings.Replace(canonical, `cookieDomain: "loopzero.example"`, `cookieDomain: "localhost"`, 1)
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for missing cookieDomain, got %v", err)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(abs, envPath)); statErr == nil {
			outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
			if checkErr != nil {
				t.Fatalf("real dev-public.yaml must satisfy invariant. err=%v failures=%v", checkErr, outcome.Failures)
			}
			return
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("could not locate dev-public.yaml within 8 parent dirs")
}

// TestCheck_CloudDevReappearingFails locks in what removing the host bought.
//
// `cloud-dev` was removed on 2026-08-10 for being a second name for the same dashboard,
// serving only 301. The real risk is not someone recreating the host — it is someone
// putting the string back into `corsAllowedOrigins` or `allowedReturnUrls` in a hurried
// fix, and thereby authorizing in auth an origin that DOES NOT RESPOND. An authorized
// origin that nobody controls is worse than one origin fewer.
func TestCheck_CloudDevReappearingFails(t *testing.T) {
	root := writeFixture(t, canonical+"\n  - \"https://cloud-dev.loopzero.example\"\n")
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when reintroducing cloud-dev into an origin list, got %v", err)
	}
}
