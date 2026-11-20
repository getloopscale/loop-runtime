// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package noenvvaultsecrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: build a tmp repo with a chart fixture
func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return root
}

func TestCheck_ForbiddenEnvFails(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"charts/loopzero-api/templates/deployment.yaml": `
spec:
  containers:
  - name: loopzero-api
    env:
    - name: GITHUB_APP_PRIVATE_KEY
      value: "fakekey"
`,
	})
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL but got PASS")
	}
	if len(out.Forbidden) != 1 {
		t.Errorf("expected 1 forbidden, got %d", len(out.Forbidden))
	}
	if len(out.Forbidden) > 0 && out.Forbidden[0].EnvName != "GITHUB_APP_PRIVATE_KEY" {
		t.Errorf("unexpected envName: %s", out.Forbidden[0].EnvName)
	}
}

func TestCheck_CleanDeploymentPasses(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"infra/helm/loopzero-api/templates/deployment.yaml": `
spec:
  containers:
  - name: loopzero-api
    env:
    - name: PORT
      value: "8080"
    - name: LOG_LEVEL
      value: "info"
`,
	})
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS but got FAIL: %s", out.FormatReport())
	}
	if len(out.Forbidden) != 0 || len(out.PatternHits) != 0 {
		t.Errorf("expected zero hits, got forbidden=%d pattern=%d", len(out.Forbidden), len(out.PatternHits))
	}
}

func TestCheck_AllowlistSkip(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"charts/loopzero-api/templates/deployment.yaml": `
spec:
  containers:
  - name: loopzero-api
    env:
    - name: GITEA_TOKEN
      valueFrom:
        secretKeyRef:
          name: gitea-admin
          key: token
`,
	})
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS (GITEA_TOKEN in DefaultAllowlist) but got FAIL: %s", out.FormatReport())
	}
}

func TestCheck_PatternFails(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"charts/foo/templates/deployment.yaml": `
spec:
  containers:
  - name: foo
    env:
    - name: MY_CUSTOM_PRIVATE_KEY
      value: "leaked"
`,
	})
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL for pattern match but got PASS")
	}
	if len(out.PatternHits) != 1 {
		t.Errorf("expected 1 pattern hit, got %d", len(out.PatternHits))
	}
}

func TestCheck_PatternWarnOnly(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"charts/foo/templates/deployment.yaml": `
spec:
  containers:
  - name: foo
    env:
    - name: SOME_NEW_TOKEN
      value: "fake"
`,
	})
	out, err := Check(Config{RepoRoot: root, WarnOnly: true})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	// WarnOnly + only pattern hits = PASS (but hits are still recorded)
	if !out.Pass {
		t.Errorf("expected PASS in WarnOnly mode but got FAIL: %s", out.FormatReport())
	}
	if len(out.PatternHits) != 1 {
		t.Errorf("expected 1 pattern hit (recorded), got %d", len(out.PatternHits))
	}
}

func TestCheck_CustomAllowlist(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"charts/foo/templates/deployment.yaml": `
spec:
  containers:
  - name: foo
    env:
    - name: MY_FEATURE_TOKEN
      value: "feature_flag_value"
`,
	})
	out, err := Check(Config{
		RepoRoot:  root,
		Allowlist: []string{"MY_FEATURE_TOKEN"},
	})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS with custom allowlist but got FAIL: %s", out.FormatReport())
	}
}

func TestCheck_CommentsIgnored(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"charts/foo/templates/deployment.yaml": `
spec:
  containers:
  - name: foo
    env:
    # - name: GITHUB_APP_PRIVATE_KEY  -- this is a comment, should NOT trip the gate
    # Note: do not use COSIGN_PASSWORD as env var
    - name: PORT
      value: "8080"
`,
	})
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS (forbidden in comments only) but got FAIL: %s", out.FormatReport())
	}
}

func TestCheck_NonTemplateFilesSkipped(t *testing.T) {
	root := writeFixture(t, map[string]string{
		// File outside /templates/ — should be skipped
		"charts/foo/values.yaml": `env:
- name: GITHUB_APP_PRIVATE_KEY
  value: fakeval
`,
	})
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS (non-template file skipped) but got FAIL: %s", out.FormatReport())
	}
}

func TestCheck_MultipleViolations(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"charts/a/templates/deployment.yaml": `
spec:
  env:
  - name: GITHUB_APP_PRIVATE_KEY
    value: x
`,
		"charts/b/templates/deployment.yaml": `
spec:
  env:
  - name: COSIGN_PASSWORD
    value: y
  - name: LOOPZERO_SECRET_ENCRYPTION_KEY
    value: z
`,
	})
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL with multiple violations")
	}
	if len(out.Forbidden) != 3 {
		t.Errorf("expected 3 forbidden, got %d", len(out.Forbidden))
	}
}

func TestFormatReport_ContainsActionable(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"charts/x/templates/deployment.yaml": `
spec:
  env:
  - name: GITHUB_APP_PRIVATE_KEY
    value: x
`,
	})
	out, _ := Check(Config{RepoRoot: root})
	report := out.FormatReport()
	if !strings.Contains(report, "FAIL") {
		t.Errorf("report should contain FAIL")
	}
	if !strings.Contains(report, "GITHUB_APP_PRIVATE_KEY") {
		t.Errorf("report should mention the offending env var")
	}
	if !strings.Contains(report, "ExternalSecret CR") {
		t.Errorf("report should suggest ExternalSecret CR migration")
	}
}

// CRITICAL test: valueFrom.secretKeyRef pattern is the ESO-managed mode we
// WANT. Gate must skip env vars sourced from K8s Secret via secretKeyRef.
// LIVE-validated against 3 false positives in current repo (iter 26-T2.7):
// cloudflared TUNNEL_TOKEN, loopzero-cloud
// LOOPZERO_SERVICE_AUTH_ED25519_PRIVATE_KEY — all valueFrom.secretKeyRef.
func TestCheck_SecretKeyRefPatternSkipped(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"charts/foo/templates/deployment.yaml": `
spec:
  containers:
  - name: foo
    env:
    - name: GITHUB_APP_PRIVATE_KEY
      valueFrom:
        secretKeyRef:
          name: github-app-secret
          key: private-key
    - name: COSIGN_PASSWORD
      valueFrom:
        secretKeyRef:
          name: cosign-key
          key: password
    - name: LOOPZERO_SECRET_ENCRYPTION_KEY
      valueFrom:
        secretKeyRef:
          name: loopzero-api-internal
          key: LOOPZERO_SECRET_ENCRYPTION_KEY
`,
	})
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS (all 3 forbidden are valueFrom.secretKeyRef = ESO-managed) but got FAIL: %s", out.FormatReport())
	}
	if len(out.Forbidden) != 0 || len(out.PatternHits) != 0 {
		t.Errorf("expected zero hits, got forbidden=%d pattern=%d", len(out.Forbidden), len(out.PatternHits))
	}
}

// Even with secretKeyRef look-ahead, a hardcoded value still trips the gate.
func TestCheck_HardcodedValueAfterSecretKeyRefPeerStillFails(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"charts/foo/templates/deployment.yaml": `
spec:
  containers:
  - name: foo
    env:
    - name: SAFE_TOKEN
      valueFrom:
        secretKeyRef:
          name: foo
          key: token
    - name: GITHUB_APP_PRIVATE_KEY
      value: "hardcoded-fake-key"
`,
	})
	out, err := Check(Config{RepoRoot: root, Allowlist: []string{"SAFE_TOKEN"}})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL (GITHUB_APP_PRIVATE_KEY has hardcoded value) but got PASS")
	}
	if len(out.Forbidden) != 1 {
		t.Errorf("expected 1 forbidden, got %d", len(out.Forbidden))
	}
}

func TestSuggestVaultPath_FollowsD6Convention(t *testing.T) {
	cases := map[string]string{
		"GITHUB_APP_PRIVATE_KEY":         "github/app/private/key",
		"LOOPZERO_SECRET_ENCRYPTION_KEY": "loopzero/secret/encryption/key",
		"COSIGN_PASSWORD":                "cosign/password",
	}
	for input, expectKey := range cases {
		got := suggestVaultPath(input)
		if !strings.Contains(got, expectKey) {
			t.Errorf("suggestVaultPath(%s): expected to contain %q, got %s", input, expectKey, got)
		}
		if !strings.Contains(got, "secret/loopzero/<env>") {
			t.Errorf("suggestVaultPath(%s): expected D6 prefix, got %s", input, got)
		}
	}
}
