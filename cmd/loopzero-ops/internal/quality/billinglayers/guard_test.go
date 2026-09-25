// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func repoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func resolverWith(t *testing.T, files map[string]string) *GuardResolver {
	t.Helper()
	root := repoWith(t, files)
	return NewGuardResolver(root, root, nil)
}

// A gate nobody runs guards nothing — the reason to resolve through `quality.yml`
// and not through the package existing.
func TestGateResolvesWhenRun(t *testing.T) {
	r := resolverWith(t, map[string]string{
		"taskfiles/quality.yml": "cmds:\n  - /tmp/loopzero-ops quality billing-secrets\n",
	})
	if err := r.Resolve(Guard{Kind: GuardGate, Target: "billing-secrets"}, "loopzero"); err != nil {
		t.Fatalf("a gate that is run resolves: %v", err)
	}
}

func TestGateNotRunFails(t *testing.T) {
	r := resolverWith(t, map[string]string{
		"taskfiles/quality.yml": "cmds:\n  - /tmp/loopzero-ops quality something-else\n",
	})
	err := r.Resolve(Guard{Kind: GuardGate, Target: "billing-secrets"}, "loopzero")
	if !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("a gate registered but not run guards nothing; err=%v", err)
	}
}

func TestTestResolvesWhenTheFileExists(t *testing.T) {
	r := resolverWith(t, map[string]string{"a/b_test.go": "package a\n"})
	if err := r.Resolve(Guard{Kind: GuardTest, Target: "a/b_test.go"}, "loopzero"); err != nil {
		t.Fatalf("a present test resolves: %v", err)
	}
}

func TestDeletedTestFails(t *testing.T) {
	r := resolverWith(t, map[string]string{"other.go": "package a\n"})
	if err := r.Resolve(Guard{Kind: GuardTest, Target: "a/b_test.go"}, "loopzero"); !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("a deleted test fails; err=%v", err)
	}
}

func resolverWithDebt(t *testing.T, status string) *GuardResolver {
	t.Helper()
	root := repoWith(t, map[string]string{"x.md": "x"})
	return NewGuardResolver(root, root, map[string]Debt{"B-033": {ID: "B-033", Status: status, Closes: "cost ceiling"}})
}

// `planned` and `approved` count as open: the ratchet exists to catch CLOSING
// without coverage, and a planned or approved item is not closed.
func TestDebtResolvesWhileTheItemIsOpen(t *testing.T) {
	for _, st := range []string{"raw", "triaged", "approved", "planned"} {
		if err := resolverWithDebt(t, st).Resolve(Guard{Kind: GuardOpen, Target: "B-033"}, "loopzero"); err != nil {
			t.Fatalf("debt with item %s resolves: %v", st, err)
		}
	}
}

// THE RATCHET: closing the debt without swapping the guard leaves the layer
// uncovered while the registry still claims somebody owns it.
func TestDebtWithAClosedItemFails(t *testing.T) {
	for _, st := range []string{"shipped", "killed"} {
		err := resolverWithDebt(t, st).Resolve(Guard{Kind: GuardOpen, Target: "B-033"}, "loopzero")
		if !errors.Is(err, ErrGuardUnresolved) {
			t.Fatalf("item %s is not open debt — the guard must become gate: or test:; err=%v", st, err)
		}
	}
}

func TestDebtNotDeclaredInTheRegistryFails(t *testing.T) {
	err := resolverWithDebt(t, "triaged").Resolve(Guard{Kind: GuardOpen, Target: "B-999"}, "loopzero")
	if !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("an undeclared debt owns nothing; err=%v", err)
	}
}
