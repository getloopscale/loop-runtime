// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package esooverlay

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
	dir := filepath.Join(root, "infra", "helmfile", "values", "dev-public")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loopzero-external-secrets.yaml.gotmpl"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

// canonical mirrors the post-T1.2 (Approach A / issue #111) shape.
const canonical = `# dev-public overlay — post-T1.2 workload identity (issue #111 / Approach A / dev-public-cross-cluster-secrets)
# See docs/runbooks.md for the Shamir ceremony (## OpenBao init ceremony).
# EC-1: cross-cluster token review required.
# EC-2: CoreDNS host-alias preserves cert SAN.
# Cross-cluster topology: loopzero-secrets (separate cluster).
openbaoTransit:
  enabled: false
  server: "https://openbao.secrets-system.svc.cluster.local:8200"
  auth:
    kind: "kubernetes"
    kubernetes:
      role: "external-secrets"
      serviceAccountRef:
        name: "external-secrets"
        audiences:
          - "openbao"
  caBundle: ""
`

func TestCheck_CanonicalPasses(t *testing.T) {
	root := writeFixture(t, canonical)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS, got err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_MissingOverlayFails(t *testing.T) {
	root := t.TempDir()
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
}

func TestCheck_EnabledTrueFails(t *testing.T) {
	// Anti-foot-shooting guard: `enabled: true` at column 2 forbidden.
	body := strings.Replace(canonical, "  enabled: false", "  enabled: true", 1)
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for `enabled: true`, got %v", err)
	}
}

func TestCheck_HTTPFails(t *testing.T) {
	body := strings.Replace(canonical, `server: "https://`, `server: "http://`, 1)
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for non-https server, got %v", err)
	}
}

func TestCheck_LegacyAppRoleRoleIdFails(t *testing.T) {
	body := canonical + "  roleId: \"deadbeef\"\n"
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for legacy roleId field, got %v", err)
	}
}

func TestCheck_LegacyAppRoleSecretRefFails(t *testing.T) {
	body := canonical + "  secretRef:\n    name: \"loopzero-openbao-approle\"\n"
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for legacy secretRef, got %v", err)
	}
}

func TestCheck_HardcodedUUIDFails(t *testing.T) {
	body := strings.Replace(canonical, `caBundle: ""`, `caBundle: "12345678-1234-1234-1234-123456789012"`, 1)
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for hardcoded UUID, got %v", err)
	}
}

func TestCheck_CoAuthoredByForbidden(t *testing.T) {
	body := canonical + "# Co-Authored-By: someone\n"
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for trailer, got %v", err)
	}
}

func TestCheck_RealRepoOverlayPasses(t *testing.T) {
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(abs, overlayPath)); statErr == nil {
			outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
			if checkErr != nil {
				t.Fatalf("real overlay must satisfy invariant. err=%v failures=%v", checkErr, outcome.Failures)
			}
			return
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("could not locate the ESO overlay within 8 parent dirs; skipping real-repo assertion")
}
