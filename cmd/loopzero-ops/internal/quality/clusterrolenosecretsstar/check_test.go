// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package clusterrolenosecretsstar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

// helper: write a chart file under tmpRoot/charts/<chartName>/templates/clusterrole.yaml
func writeChart(t *testing.T, tmpRoot, chartName, content string) {
	t.Helper()
	dir := filepath.Join(tmpRoot, "charts", chartName, "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clusterrole.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCheck_EmptyRepoRoot_Errors(t *testing.T) {
	_, err := Check(Config{RepoRoot: ""})
	if err == nil {
		t.Fatal("expected error for empty RepoRoot")
	}
}

func TestCheck_NoChartsDir_Pass(t *testing.T) {
	tmp := t.TempDir()
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS with no chart dirs, got hits: %+v", out.Hits)
	}
}

func TestCheck_UnconditionalSecretsRule_FAIL(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "bad-chart", `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: bad-chart
rules:
  - apiGroups: [""]
    resources: ["secrets", "configmaps"]
    verbs: ["get", "list"]
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL for unconditional secrets rule")
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(out.Hits), out.Hits)
	}
	hit := out.Hits[0]
	if !strings.Contains(hit.File, "bad-chart") {
		t.Errorf("expected hit file to contain bad-chart, got %s", hit.File)
	}
	// Regex captures either "secrets" or "configmaps" when both appear in
	// the same line; the leftmost-greedy match yields the rightmost alternation
	// candidate. Accept either as evidence the rule was detected.
	if hit.Resources != "secrets" && hit.Resources != "configmaps" {
		t.Errorf("expected Resources in [secrets, configmaps], got %s", hit.Resources)
	}
	if !strings.Contains(hit.Suggestion, "legacyClusterWideSecrets") {
		t.Errorf("expected suggestion to mention legacyClusterWideSecrets, got %s", hit.Suggestion)
	}
}

func TestCheck_GuardedSecretsRule_PASS(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "good-chart", `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: good-chart
rules:
  - apiGroups: ["loopzero.io"]
    resources: ["*"]
    verbs: ["get", "list"]
  {{- if .Values.rbac.legacyClusterWideSecrets }}
  - apiGroups: [""]
    resources: ["secrets", "configmaps"]
    verbs: ["get", "list"]
  {{- end }}
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS for guarded rule, got hits: %+v", out.Hits)
	}
}

func TestCheck_GuardedConfigmapsRule_PASS(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "configmaps-chart", `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
rules:
  {{- if .Values.rbac.legacyClusterWideSecrets }}
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get"]
  {{- end }}
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS for guarded configmaps, got hits: %+v", out.Hits)
	}
}

func TestCheck_AllowlistPath_Skipped(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "allowed-chart", `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
`)
	out, err := Check(Config{
		RepoRoot:       tmp,
		AllowlistPaths: []string{"charts/allowed-chart/templates/clusterrole.yaml"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS for allowlisted chart, got hits: %+v", out.Hits)
	}
}

func TestCheck_NestedGuards_Tracked(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "nested", `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
rules:
  {{- if .Values.rbac.create }}
  {{- if .Values.rbac.legacyClusterWideSecrets }}
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
  {{- end }}
  {{- end }}
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS for nested guards, got hits: %+v", out.Hits)
	}
}

func TestCheck_MixedGuardedAndUnguarded_PartialFAIL(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "mixed", `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
rules:
  {{- if .Values.rbac.legacyClusterWideSecrets }}
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
  {{- end }}
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get"]
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL for the unguarded configmaps rule")
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected exactly 1 hit (the unguarded one), got %d", len(out.Hits))
	}
	if out.Hits[0].Resources != "configmaps" {
		t.Errorf("expected the unguarded configmaps to be flagged, got %s", out.Hits[0].Resources)
	}
}

func TestCheck_IgnoresOtherTemplateFiles(t *testing.T) {
	tmp := t.TempDir()
	// Put the offending content in a NON-clusterrole.yaml file — should be ignored.
	dir := filepath.Join(tmp, "charts", "decoy", "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
`
	if err := os.WriteFile(filepath.Join(dir, "role.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS — Role files (not ClusterRole) should be ignored, got hits: %+v", out.Hits)
	}
}

func TestFormatOutcome_PassMessage(t *testing.T) {
	s := FormatOutcome(Outcome{Pass: true})
	if !strings.Contains(s, "PASS") {
		t.Errorf("expected PASS in formatted output, got: %s", s)
	}
}

func TestFormatOutcome_FailIncludesHitDetails(t *testing.T) {
	out := Outcome{
		Pass: false,
		Hits: []Hit{
			{File: "charts/x/templates/clusterrole.yaml", Line: 42, Resources: "secrets", Reason: "BAD", Suggestion: "FIX"},
		},
	}
	s := FormatOutcome(out)
	if !strings.Contains(s, "charts/x/templates/clusterrole.yaml:42") {
		t.Errorf("expected file:line in output, got: %s", s)
	}
	if !strings.Contains(s, "BAD") {
		t.Errorf("expected reason in output, got: %s", s)
	}
	if !strings.Contains(s, "FIX") {
		t.Errorf("expected suggestion in output, got: %s", s)
	}
}

// LIVE invariant: the loopzero-api chart — the SA deep review finding #1 was
// about — keeps its cluster-wide grant inside the cutover guard (T5.13).
// The whole-repo verdict is the gate's own output (`loopzero-ops quality
// cluster-role-no-secrets-star`); asserting it here would pin the state of
// charts this package does not own.
func TestCheck_LIVE_LoopZeroAPIClusterRoleIsGuarded(t *testing.T) {
	root := qtestutil.Root(t)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, h := range out.Hits {
		if strings.HasPrefix(h.File, filepath.Join("charts", "loopzero-api")+string(filepath.Separator)) ||
			strings.HasPrefix(h.File, filepath.Join("infra", "helm", "loopzero-api")+string(filepath.Separator)) {
			t.Errorf("loopzero-api grant escaped the cutover guard: %s:%d (%s)", h.File, h.Line, h.Resources)
		}
	}
}
