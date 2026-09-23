// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func repoCom(t *testing.T, arquivos map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, corpo := range arquivos {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(corpo), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func resolvedorCom(t *testing.T, arquivos map[string]string) *GuardResolver {
	t.Helper()
	root := repoCom(t, arquivos)
	return NewGuardResolver(root, root, nil)
}

// Um gate que ninguém roda não guarda nada — a razão de resolver por `quality.yml`
// e não pela existência do pacote.
func TestGateResolveQuandoRodado(t *testing.T) {
	r := resolvedorCom(t, map[string]string{
		"taskfiles/quality.yml": "cmds:\n  - /tmp/loopzero-ops quality billing-secrets\n",
	})
	if err := r.Resolve(Guard{Kind: GuardGate, Target: "billing-secrets"}, "loopzero"); err != nil {
		t.Fatalf("gate rodado resolve: %v", err)
	}
}

func TestGateNaoRodadoReprova(t *testing.T) {
	r := resolvedorCom(t, map[string]string{
		"taskfiles/quality.yml": "cmds:\n  - /tmp/loopzero-ops quality outra-coisa\n",
	})
	err := r.Resolve(Guard{Kind: GuardGate, Target: "billing-secrets"}, "loopzero")
	if !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("gate registrado mas não rodado não guarda nada; err=%v", err)
	}
}

func TestTesteResolveQuandoOArquivoExiste(t *testing.T) {
	r := resolvedorCom(t, map[string]string{"a/b_test.go": "package a\n"})
	if err := r.Resolve(Guard{Kind: GuardTest, Target: "a/b_test.go"}, "loopzero"); err != nil {
		t.Fatalf("teste presente resolve: %v", err)
	}
}

func TestTesteApagadoReprova(t *testing.T) {
	r := resolvedorCom(t, map[string]string{"outro.go": "package a\n"})
	if err := r.Resolve(Guard{Kind: GuardTest, Target: "a/b_test.go"}, "loopzero"); !errors.Is(err, ErrGuardUnresolved) {
		t.Fatalf("teste apagado reprova; err=%v", err)
	}
}

func resolverWithDebt(t *testing.T, status string) *GuardResolver {
	t.Helper()
	root := repoCom(t, map[string]string{"x.md": "x"})
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
