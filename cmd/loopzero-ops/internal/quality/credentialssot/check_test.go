// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package credentialssot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// buildFixture creates the minimum repo layout satisfying the invariant.
func buildFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// One canonical generator producing 2 literal Secret names.
	writeFile(t, root, "infra/helmfile/scripts/generate-secrets.sh",
		"#!/bin/bash\n"+
			"kubectl create secret generic loopzero-foo-secret -n foo\n"+
			"kubectl create secret docker-registry loopzero-bar-registry -n bar\n"+
			// This one uses a variable — MUST be skipped by the discovery regex.
			"kubectl create secret generic \"${DYN_NAME}\" -n dynamic\n",
	)
	// SoT doc listing both discovered secrets + all 4 env vars + both sections.
	writeFile(t, root, sotDoc,
		"# Credentials Required\n\n"+
			"## Bootstrap Secrets\n\n"+
			"| Name | Provider |\n"+
			"|---|---|\n"+
			"| loopzero-foo-secret | Vault |\n"+
			"| loopzero-bar-registry | Docker Registry |\n\n"+
			"## Drill Env Vars\n\n"+
			"| Env Var | Purpose |\n"+
			"|---|---|\n"+
			"| DIGITALOCEAN_TOKEN | DO API |\n"+
			"| PULUMI_ACCESS_TOKEN | Pulumi Cloud |\n"+
			"| PULUMI_CONFIG_PASSPHRASE | Stack decryption |\n"+
			"| CLOUDFLARE_API_TOKEN | Cloudflare |\n\n"+
			"## Chart-hardcoded credentials\n\n"+
			"(none — being eliminated per T1.3)\n\n"+
			"## Rotation playbook\n\n"+
			"See the rotation runbook for each secret.\n",
	)
	return root
}

func TestCheck_CanonicalPasses(t *testing.T) {
	root := buildFixture(t)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS, got err=%v failures=%v", err, outcome.Failures)
	}
	if len(outcome.Discovered) != 2 {
		t.Fatalf("expected 2 discovered secrets, got %v", outcome.Discovered)
	}
}

func TestCheck_SotMissingFails(t *testing.T) {
	root := t.TempDir()
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrSotMissing) {
		t.Fatalf("expected ErrSotMissing, got %v", err)
	}
}

func TestCheck_UndocumentedSecretFails(t *testing.T) {
	root := buildFixture(t)
	// Add a new secret to the generator but NOT to the SoT.
	writeFile(t, root, "infra/helmfile/scripts/generate-runtime-secrets.sh",
		"#!/bin/bash\nkubectl create secret generic loopzero-orphan-runtime -n runtime\n",
	)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for undocumented secret, got %v", err)
	}
}

func TestCheck_MissingDrillEnvVarFails(t *testing.T) {
	root := buildFixture(t)
	// Rewrite SoT with a missing DIGITALOCEAN_TOKEN row.
	writeFile(t, root, sotDoc,
		"# Credentials Required\n\n"+
			"| Name | Provider |\n"+
			"|---|---|\n"+
			"| loopzero-foo-secret | Vault |\n"+
			"| loopzero-bar-registry | Docker |\n\n"+
			"| Env Var |\n"+
			"|---|\n"+
			"| PULUMI_ACCESS_TOKEN |\n"+
			"| PULUMI_CONFIG_PASSPHRASE |\n"+
			"| CLOUDFLARE_API_TOKEN |\n\n"+
			"## Chart-hardcoded credentials\n\n(none)\n\n"+
			"## Rotation playbook\n\n(runbook)\n",
	)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for missing DIGITALOCEAN_TOKEN, got %v", err)
	}
}

func TestCheck_MissingSectionFails(t *testing.T) {
	root := buildFixture(t)
	// Rewrite SoT without the Rotation playbook section.
	writeFile(t, root, sotDoc,
		"# Credentials Required\n\n"+
			"| Name | Provider |\n"+
			"|---|---|\n"+
			"| loopzero-foo-secret | Vault |\n"+
			"| loopzero-bar-registry | Docker |\n\n"+
			"| Env Var |\n"+
			"|---|\n"+
			"| DIGITALOCEAN_TOKEN |\n"+
			"| PULUMI_ACCESS_TOKEN |\n"+
			"| PULUMI_CONFIG_PASSPHRASE |\n"+
			"| CLOUDFLARE_API_TOKEN |\n\n"+
			"## Chart-hardcoded credentials\n\n(none)\n",
	)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for missing Rotation playbook, got %v", err)
	}
}

func TestCheck_VariableExpansionsSkipped(t *testing.T) {
	root := t.TempDir()
	// Only variable expansions in the generator — discovery should yield 0.
	writeFile(t, root, "infra/helmfile/scripts/generate-secrets.sh",
		"#!/bin/bash\n"+
			"kubectl create secret generic \"${VAR}\" -n foo\n"+
			"kubectl create secret generic \"$VAR2\" -n bar\n",
	)
	writeFile(t, root, sotDoc,
		"# CR\n\n| DIGITALOCEAN_TOKEN |\n| PULUMI_ACCESS_TOKEN |\n"+
			"| PULUMI_CONFIG_PASSPHRASE |\n| CLOUDFLARE_API_TOKEN |\n\n"+
			"## Chart-hardcoded credentials\n(none)\n\n## Rotation playbook\n(runbook)\n",
	)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when zero discovered, got %v (failures=%v)", err, outcome.Failures)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(abs, sotDoc)); statErr == nil {
			outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
			if checkErr != nil {
				t.Fatalf("real repo must satisfy invariant. err=%v failures=%v discovered=%v", checkErr, outcome.Failures, outcome.Discovered)
			}
			return
		}
		abs = filepath.Dir(abs)
	}
	// The message used to name docs/operations/credentials-required.md, a path the
	// 2026-09-11 docs consolidation folded away; sotDoc has been docs/operations.md
	// since. Derive it from the constant so it cannot drift again, and fail rather
	// than skip: the document is tracked, so not finding it is a broken checkout.
	t.Fatalf("could not locate %s within 8 parent directories", sotDoc)
}
