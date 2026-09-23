// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"testing"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
)

// O nome do passo de Dockerfile foi um PONTO DE VARIACAO de verdade: o caminho
// "traga seu Dockerfile" emitia `dockerfile-lint` enquanto as estrategias de
// linguagem emitiam `generate-dockerfile`. Esse caminho foi removido em T2.2
// (ver `user_dockerfile_strategy_test.go`, que assere a AUSENCIA do passo), e a
// variacao colapsou sem que nada registrasse.
//
// Este teste e' o registro. Enquanto ele passa, o nome nao varia e a constante e'
// legitima. Se alguem reintroduzir uma estrategia que emita outro nome, ele
// reprova aqui — e o recado esta' na mensagem: o ponto de variacao precisa
// VOLTAR, e nao ser contornado com um literal no orquestrador.
//
// A versao anterior mantinha a variacao construindo um container DESCARTAVEL so'
// para ler o nome dele (B-068). Uma construcao descartada nao diz a ninguem se
// ainda ha' variacao; este teste diz.
func TestTodaEstrategiaEmiteOMesmoNomeDePassoDeDockerfile(t *testing.T) {
	if len(runtimeRegistry) == 0 {
		t.Fatal("registry vazio: um teste que nao percorre estrategia nenhuma passa por vacuidade")
	}

	sonda := []deploycontracts.AppSpec{{Name: "probe", Path: "."}}

	// O fallback entra junto de proposito: ele tambem emite um passo, e uma
	// divergencia nele produziria o mesmo defeito no DAG.
	estrategias := map[string]LanguageStrategy{"<unsupported>": &unsupportedStrategy{runtime: "nenhum"}}
	for runtime, d := range runtimeRegistry {
		estrategias[runtime] = d.Strategy
	}
	if len(estrategias) < 3 {
		t.Fatalf("apenas %d estrategia(s) varrida(s); a arvore real tinha 3 em 2026-08-18 "+
			"(node, python e o fallback) — a varredura encolheu", len(estrategias))
	}

	for nome, s := range estrategias {
		c := s.DockerfileStep(sonda, 1)
		if c == nil {
			t.Errorf("estrategia %s nao emitiu passo de Dockerfile", nome)
			continue
		}
		if c.Name != dockerfileStepTemplate {
			t.Errorf("estrategia %s emite o passo %q, e a constante diz %q.\n\n"+
				"O nome voltou a VARIAR entre estrategias. Restaure o ponto de variacao — "+
				"o orquestrador precisa perguntar a estrategia qual passo ela emite — em vez "+
				"de fixar um literal no DAG.", nome, c.Name, dockerfileStepTemplate)
		}
	}
}
