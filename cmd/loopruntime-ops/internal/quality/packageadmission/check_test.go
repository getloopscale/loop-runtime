// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package packageadmission_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/packageadmission"
)

// tree writes files under api/internal/k8s/<relative path> and returns the repo root.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, "api", "internal", "k8s", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// compliant is the shape the B-058 split produces: plumbing at the top, one package
// per responsibility underneath.
func compliant() map[string]string {
	return map[string]string{
		"typed_client.go":                    "package k8s\n\ntype Scheme struct{}\n\nfunc NewTypedClient() *Scheme { return nil }\n",
		"tenancy/tenant_cr_manager.go":       "package tenancy\n\ntype TenantCRManager struct{}\n",
		"workload/application_cr_manager.go": "package workload\n\ntype ApplicationCRManager struct{}\n",
		"secretsync/secret_k8s_manager.go":   "package secretsync\n\ntype SecretK8sManager struct{}\n",
	}
}

// The split shape passes: plumbing carries no domain vocabulary and every home is populated.
func TestPlumbingWithoutDomainVocabularyPasses(t *testing.T) {
	out, err := violationOf(packageadmission.Check(packageadmission.Config{RepoRoot: tree(t, compliant())}))
	if err != nil {
		t.Fatalf("the split shape should pass; got %v", err)
	}
	if out.Scanned != 1 {
		t.Errorf("expected 1 top-level file scanned, got %d", out.Scanned)
	}
	if len(out.Homes) != 3 {
		t.Errorf("expected 3 populated homes, got %v", out.Homes)
	}
}

// The defect itself: a file admitted because it uses client-go, named after a domain.
//
// This is the case "it uses client-go" could not refuse and "plumbing names no domain" can.
func TestDomainNamedFileInPlumbingIsRefused(t *testing.T) {
	files := compliant()
	files["tenant_evictor.go"] = "package k8s\n\ntype TenantK8sEvictor struct{}\n"
	out, err := violationOf(packageadmission.Check(packageadmission.Config{RepoRoot: tree(t, files)}))
	if !errors.Is(err, packageadmission.ErrForeignDomain) {
		t.Fatalf("expected ErrForeignDomain, got %v", err)
	}
	// Two findings from one file: the name on disk and the name it exports. Both are
	// how a domain gets read back out of a package, so both are reported.
	var byFile, byIdent bool
	for _, v := range out.Violations {
		if v.File != "tenant_evictor.go" {
			continue
		}
		if v.Where == "file name" {
			byFile = true
		}
		if v.Where == "TenantK8sEvictor" {
			byIdent = true
		}
	}
	if !byFile || !byIdent {
		t.Errorf("finding must name both the file and the exported identifier: %v", out.Violations)
	}
}

// A domain that hides behind a neutral file name is still refused, because the
// exported identifier carries it. Checking only file names would miss this.
func TestNeutralFileNameWithDomainExportIsRefused(t *testing.T) {
	files := compliant()
	files["helpers.go"] = "package k8s\n\nfunc EnsureLoopRuntimeApplication() {}\n"
	if _, err := violationOf(packageadmission.Check(packageadmission.Config{RepoRoot: tree(t, files)})); !errors.Is(err, packageadmission.ErrForeignDomain) {
		t.Fatalf("a neutral file name does not launder a domain export; got %v", err)
	}
}

// An unexported domain helper is NOT refused: it cannot be read from outside, so it
// does not make the package admit a responsibility. Refusing it would make the gate
// noisy without making the criterion sharper.
func TestUnexportedDomainHelperIsAllowed(t *testing.T) {
	files := compliant()
	files["resolve.go"] = "package k8s\n\nfunc resolveTenantEntry() {}\n"
	if _, err := violationOf(packageadmission.Check(packageadmission.Config{RepoRoot: tree(t, files)})); err != nil {
		t.Fatalf("an unexported helper is not an admission; got %v", err)
	}
}

// Refusing into nowhere is not a split. A home that empties out FAILS, so this gate
// cannot outlive the object it guards.
func TestEmptiedHomeIsRefused(t *testing.T) {
	files := compliant()
	delete(files, "workload/application_cr_manager.go")
	if _, err := violationOf(packageadmission.Check(packageadmission.Config{RepoRoot: tree(t, files)})); !errors.Is(err, packageadmission.ErrHomeMissing) {
		t.Fatalf("expected ErrHomeMissing, got %v", err)
	}
}

// A home reduced to a doc file with no exported type is empty for this purpose:
// the responsibility did not land there.
func TestHomeWithoutExportedTypeIsRefused(t *testing.T) {
	files := compliant()
	files["workload/application_cr_manager.go"] = "// Package workload ...\npackage workload\n"
	if _, err := violationOf(packageadmission.Check(packageadmission.Config{RepoRoot: tree(t, files)})); !errors.Is(err, packageadmission.ErrHomeMissing) {
		t.Fatalf("expected ErrHomeMissing, got %v", err)
	}
}

// Absent package FAILS. Green over a directory that vanished is the failure mode the
// sibling gate adapter-ports records.
func TestMissingPackageIsRefused(t *testing.T) {
	if _, err := violationOf(packageadmission.Check(packageadmission.Config{RepoRoot: t.TempDir()})); !errors.Is(err, packageadmission.ErrNothingScanned) {
		t.Fatal("an absent governed package must fail")
	}
}

// The real population.
func TestRealTree(t *testing.T) {
	out, err := violationOf(packageadmission.Check(packageadmission.Config{RepoRoot: filepath.Join("..", "..", "..", "..", "..")}))
	if err != nil {
		t.Fatalf("real tree: %v (violations: %v)", err, out.Violations)
	}
	if out.Scanned == 0 {
		t.Fatal("zero files scanned — green over nothing")
	}
	t.Logf("real population: %d plumbing file(s), homes %v", out.Scanned, out.Homes)
}
