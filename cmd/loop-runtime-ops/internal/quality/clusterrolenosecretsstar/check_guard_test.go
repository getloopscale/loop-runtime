// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package clusterrolenosecretsstar

import (
	"os"
	"path/filepath"
	"testing"
)

// A chart-wide `{{- if .Values.rbac.create }}` wrapper decides whether the
// ClusterRole exists at all; it does not make the secrets grant inside it
// conditional on the cutover. Every operator chart is shaped exactly like this.
func TestCheck_FileWideRBACCreateWrapper_IsNotACutoverGuard(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "operator", `{{- if .Values.rbac.create }}
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "update"]
{{- end }}
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass || len(out.Hits) != 1 {
		t.Fatalf("want 1 hit for a grant wrapped only by rbac.create, got pass=%v hits=%+v", out.Pass, out.Hits)
	}
}

// An arbitrary feature flag is not the cutover either: only the declared
// cutover expression makes a cluster-wide grant operator-controlled.
func TestCheck_ArbitraryFeatureFlag_IsNotACutoverGuard(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "feature", `rules:
  {{- if .Values.someFeature.enabled }}
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get"]
  {{- end }}
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatal("want FAIL: someFeature.enabled is not the cutover guard")
	}
}

// The else branch of the cutover guard is exactly the case the guard is NOT set.
func TestCheck_ElseBranchOfCutoverGuard_IsUnguarded(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "elsebranch", `rules:
  {{- if .Values.rbac.legacyClusterWideSecrets }}
  - apiGroups: ["runtime.getloopscale.com"]
    resources: ["*"]
    verbs: ["get"]
  {{- else }}
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
  {{- end }}
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatal("want FAIL: a grant in the else branch of the cutover guard is unconditional in practice")
	}
}

// `range`/`with` open a block whose `end` must not close the cutover guard
// that surrounds it.
func TestCheck_RangeInsideCutoverGuard_KeepsTheGuard(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "rangeinside", `rules:
  {{- if .Values.rbac.legacyClusterWideSecrets }}
  {{- range .Values.extraRules }}
  - {{ . }}
  {{- end }}
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
  {{- end }}
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("want PASS: the grant is still inside the cutover guard, got %+v", out.Hits)
	}
}

// A `range` closed before the grant must not leave the counter one short,
// which used to let an `end` belonging to range close an unrelated guard.
func TestCheck_RangeEndDoesNotCloseAnOuterFrame(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "rangeouter", `{{- if .Values.rbac.create }}
rules:
  {{- range .Values.extraRules }}
  - {{ . }}
  {{- end }}
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
{{- end }}
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pass {
		t.Fatal("want FAIL: the grant is only under rbac.create")
	}
}

// An unreadable clusterrole.yaml is a template the gate could not audit; a
// security gate must fail closed rather than report it clean.
func TestCheck_UnreadableClusterRole_FailsClosed(t *testing.T) {
	tmp := t.TempDir()
	writeChart(t, tmp, "unreadable", "rules: []\n")
	p := filepath.Join(tmp, "charts", "unreadable", "templates", "clusterrole.yaml")
	// A dangling symlink is unreadable for every uid, root included.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(p), "does-not-exist"), p); err != nil {
		t.Fatal(err)
	}

	if _, err := Check(Config{RepoRoot: tmp}); err == nil {
		t.Fatal("want an error for an unreadable clusterrole.yaml, got nil (fail-open)")
	}
}
