// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapterports_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/adapterports"
)

func adaptersTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, "api", "internal", "x", "xadapter", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// Every adapter declares, at compile time, which port it satisfies.
//
// B-058, measured 2026-08-18: the package is called `adapters` and its admission criterion
// was "it is an adapter" — which refuses nothing. Of the nine adapters, five NAMED the port
// in prose and only ONE verified it. Prose does not compile.
//
// The assertion is what lets the criterion say no: whoever cannot name the port it
// satisfies does not belong here.
func TestAdapterWithAssertionPasses(t *testing.T) {
	root := adaptersTree(t, map[string]string{
		"x_adapter.go": "package xadapter\n\ntype XAdapter struct{}\n\nvar _ foo.Port = (*XAdapter)(nil)\n",
	})
	out, err := violationOf(adapterports.Check(adapterports.Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("adapter with an assertion should pass; got %v", err)
	}
	if out.Scanned != 1 {
		t.Errorf("expected 1 scanned, got %d", out.Scanned)
	}
}

// An adapter without an assertion is the case the old criterion did not refuse.
func TestAdapterWithoutAssertionIsRefused(t *testing.T) {
	root := adaptersTree(t, map[string]string{
		"y_adapter.go": "package xadapter\n\n// YAdapter adapts something.\ntype YAdapter struct{}\n",
	})
	out, err := violationOf(adapterports.Check(adapterports.Config{RepoRoot: root}))
	if !errors.Is(err, adapterports.ErrNoPortDeclared) {
		t.Fatalf("expected ErrNoPortDeclared, got %v", err)
	}
	if len(out.Missing) != 1 || out.Missing[0] != "api/internal/x/xadapter/y_adapter.go" {
		t.Errorf("the finding must name the file: %v", out.Missing)
	}
}

// A USAGE assertion (`var _ = pkg.Type{}`) is not a port declaration — it does not claim
// the adapter satisfies any interface. Confusing the two would let the defect through.
func TestUsageAssertionIsNotAPortDeclaration(t *testing.T) {
	root := adaptersTree(t, map[string]string{
		"z_adapter.go": "package xadapter\n\ntype ZAdapter struct{}\n\nvar _ = db.SomeType{}\n",
	})
	if _, err := violationOf(adapterports.Check(adapterports.Config{RepoRoot: root})); !errors.Is(err, adapterports.ErrNoPortDeclared) {
		t.Fatalf("a usage assertion does not declare a port; expected refusal, got %v", err)
	}
}

// A file without an exported type is not an adapter — there is no port to require of it,
// and it does not contaminate the count.
//
// The first version of this test passed ONLY the doc file and expected green. It was wrong,
// and the guard contradicted me: a package with zero adapters is indistinguishable from a
// dissolved package, and the other alternative in B-058's DoD is precisely to DISSOLVE
// `adapters`. Staying green there would be the guard outliving its own subject. The right
// contract is the one below: the doc file need not declare anything, and the adapter next
// to it must.
func TestFileWithoutExportedTypeIsNotRequiredToDeclare(t *testing.T) {
	root := adaptersTree(t, map[string]string{
		"adapters.go":  "// Package adapters ...\npackage adapters\n",
		"w_adapter.go": "package xadapter\n\ntype WAdapter struct{}\n\nvar _ foo.Port = (*WAdapter)(nil)\n",
	})
	out, err := violationOf(adapterports.Check(adapterports.Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("the doc file need not declare a port; got %v", err)
	}
	if out.Scanned != 1 {
		t.Errorf("only the adapter counts: expected 1 scanned, got %d", out.Scanned)
	}
}

// An absent package fails: a future dissolution (the other alternative in the DoD) must not
// leave the guard green over nothing.
func TestMissingPackageIsRefused(t *testing.T) {
	if _, err := violationOf(adapterports.Check(adapterports.Config{RepoRoot: t.TempDir()})); !errors.Is(err, adapterports.ErrNothingScanned) {
		t.Fatal("an absent package should fail")
	}
}

// The real population.
func TestRealTree(t *testing.T) {
	out, err := violationOf(adapterports.Check(adapterports.Config{RepoRoot: filepath.Join("..", "..", "..", "..", "..")}))
	if err != nil {
		t.Fatalf("real tree: %v (without a port: %v)", err, out.Missing)
	}
	if out.Scanned == 0 {
		t.Fatal("zero adapters scanned — green over nothing")
	}
	t.Logf("real population: %d adapter(s), all declaring the port they satisfy", out.Scanned)
}
