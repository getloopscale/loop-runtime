// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak_test

import (
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/leak"
)

// Um 502 na borda nao diz QUEM caiu, e hoje o motor e o painel aparecem iguais.
//
// B-022, medido 2026-08-10: o caminho do motor atravessa o painel — `/engine/*` e proxiado
// por `loopzero-cloud` — entao quando o painel fica sem upstream, `/engine/*` devolve 502 pelo
// MESMO motivo que uma rota do painel devolveria. Quem le o relatorio nao consegue separar
// "o motor esta fora" de "o painel esta fora", e as duas exigem acoes diferentes.
//
// A separacao existe porque as rotas de produto do painel NAO atravessam o motor (medido em
// B-002): se uma delas responde e `/engine/*` nao, o painel esta de pe e o motor nao.
func TestClassifyUpstream(t *testing.T) {
	casos := []struct {
		nome     string
		engine   int
		painel   int
		esperado leak.UpstreamState
	}{
		{"os dois respondem", 401, 401, leak.UpstreamBothUp},
		{"motor fora, painel de pe", 502, 401, leak.UpstreamEngineDown},
		{"painel fora leva o motor junto", 502, 502, leak.UpstreamDashboardDown},
		{"painel fora com 503", 503, 503, leak.UpstreamDashboardDown},
		{"motor com 504 e painel servindo", 504, 302, leak.UpstreamEngineDown},
	}
	for _, c := range casos {
		if got := leak.ClassifyUpstream(c.engine, c.painel); got != c.esperado {
			t.Errorf("%s: engine=%d painel=%d -> %v, esperava %v", c.nome, c.engine, c.painel, got, c.esperado)
		}
	}
}

// O caso que nao se deve resolver por adivinhacao: o painel falha e o motor responde. Isso
// contradiz a topologia medida — `/engine/*` atravessa o painel —, entao inventar um
// veredito aqui seria afirmar o que a sonda nao sabe.
func TestClassifyUpstream_ContradictoryIsNotGuessed(t *testing.T) {
	if got := leak.ClassifyUpstream(401, 502); got != leak.UpstreamInconclusive {
		t.Errorf("motor responde e painel nao contradiz a topologia; esperava inconclusivo, veio %v", got)
	}
}

// Cada estado precisa dizer ao operador o que fazer, senao a distincao nao serve para nada.
func TestUpstreamStateExplainsTheAction(t *testing.T) {
	for _, st := range []leak.UpstreamState{
		leak.UpstreamBothUp, leak.UpstreamEngineDown,
		leak.UpstreamDashboardDown, leak.UpstreamInconclusive,
	} {
		if st.Explicacao() == "" {
			t.Errorf("%v nao explica nada ao operador", st)
		}
	}
}
