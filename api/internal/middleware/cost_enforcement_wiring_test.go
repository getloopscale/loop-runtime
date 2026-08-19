// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/middleware"
	"github.com/getloopscale/loopzero/pkg/enforcement"
)

// A metrica precisa ser ALIMENTADA pelo caminho real, nao apenas existir.
//
// B-033. Uma metrica registrada e nunca incrementada e o padrao "implementado mas nao
// ligado" que o `CLAUDE.md` deste repositorio nomeia — e seria especialmente perverso aqui,
// porque o item existe justamente porque uma protecao estava instalada e inerte.
func TestCostEnforcer_FeedsTheDecisionMetric(t *testing.T) {
	const metrica = enforcement.MetricBuildMinutes

	antes := testutil.ToFloat64(
		metrics.CostCapDecisionsTotal.WithLabelValues(string(metrica), "unmeasured"))

	// Store vazio = nenhuma linha de contador = exatamente o estado medido no app-dev.
	enforcer := middleware.NewCostEnforcer(
		enforcement.NewCapChecker(storeVazio{}), planoFixo("starter"), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	req = req.WithContext(context.WithValue(req.Context(),
		middleware.UserContextKeyPublic(), &middleware.UserContext{TenantID: "tn-metrica"}))
	rec := httptest.NewRecorder()

	passou := false
	enforcer.Middleware(metrica)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		passou = true
	})).ServeHTTP(rec, req)

	if !passou {
		t.Fatal("sem contador o portao deve liberar — o veredito nao muda com esta fatia")
	}

	depois := testutil.ToFloat64(
		metrics.CostCapDecisionsTotal.WithLabelValues(string(metrica), "unmeasured"))
	if depois <= antes {
		t.Errorf("a serie `unmeasured` nao foi alimentada pelo middleware: %v -> %v", antes, depois)
	}
}

type storeVazio struct{}

func (storeVazio) GetCounter(context.Context, string, enforcement.CostMetric) (enforcement.CostCounter, bool, error) {
	return enforcement.CostCounter{}, false, nil
}

type planoFixo string

func (p planoFixo) GetTenantPlan(context.Context, string) (enforcement.Plan, error) {
	return enforcement.Plan(p), nil
}
