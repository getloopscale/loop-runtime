// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// A degradacao silenciosa do teto de custo tem de virar estado do sistema.
//
// B-033: quando o agregador nao sobe, `cost_counters` fica vazia e TODO teto libera — e o
// unico sinal era uma linha de log no boot de outro processo. Uma metrica com a razao como
// rotulo torna isso consultavel: `unmeasured` alto significa que a protecao nao esta
// avaliando nada, e `under_cap` zerado significa que ela nunca avaliou.
func TestCostCapDecisions_IsRegisteredWithReasonLabel(t *testing.T) {
	CostCapDecisionsTotal.WithLabelValues("build_minutes", "unmeasured").Inc()
	CostCapDecisionsTotal.WithLabelValues("build_minutes", "under_cap").Inc()

	n := testutil.CollectAndCount(CostCapDecisionsTotal)
	if n < 2 {
		t.Fatalf("esperava as duas series coletaveis, veio %d", n)
	}

	if v := testutil.ToFloat64(CostCapDecisionsTotal.WithLabelValues("build_minutes", "unmeasured")); v < 1 {
		t.Errorf("a serie `unmeasured` nao registrou: %v", v)
	}
}

// O nome e os rotulos sao contrato de alerta: mudar em silencio quebra o painel de quem
// depende deles.
func TestCostCapDecisions_NameAndLabelsAreStable(t *testing.T) {
	desc := CostCapDecisionsTotal.WithLabelValues("x", "y").Desc().String()

	for _, esperado := range []string{"loopzero_cost_cap_decisions_total", "metric", "reason"} {
		if !strings.Contains(desc, esperado) {
			t.Errorf("descritor perdeu %q: %s", esperado, desc)
		}
	}
}
