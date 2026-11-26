// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package platformcrnamespace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

func tree(t *testing.T, files map[string]string) Config {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Config{RepoRoot: root, Dirs: []string{"infra"}}
}

const canonicalCR = "apiVersion: runtime.getloopscale.com/v1\nkind: Tenant\nmetadata:\n  name: tn-x\n  namespace: loopruntime-system\n"

func TestCanonicalNamespacePasses(t *testing.T) {
	out, err := violationOf(Check(tree(t, map[string]string{"infra/a.yaml": canonicalCR})))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(out.Checked) != 1 {
		t.Errorf("checked = %d; want 1", len(out.Checked))
	}
}

// The MEASURED defect (B-095): the production bootstrap applied the Tenant in `default`.
func TestNamespaceOutsideCanonicalFails(t *testing.T) {
	outside := "apiVersion: runtime.getloopscale.com/v1\nkind: Tenant\nmetadata:\n  name: tn-x\n  namespace: default\n"
	out, err := violationOf(Check(tree(t, map[string]string{"infra/a.yaml": outside})))
	if !errors.Is(err, ErrOutsideNamespace) {
		t.Fatalf("err = %v; want ErrOutsideNamespace", err)
	}
	if len(out.Violations) != 1 || out.Violations[0].Namespace != "default" {
		t.Errorf("violations = %+v", out.Violations)
	}
}

// A heredoc inside shell is where the real defect lived. A YAML parser does not read that without
// running the shell first — which is why detection is textual.
func TestFindsInsideShellHeredoc(t *testing.T) {
	sh := "#!/usr/bin/env bash\nkubectl apply -f - <<'EOF'\napiVersion: runtime.getloopscale.com/v1\nkind: Tenant\nmetadata:\n  name: tn-x\n  namespace: default\nEOF\n"
	if _, err := violationOf(Check(tree(t, map[string]string{"infra/s.sh": sh}))); !errors.Is(err, ErrOutsideNamespace) {
		t.Fatalf("err = %v; the CR was in a heredoc, which is exactly where the real defect was", err)
	}
}

// A `kind: Environment` from ANOTHER group is not a platform CR. Without the apiVersion anchor
// the gate would accuse any same-named kind.
func TestSameNamedKindFromAnotherGroupDoesNotCount(t *testing.T) {
	other := "apiVersion: other.example.com/v1\nkind: Environment\nmetadata:\n  namespace: default\n"
	if _, err := violationOf(Check(tree(t, map[string]string{"infra/a.yaml": other}))); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("err = %v; an `Environment` from another apiGroup is not a platform CR", err)
	}
}

// A shell variable: the value is a runtime one. Judging it here would be guessing.
func TestNamespaceInVariableIsNotAccused(t *testing.T) {
	v := "apiVersion: runtime.getloopscale.com/v1\nkind: Tenant\nmetadata:\n  namespace: ${NS}\n"
	if _, err := violationOf(Check(tree(t, map[string]string{"infra/s.sh": v}))); err != nil {
		t.Fatalf("err = %v; the value of ${NS} is decided at execution time", err)
	}
}

// A scan that reaches nothing reports green over nothing — the `PASS — 0 routes` shape.
func TestEmptyScanIsAnError(t *testing.T) {
	if _, err := violationOf(Check(tree(t, map[string]string{"infra/a.yaml": "kind: ConfigMap\n"}))); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("err = %v; want ErrNothingScanned", err)
	}
}

// THE REAL POPULATION (`rules/testing.md` § 4.2). The mutant above proves the detector fires;
// only this says what it finds in the tree that exists.
//
// SCAN RESULT, measured on 2026-08-19: **one** violation — the production
// bootstrap-platform script at line 199, a Tenant in `default`. This test failed because of
// it before the same-day fix. Recording the number here is what distinguishes "found
// nothing" from "did not look".
//
// The floor below is 2, not 3, since that script was deleted as dead (it had no caller,
// #240). The tree carries two seeds now, both under infra/scripts —
// drill/velero-dr-drill.sh and local/bootstrap-seed-tenant-cr.sh — and a scan that finds
// fewer is measuring less than the tree.
func TestRealTreeHasNoCROutsideNamespace(t *testing.T) {
	root := qtestutil.Root(t)
	out, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(out.Checked) < 2 {
		t.Errorf("checked = %d (%+v); the tree carries two seeds — a "+
			"scan that finds fewer is measuring less than the tree", len(out.Checked), out.Checked)
	}
}
