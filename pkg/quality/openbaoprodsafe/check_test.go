// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package openbaoprodsafe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: create a minimal chart tree under tmpdir with given values-prod.yaml + vap content
func setupChartFixture(t *testing.T, valuesProd, sharedValues, vap string) string {
	t.Helper()
	tmp := t.TempDir()
	chartDir := filepath.Join(tmp, "infra", "helm", "openbao")
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if valuesProd != "" {
		if err := os.WriteFile(filepath.Join(chartDir, "values-prod.yaml"), []byte(valuesProd), 0o644); err != nil {
			t.Fatalf("write values-prod: %v", err)
		}
	}
	if sharedValues != "" {
		if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte(sharedValues), 0o644); err != nil {
			t.Fatalf("write values: %v", err)
		}
	}
	if vap != "" {
		if err := os.WriteFile(filepath.Join(chartDir, "templates", "vap-dev-mode.yaml"), []byte(vap), 0o644); err != nil {
			t.Fatalf("write vap: %v", err)
		}
	}
	return tmp
}

const cleanProdValues = `
env: prod
size: 3
storage:
  type: raft
image:
  repository: ghcr.io/openbao/openbao
  tag: "2.2.0"
unseal:
  type: awskms
devMode:
  enabled: false
  rootToken: ""
`

const cleanSharedValues = `
image:
  repository: ghcr.io/openbao/openbao
  tag: "2.2.0"
`

const cleanVap = `
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: openbao-no-devmode-in-prod
spec:
  matchConditions:
    - name: exclude-system-controllers
      expression: |
        request.userInfo.username != 'system:serviceaccount:kube-system:namespace-controller'
`

func TestCheck_AllClean_Pass(t *testing.T) {
	root := setupChartFixture(t, cleanProdValues, cleanSharedValues, cleanVap)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS; got violations: %v", out.Violations)
	}
	if out.Skipped {
		t.Errorf("unexpected skip on clean chart")
	}
}

func TestCheck_NoChart_Skip(t *testing.T) {
	tmp := t.TempDir()
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !out.Skipped {
		t.Errorf("expected Skip when chart absent")
	}
	if !out.Pass {
		t.Errorf("expected Pass when skipped")
	}
}

func TestCheck_DevLiteral_LoopZeroDev_Fail(t *testing.T) {
	prodWithDevLit := cleanProdValues + `
extraToken: "loopzero-dev-something"
`
	root := setupChartFixture(t, prodWithDevLit, cleanSharedValues, cleanVap)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL on loopzero-dev-* literal in prod values")
	}
	found := false
	for _, v := range out.Violations {
		if strings.Contains(v, "loopzero-dev-something") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected violation citing 'loopzero-dev-something'; got %v", out.Violations)
	}
}

func TestCheck_DevModeEnabledTrue_Fail(t *testing.T) {
	prodWithDevMode := `
env: prod
devMode:
  enabled: true
  rootToken: ""
image:
  tag: "2.2.0"
`
	root := setupChartFixture(t, prodWithDevMode, cleanSharedValues, cleanVap)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL on devMode.enabled: true in prod values")
	}
	found := false
	for _, v := range out.Violations {
		if strings.Contains(v, "devMode.enabled: true") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected violation citing devMode.enabled: true; got %v", out.Violations)
	}
}

func TestCheck_RootTokenNonEmpty_Fail(t *testing.T) {
	prodWithToken := `
env: prod
devMode:
  enabled: false
  rootToken: "loopzero-dev-vault-root"
image:
  tag: "2.2.0"
`
	root := setupChartFixture(t, prodWithToken, cleanSharedValues, cleanVap)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL on rootToken non-empty in prod values")
	}
}

func TestCheck_ForbiddenTag_Latest_Fail(t *testing.T) {
	prodLatest := `
env: prod
image:
  repository: ghcr.io/openbao/openbao
  tag: "latest"
devMode:
  enabled: false
  rootToken: ""
`
	root := setupChartFixture(t, prodLatest, cleanSharedValues, cleanVap)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL on image.tag=latest")
	}
}

func TestCheck_ForbiddenTag_Main_Fail(t *testing.T) {
	for _, badTag := range []string{"main", "edge", "devel", "stable"} {
		yaml := `
env: prod
image:
  repository: ghcr.io/openbao/openbao
  tag: "` + badTag + `"
devMode:
  enabled: false
  rootToken: ""
`
		root := setupChartFixture(t, yaml, cleanSharedValues, cleanVap)
		out, _ := Check(Config{RepoRoot: root})
		if out.Pass {
			t.Errorf("expected FAIL on image.tag=%s", badTag)
		}
	}
}

func TestCheck_MissingVap_Fail(t *testing.T) {
	root := setupChartFixture(t, cleanProdValues, cleanSharedValues, "")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL when vap-dev-mode.yaml absent (EC-04 missing)")
	}
	found := false
	for _, v := range out.Violations {
		if strings.Contains(v, "vap-dev-mode.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected violation citing vap-dev-mode.yaml; got %v", out.Violations)
	}
}

func TestCheck_VapWithoutMatchConditions_Fail(t *testing.T) {
	vapNoMatch := `
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: openbao-no-devmode-in-prod
spec:
  validations: []
`
	root := setupChartFixture(t, cleanProdValues, cleanSharedValues, vapNoMatch)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL when VAP missing matchConditions (B33)")
	}
}

func TestCheck_VapMatchConditionsMissingNsController_Fail(t *testing.T) {
	vapPartial := `
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: openbao-no-devmode-in-prod
spec:
  matchConditions:
    - name: foo
      expression: "true"
`
	root := setupChartFixture(t, cleanProdValues, cleanSharedValues, vapPartial)
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass {
		t.Errorf("expected FAIL when matchConditions doesn't reference namespace-controller (B33)")
	}
}

func TestExtractImageTag(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantTag string
	}{
		{
			name: "simple",
			yaml: `
image:
  repository: foo
  tag: "1.2.3"
`,
			wantTag: "1.2.3",
		},
		{
			name: "no quotes",
			yaml: `
image:
  tag: 2.2.0
`,
			wantTag: "2.2.0",
		},
		{
			name: "absent",
			yaml: `
image:
  repository: foo
`,
			wantTag: "",
		},
		{
			name: "different block",
			yaml: `
other:
  tag: "wrong"
image:
  tag: "right"
`,
			wantTag: "right",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractImageTag(tc.yaml)
			if got != tc.wantTag {
				t.Errorf("got %q; want %q", got, tc.wantTag)
			}
		})
	}
}

func TestHasDevModeEnabledTrue(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want bool
	}{
		{
			name: "true",
			yaml: `devMode:
  enabled: true`,
			want: true,
		},
		{
			name: "false",
			yaml: `devMode:
  enabled: false`,
			want: false,
		},
		{
			name: "absent",
			yaml: `image:
  tag: "1.0"`,
			want: false,
		},
		{
			name: "unrelated enabled true",
			yaml: `serviceMonitor:
  enabled: true
devMode:
  enabled: false`,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hasDevModeEnabledTrue(tc.yaml)
			if got != tc.want {
				t.Errorf("got %v; want %v", got, tc.want)
			}
		})
	}
}
