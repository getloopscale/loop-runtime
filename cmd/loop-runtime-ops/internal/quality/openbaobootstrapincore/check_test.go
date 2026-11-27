// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package openbaobootstrapincore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

// withTaskfile writes a synthetic taskfile and returns a Config for it.
func withTaskfile(t *testing.T, body string) Config {
	t.Helper()
	dir := t.TempDir()
	rel := filepath.Join("taskfiles", "k3d.yml")
	if err := os.MkdirAll(filepath.Join(dir, "taskfiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return Config{RepoRoot: dir, TaskfilePath: rel}
}

const cleanTaskfile = `version: '3'

tasks:
  k3d:secrets:create:
    cmds:
      - k3d cluster create --config infra/k3d/secrets-cluster.yaml

  k3d:secrets:bootstrap:
    cmds:
      - helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e vault-local --kube-context k3d-loop-runtime-secrets --selector name=openbao sync
`

// The canonical path must pass, or the gate is an exemption in its own birth.
func TestCheck_CanonicalTaskfilePasses(t *testing.T) {
	out, err := violationOf(Check(withTaskfile(t, cleanTaskfile)))
	if err != nil {
		t.Fatalf("err = %v; the canonical secrets-cluster path must pass. violations=%v", err, out.Violations)
	}
	if out.LinesScanned == 0 {
		t.Error("LinesScanned = 0; a check that parsed nothing verified nothing (B-040)")
	}
}

// The named target, which is what the ADR ordered removed.
func TestCheck_NamedTargetIsForbidden(t *testing.T) {
	body := cleanTaskfile + `
  k3d:secrets:bootstrap-in-core:
    cmds:
      - echo hi
`
	out, err := violationOf(Check(withTaskfile(t, body)))
	if !errors.Is(err, ErrForbiddenShortcut) {
		t.Fatalf("err = %v; want ErrForbiddenShortcut", err)
	}
	if len(out.Violations) != 1 {
		t.Fatalf("violations = %v; want exactly the target", out.Violations)
	}
	if !strings.Contains(out.Violations[0], "k3d.yml:") {
		t.Errorf("violation carries no file:line: %q — a gate that cannot say WHERE forces a hunt", out.Violations[0])
	}
}

// The whole reason the check is effect-based: banning one name would be evaded
// by typing another. This is the test that would have caught a rename.
func TestCheck_RenamedTargetWithTheSameEffectIsForbidden(t *testing.T) {
	body := cleanTaskfile + `
  k3d:secrets:bootstrap-single-cluster:
    cmds:
      - helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e vault-local --kube-context k3d-loop-runtime-core --selector name=openbao sync
`
	out, err := violationOf(Check(withTaskfile(t, body)))
	if !errors.Is(err, ErrForbiddenShortcut) {
		t.Fatalf("err = %v; a renamed target with the SAME effect must still fail. violations=%v", err, out.Violations)
	}
	// "core context", not "mgmt": the message now names the cluster by the name it has
	// had since the B-001 rename. The Go identifier keeps `mgmt` because `MGMT_CONTEXT`
	// is still an accepted variable name — the message is what the operator reads, and
	// it pointed at a cluster that no longer exists.
	if len(out.Violations) != 1 || !strings.Contains(out.Violations[0], "core context") {
		t.Errorf("violations = %v; want the effect-based finding", out.Violations)
	}
}

// A NON-secrets helmfile aimed at mgmt is normal and must not fail.
func TestCheck_MgmtContextOnANonSecretsHelmfileIsFine(t *testing.T) {
	body := cleanTaskfile + `
  k3d:core:bootstrap:
    cmds:
      - helmfile -f infra/helmfile/core.helmfile.yaml.gotmpl --kube-context k3d-loop-runtime-core sync
`
	if _, err := violationOf(Check(withTaskfile(t, body))); err != nil {
		t.Fatalf("err = %v; only the SECRETS plane is forbidden in mgmt", err)
	}
}

// A missing or empty taskfile must be loud. Reporting success for a file it
// could not read is how a gate silently stops guarding.
func TestCheck_UnreadableTaskfileIsAnError(t *testing.T) {
	if _, err := violationOf(Check(Config{RepoRoot: t.TempDir()})); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("err = %v; want ErrNothingScanned for a missing taskfile", err)
	}
	if _, err := violationOf(Check(withTaskfile(t, ""))); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("err = %v; want ErrNothingScanned for an empty taskfile", err)
	}
}

// Against the real tree: the task was removed by this slice, so it must pass —
// and must have actually read the file, not passed by reading nothing.
func TestCheck_RealTreeHasNoShortcut(t *testing.T) {
	root := qtestutil.Root(t)
	out, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("err = %v; violations=%v", err, out.Violations)
	}
	if out.LinesScanned < 50 {
		t.Errorf("LinesScanned = %d; taskfiles/k3d.yml is far longer — the gate did not read it",
			out.LinesScanned)
	}
}

// --- the six evasions an independent review measured on 2026-08-12 ----------
//
// All six returned err==nil against the first version of this gate. Four are
// closed here; the two that need dataflow are documented in check.go and
// covered by helmfile-context-parity instead. Fixtures beat prose: without
// these, "closed" would be a claim.

func TestCheck_EvasionsAreClosed(t *testing.T) {
	const secretsSync = "helmfile -f infra/helmfile/secrets.helmfile.yaml.gotmpl -e vault-local"
	cases := []struct{ name, body string }{
		{"equals form", cleanTaskfile + "\n  k3d:x:\n    cmds:\n      - " + secretsSync + " --kube-context=k3d-loop-runtime-core sync\n"},
		{"template var", cleanTaskfile + "\n  k3d:x:\n    cmds:\n      - " + secretsSync + " --kube-context {{.CORE_CONTEXT}} sync\n"},
		{"line continuation", cleanTaskfile + "\n  k3d:x:\n    cmds:\n      - " + secretsSync + " \\\n          --kube-context k3d-loop-runtime-core sync\n"},
		{"four-space target", cleanTaskfile + "\n    k3d:secrets:bootstrap-in-core:\n      cmds:\n        - echo hi\n"},
		{"trailing comment on target", cleanTaskfile + "\n  k3d:secrets:bootstrap-in-core: # shortcut\n    cmds:\n      - echo hi\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := violationOf(Check(withTaskfile(t, tc.body)))
			if !errors.Is(err, ErrForbiddenShortcut) {
				t.Errorf("err = %v; this evasion must be caught. violations=%v", err, out.Violations)
			}
		})
	}
}

// The glob is the point: a forbidden target in another taskfile is invocable
// exactly like one in k3d.yml, because the root Taskfile flattens them.
func TestCheck_ScansEveryTaskfileNotJustK3d(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "taskfiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"k3d.yml":   cleanTaskfile,
		"infra.yml": "version: '3'\ntasks:\n  infra:secrets:bootstrap-in-core:\n    cmds:\n      - echo hi\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, "taskfiles", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := violationOf(Check(Config{RepoRoot: dir}))
	if !errors.Is(err, ErrForbiddenShortcut) {
		t.Fatalf("err = %v; a target in infra.yml is as invocable as one in k3d.yml", err)
	}
	if len(out.Violations) != 1 || !strings.Contains(out.Violations[0], "infra.yml") {
		t.Errorf("violations = %v; want the infra.yml finding", out.Violations)
	}
}
