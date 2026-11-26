// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package nolegacysatoken

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

func writeYAML(t *testing.T, tmp, rel, content string) {
	t.Helper()
	full := filepath.Join(tmp, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCheck_EmptyRoot_Errors(t *testing.T) {
	_, err := Check(Config{RepoRoot: ""})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheck_NoYAMLs_Pass(t *testing.T) {
	tmp := t.TempDir()
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS, got hits: %+v", out.Hits)
	}
}

func TestCheck_LegacyTypeInYAML_FAIL(t *testing.T) {
	tmp := t.TempDir()
	writeYAML(t, tmp, "charts/bad/templates/sa-token.yaml", `apiVersion: v1
kind: Secret
metadata:
  name: bad-token
type: kubernetes.io/service-account-token
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL")
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(out.Hits))
	}
	if !strings.Contains(out.Hits[0].File, "charts/bad/templates/sa-token.yaml") {
		t.Errorf("unexpected file: %s", out.Hits[0].File)
	}
}

func TestCheck_CommentMentioningType_PASS(t *testing.T) {
	tmp := t.TempDir()
	writeYAML(t, tmp, "charts/x/templates/cm.yaml", `apiVersion: v1
kind: ConfigMap
metadata:
  name: x
data:
  # This SA used to be type: kubernetes.io/service-account-token before T7.
  note: |
    Migrated to Vault.
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS — comment should not trigger gate, got: %+v", out.Hits)
	}
}

func TestCheck_MultipleHits_AllReported(t *testing.T) {
	tmp := t.TempDir()
	writeYAML(t, tmp, "charts/a/templates/x.yaml", `type: kubernetes.io/service-account-token`)
	writeYAML(t, tmp, "infra/helm/b/templates/y.yaml", `type: kubernetes.io/service-account-token`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out.Hits) != 2 {
		t.Errorf("expected 2 hits across both scan dirs, got %d", len(out.Hits))
	}
}

func TestCheck_Allowlist_Skipped(t *testing.T) {
	tmp := t.TempDir()
	writeYAML(t, tmp, "charts/allowed/templates/legacy.yaml", `type: kubernetes.io/service-account-token`)
	out, err := Check(Config{
		RepoRoot:       tmp,
		AllowlistPaths: []string{"charts/allowed/templates/legacy.yaml"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS for allowlisted path, got: %+v", out.Hits)
	}
}

func TestCheck_NonYAML_Ignored(t *testing.T) {
	tmp := t.TempDir()
	writeYAML(t, tmp, "charts/x/templates/README.md", `type: kubernetes.io/service-account-token`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS — non-YAML files ignored")
	}
}

func TestFormatOutcome_Pass(t *testing.T) {
	s := FormatOutcome(Outcome{Pass: true})
	if !strings.Contains(s, "PASS") {
		t.Errorf("expected PASS in output, got: %s", s)
	}
}

func TestFormatOutcome_FailHasHitDetails(t *testing.T) {
	out := Outcome{
		Pass: false,
		Hits: []Hit{{File: "f.yaml", Line: 10, Reason: "R", Suggestion: "S"}},
	}
	s := FormatOutcome(out)
	if !strings.Contains(s, "f.yaml:10") || !strings.Contains(s, "R") || !strings.Contains(s, "S") {
		t.Errorf("missing hit detail in output: %s", s)
	}
}

// LIVE invariant: the actual repo must PASS this gate (no legacy SA tokens in
// chart templates). It used to skip on a failure, "pending T7"; the tree has
// passed since, so a skip there could only ever hide a regression.
func TestCheck_LIVE_Passes(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Fatalf("legacy SA-token literals present in the real tree:\n%s", FormatOutcome(out))
	}
}
