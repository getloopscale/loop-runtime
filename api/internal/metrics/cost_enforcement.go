// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// CostCapDecisionsTotal conta cada decisão do teto de custo, rotulada pela RAZÃO.
//
// # Por que existe
//
// B-033, medido no app-dev em 2026-08-11: o agregador que preenche `cost_counters` estava
// desligado, a tabela tinha zero linhas, e o portão trata linha ausente como consumo zero.
// Resultado: **todo** teto liberava, as três métricas de custo ficavam sem limite no
// ambiente que serve o painel público, e o único sinal era uma linha de log no boot de
// outro processo — que ninguém lê depois do dia em que o processo subiu.
//
// # O que esta métrica torna consultável
//
// A distinção que o veredito sozinho não carrega:
//
//	reason="under_cap"    a proteção AVALIOU um contador e ele estava abaixo do teto
//	reason="unmeasured"   não havia contador — pode ser tenant novo, pode ser agregador morto
//	reason="store_error"  a leitura falhou e o portão liberou (fail-open deliberado)
//	reason="unlimited"    o plano não tem teto para esta métrica
//	reason="blocked"      o contador alcançou o teto
//
// `unmeasured` alto significa que a proteção não está avaliando nada. `under_cap` em zero
// ao longo de um período inteiro significa que ela **nunca** avaliou — que é exatamente o
// estado que o item mediu e que nada tornava visível.
//
// A métrica NÃO muda veredito nenhum: libera e bloqueia exatamente como antes. Ela muda o
// que o sistema consegue dizer sobre a própria proteção.
var CostCapDecisionsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "loopzero_cost_cap_decisions_total",
		Help: "Decisões do teto de custo por métrica e razão. reason=unmeasured indica que não havia contador para avaliar.",
	},
	[]string{"metric", "reason"},
)
