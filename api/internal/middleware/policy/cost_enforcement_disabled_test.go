// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package policy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	planpkg "github.com/getloopscale/loopzero-contracts/plan"
	"github.com/getloopscale/loopzero/api/internal/metrics"
)

// O teto DESLIGADO precisa aparecer como estado do sistema, e nao so' como uma linha
// de log no boot de outro processo (B-033, terceira bala — ela diz exatamente isso).
//
// `CostCapDecisionsTotal` ja' existia e ja' distinguia `unmeasured` — "o portao
// avaliou e nao havia contador". Mas ela e' incrementada DEPOIS do desvio de
// receptor nulo, entao o caso mais grave — **o portao nao existe** — nao emitia
// nada. Medido em 2026-08-19: um `nil` passava direto, em silencio, e o unico
// sinal era o log de boot que o proprio item recusa como suficiente.
//
// A diferenca entre os dois importa para quem esta' de plantao: `unmeasured` pode
// ser tenant novo; `disabled` e' a protecao ausente para TODO mundo.
func TestTetoDesligadoEmiteDecisaoEmVezDePassarEmSilencio(t *testing.T) {
	const metrica = planpkg.CostMetric("build_minutes")

	antes := testutil.ToFloat64(metrics.CostCapDecisionsTotal.WithLabelValues(string(metrica), "disabled"))

	var chegou bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { chegou = true })

	// Receptor nulo: e' o estado real medido no app-dev — o agregador nao subiu e o
	// enforcer nunca foi ligado.
	var e *CostEnforcer
	h := e.Middleware(metrica)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !chegou {
		t.Fatal("o desvio de receptor nulo deixou de ser pass-through — isso QUEBRA o boot " +
			"tolerante que ele existe para permitir; a mudanca pedida e' observar, nao bloquear")
	}
	depois := testutil.ToFloat64(metrics.CostCapDecisionsTotal.WithLabelValues(string(metrica), "disabled"))
	if depois <= antes {
		t.Errorf("o teto desligado passou sem emitir decisao (antes=%v depois=%v). "+
			"Enquanto for assim, 'a protecao nao existe' e' indistinguivel de 'nao houve requisicao', "+
			"e o unico sinal e' um log de boot que ninguem le' depois do dia em que o processo subiu.",
			antes, depois)
	}
}
