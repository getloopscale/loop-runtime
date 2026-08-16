// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package surfaces

import (
	"strings"
	"testing"
)

func TestRegistry_SuperficieDesligadaCarregaARazao(t *testing.T) {
	r := NewRegistry()
	if err := r.Record("github_webhook", false, "GITHUB_WEBHOOK_SECRET ausente"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := r.Record("billing", true, ""); err != nil {
		t.Fatalf("Record: %v", err)
	}

	off := r.Disabled()
	if len(off) != 1 {
		t.Fatalf("Disabled() = %d superfícies, esperado 1: %+v", len(off), off)
	}
	if off[0].Name != "github_webhook" {
		t.Errorf("nome = %q, esperado github_webhook", off[0].Name)
	}
	// A razão é o que torna a métrica acionável: quem vê o zero precisa saber
	// o que provisionar, sem abrir o log do arranque.
	if !strings.Contains(off[0].Reason, "GITHUB_WEBHOOK_SECRET") {
		t.Errorf("razão = %q, esperado nomear a variável ausente", off[0].Reason)
	}
}

func TestRegistry_RecusaDesligadaSemRazao(t *testing.T) {
	r := NewRegistry()
	err := r.Record("github_webhook", false, "   ")
	if err == nil {
		t.Fatal("desligar sem razão foi aceito — uma superfície desligada sem razão " +
			"produz um zero que ninguém sabe consertar")
	}
	if !strings.Contains(err.Error(), "github_webhook") {
		t.Errorf("erro não nomeia a superfície: %v", err)
	}
}

func TestRegistry_RecusaNomeDuplicado(t *testing.T) {
	r := NewRegistry()
	if err := r.Record("github_webhook", true, ""); err != nil {
		t.Fatalf("primeiro Record: %v", err)
	}
	err := r.Record("github_webhook", false, "outra coisa")
	if err == nil {
		t.Fatal("nome duplicado foi aceito — duas superfícies com o mesmo nome tornam " +
			"a série ambígua e o operador não consegue saber qual está zerada")
	}
}

func TestRegistry_OrdemDeterministica(t *testing.T) {
	// Sem ordem estável o resumo de arranque muda a cada boot e vira ruído no
	// diff de log — o oposto do que a observabilidade deveria comprar.
	r := NewRegistry()
	for _, n := range []string{"zeta", "alpha", "meio"} {
		if err := r.Record(n, false, "razão"); err != nil {
			t.Fatalf("Record(%s): %v", n, err)
		}
	}
	got := r.Disabled()
	want := []string{"alpha", "meio", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("Disabled() = %d, esperado %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Errorf("posição %d = %q, esperado %q", i, got[i].Name, want[i])
		}
	}
}

func TestRegistry_AllDevolveLigadasEDesligadas(t *testing.T) {
	// O publicador precisa das duas: uma superfície LIGADA tem de emitir 1, e
	// não simplesmente ausência de série. Ausência e zero são indistinguíveis
	// para quem alerta, e essa ambiguidade é o defeito que este item ataca.
	r := NewRegistry()
	_ = r.Record("ligada", true, "")
	_ = r.Record("desligada", false, "razão")

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("All() = %d, esperado 2", len(all))
	}
	byName := map[string]bool{}
	for _, s := range all {
		byName[s.Name] = s.Enabled
	}
	if !byName["ligada"] || byName["desligada"] {
		t.Errorf("estados invertidos ou ausentes: %+v", byName)
	}
}

func TestRegistry_NomeVazioERecusado(t *testing.T) {
	r := NewRegistry()
	if err := r.Record("  ", true, ""); err == nil {
		t.Fatal("nome vazio foi aceito — viraria uma série sem rótulo utilizável")
	}
}
