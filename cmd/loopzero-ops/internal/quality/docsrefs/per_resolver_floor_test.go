// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package docsrefs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// RED (B-013, orthogonal-seat return): the ErrNoReferencesEnumerated floor is
// computed over the AGGREGATE Checked counter (check.go:427 increments one
// field inside the loop over every resolver; check.go:450 tests that single
// total). So a resolver whose collect() returns nothing is invisible as long as
// ANY other resolver enumerated something.
//
// Constructed with the two resolvers that exist today: CLAUDE.md carries one
// rule reference that RESOLVES, and the tree has no Go file at all, so
// adrResolver.collect walks its six roots and returns zero. If the floor were
// per-resolver this must fail; with the aggregate floor it passes green.
// RED POR DESENHO ate' B-074 ser implementado (o item cita B-013 como antecedente).
//
// Mede o defeito: `docsrefs` conta vacuidade num unico agregado, entao um resolver que
// enumera ZERO referencias e' absorvido pelos que enumeram alguma — medido aqui,
// `Checked=1` inteiro vindo de `ruleResolver` enquanto `adrResolver` enumerou 0, e o
// piso agregado passa.
//
// SKIP com o item e a medicao na mensagem, pelo mesmo argumento de B-001 e B-048: um
// teste permanentemente vermelho e' ignorado numa semana e leva a suite junto
// (`rules/testing.md`). `LOOPZERO_RUN_B074_RED=1` o arma.
func skipUnlessRedB074(t *testing.T) {
	t.Helper()
	if os.Getenv("LOOPZERO_RUN_B074_RED") == "" {
		t.Skip("DEBITO CONHECIDO B-074: o contador de vacuidade de `docsrefs` e' um agregado " +
			"unico, entao um resolver que enumera zero referencias fica invisivel atras dos " +
			"que enumeram alguma. Rode com LOOPZERO_RUN_B074_RED=1 para ver a medicao.")
	}
}

func TestCheck_AResolverThatEnumeratesNothingIsInvisible(t *testing.T) {
	skipUnlessRedB074(t)
	root := writeTree(t, map[string]string{
		"CLAUDE.md":                "Read `.claude/rules/present.md`.\n",
		".claude/rules/present.md": "x\n",
		"docs/d.md":                "no ADR here\n",
		"docsrefs-allowlist.txt":   "",
	})
	out, err := Check(Config{RepoRoot: root, AllowlistPath: filepath.Join(root, "docsrefs-allowlist.txt")})

	// Prove the premise: adrResolver really did enumerate zero.
	adr, cerr := adrResolver{}.collect(root)
	if cerr != nil {
		t.Fatalf("collect: %v", cerr)
	}
	if len(adr) != 0 {
		t.Fatalf("premise broken: adrResolver found %d refs, expected 0", len(adr))
	}

	t.Logf("aggregate Checked=%d (all of it from ruleResolver); adrResolver enumerated 0", out.Checked)

	if !errors.Is(err, ErrNoReferencesEnumerated) {
		t.Errorf("a resolver that enumerated ZERO references was absorbed by the aggregate floor: "+
			"Checked=%d, err=%v — the gate vouches for a kind it never measured", out.Checked, err)
	}
}
