// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"strings"
	"testing"
)

// A dica emitida quando o banco avancou e o patch no CR falhou precisa dizer que
// o patch direto e' NEGADO pela politica de admissao. A versao anterior oferecia
// o `kubectl patch` seco, e desde 2026-08-13 a `loopzero-tenant-cr-shape` nega
// exatamente essa mudanca — quem seguisse a dica com kubeconfig sem privilegio
// recebia um `Denied` sem entender por que (B-097).
func TestDicaDeConsertoAvisaQueOPatchDiretoENegado(t *testing.T) {
	d := dicaDeConserto("loopzero-system", "tn-abc", "cell-1")

	for _, exigido := range []string{
		"NEGADO",                 // o fato central
		"loopzero-tenant-cr-shape",   // qual politica
		"system:masters",         // quem esta' isento
		"kubectl -n loopzero-system", // o comando segue citado, para quem E' isento
		"tn-abc",
		"cell-1",
	} {
		if !strings.Contains(d, exigido) {
			t.Errorf("a dica nao menciona %q:\n%s", exigido, d)
		}
	}
}

// O conselho obvio e errado: esta funcao E' o caminho do `loopzero admin tenants pin`,
// e foi ele que acabou de falhar. Mandar usa-lo seria mandar repetir o que nao
// funcionou sem dizer o que mudou.
func TestDicaDeConsertoNaoMandaUsarOComandoQueAcabouDeFalhar(t *testing.T) {
	d := dicaDeConserto("loopzero-system", "tn-abc", "cell-1")
	if strings.Contains(d, "loopzero admin tenants pin") {
		t.Errorf("a dica manda usar o comando cujo proprio caminho acabou de falhar:\n%s", d)
	}
	if !strings.Contains(d, "Re-execute o pin") {
		t.Errorf("a dica nao diz o que de fato destrava — re-executar para que o patch venha da identidade isenta:\n%s", d)
	}
}
