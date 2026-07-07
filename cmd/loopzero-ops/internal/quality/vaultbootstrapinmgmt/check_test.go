// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vaultbootstrapinmgmt

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
	if err := os.MkdirAll(filepath.Join(root, "taskfiles"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "taskfiles", "k3d.yml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

// canonicalTaskfile mimics a k3d.yml with the upstream + shortcut tasks
// mirroring each other symmetrically (4 helmfile syncs + 2 kubectl waits).
const canonicalTaskfile = `version: '3'
tasks:
  k3d:vault:bootstrap:
    desc: Bootstrap OpenBao on the loopzero-vault k3d cluster (canonical).
    cmds:
      - helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl --kube-context k3d-loopzero-vault --selector name=cert-manager sync
      - helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl --kube-context k3d-loopzero-vault --selector name=cluster-issuer sync
      - kubectl --context k3d-loopzero-vault wait deployment/cert-manager --for=condition=Available --timeout=180s
      - helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl --kube-context k3d-loopzero-vault --selector name=openbao-cert sync
      - helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl --kube-context k3d-loopzero-vault --selector name=openbao sync
      - kubectl --context k3d-loopzero-vault wait pod -l app=openbao --for=condition=Ready --timeout=180s

  k3d:vault:bootstrap-in-mgmt:
    desc: |
      Shortcut for Option (c) of the iter 23 cross-cluster reachability Open Question
      (docs/operations/openbao-transit-cutover-runbook.md). Loses control-plane
      isolation for dev-public; prod cutover MUST use the canonical
      k3d:vault:bootstrap task instead.
    cmds:
      - test -z "$(kubectl config get-contexts -o name | grep '^k3d-loopzero-mgmt$')" && { echo "run task setup first"; exit 1; } || true
      - helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl --kube-context k3d-loopzero-mgmt --selector name=cert-manager sync
      - helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl --kube-context k3d-loopzero-mgmt --selector name=cluster-issuer sync
      - kubectl --context k3d-loopzero-mgmt wait deployment/cert-manager --for=condition=Available --timeout=180s
      - helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl --kube-context k3d-loopzero-mgmt --selector name=openbao-cert sync
      - helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl --kube-context k3d-loopzero-mgmt --selector name=openbao sync
      - kubectl --context k3d-loopzero-mgmt wait pod -l app=openbao --for=condition=Ready --timeout=180s

  k3d:vault:status-in-mgmt:
    desc: Status of OpenBao in mgmt cluster.
    cmds:
      - kubectl --context k3d-loopzero-mgmt -n openbao exec -it openbao-0 -- bao status
`

func TestCheck_CanonicalPasses(t *testing.T) {
	root := writeFixture(t, canonicalTaskfile)
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected PASS, got err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_TaskfileMissingFails(t *testing.T) {
	root := t.TempDir()
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
}

func TestCheck_DriftFailsWhenSelectorMissing(t *testing.T) {
	// Strip the openbao-cert sync from the SHORTCUT only → count drift.
	body := strings.Replace(canonicalTaskfile, "--selector name=openbao-cert sync\n      - helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl --kube-context k3d-loopzero-mgmt", "--selector name=openbao sync\n      - helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl --kube-context k3d-loopzero-mgmt", 1)
	root := writeFixture(t, body)
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for helmfile count drift, got %v", err)
	}
}

func TestCheck_ContextLeakageFails(t *testing.T) {
	// Leak k3d-loopzero-vault into the shortcut body.
	body := strings.Replace(canonicalTaskfile,
		"kubectl --context k3d-loopzero-mgmt wait pod -l app=openbao",
		"kubectl --context k3d-loopzero-vault wait pod -l app=openbao",
		1) // only in shortcut (the LAST kubectl wait line is in shortcut)
	root := writeFixture(t, body)
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for k3d-loopzero-vault leaked into shortcut, got %v", err)
	}
}

func TestCheck_PreflightMissingTaskSetupFails(t *testing.T) {
	body := strings.Replace(canonicalTaskfile, `run task setup first`, `run task foo first`, 1)
	root := writeFixture(t, body)
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when pre-flight does not reference 'task setup', got %v", err)
	}
}

func TestCheck_CoAuthoredByForbidden(t *testing.T) {
	body := strings.Replace(canonicalTaskfile, "prod cutover MUST use", "prod cutover MUST use\n      # Co-Authored-By: someone", 1)
	root := writeFixture(t, body)
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when Co-Authored-By trailer present, got %v", err)
	}
}

func TestCheck_RealRepoTaskfilePasses(t *testing.T) {
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(abs, "taskfiles", "k3d.yml")); statErr == nil {
			outcome, checkErr := Check(Config{RepoRoot: abs})
			if checkErr != nil {
				t.Fatalf("real k3d.yml must satisfy invariant. err=%v failures=%v", checkErr, outcome.Failures)
			}
			return
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("could not locate taskfiles/k3d.yml within 8 parent dirs; skipping real-repo assertion")
}
