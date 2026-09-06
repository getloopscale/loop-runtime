// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"errors"
	"testing"
	"time"
)

// O portao precisa dizer POR QUE liberou.
//
// B-033, medido no app-dev em 2026-08-11: o agregador que preenche `cost_counters` esta
// desligado (`LOOPZERO_PROMETHEUS_URL` ausente do Deployment), a tabela tem **0 linhas**, e o
// contrato de `Check` trata linha ausente como consumo zero — entao **todo** teto de custo
// libera, e nada distingue isso de "o tenant realmente nao consumiu".
//
// As tres metricas de custo — minutos de build, egresso, puxadas de registro — ficam sem
// teto no ambiente que serve o painel publico, e o unico sinal disso e uma linha de log no
// boot de outro processo.
//
// Devolver a razao nao muda o veredito: libera igual. Muda o que o sistema consegue
// observar sobre a propria protecao.
func TestCheckReason_DistinguishesUnmeasuredFromUnderCap(t *testing.T) {
	ctx := context.Background()

	semLinha := newFakeCostStore()
	razão, err := NewCapChecker(semLinha).CheckReason(ctx, "tn-x", PlanStarter, MetricBuildMinutes)
	if err != nil {
		t.Fatalf("linha ausente deve liberar: %v", err)
	}
	if razão != AllowedUnmeasured {
		t.Errorf("linha ausente: razao %v, esperava AllowedUnmeasured", razão)
	}

	comLinha := newFakeCostStore()
	comLinha.put(CostCounter{TenantID: "tn-x", Metric: MetricBuildMinutes, Value: 1, PeriodStart: time.Now().Unix()})
	razão, err = NewCapChecker(comLinha).CheckReason(ctx, "tn-x", PlanStarter, MetricBuildMinutes)
	if err != nil {
		t.Fatalf("abaixo do teto deve liberar: %v", err)
	}
	if razão != AllowedUnderCap {
		t.Errorf("medido e abaixo do teto: razao %v, esperava AllowedUnderCap", razão)
	}
}

// Erro de store tambem libera — e tambem precisa ser distinguivel, porque e a segunda porta
// silenciosa: um agregador ruidoso nao deve barrar producao, mas o operador tem de saber
// que a protecao nao avaliou nada.
func TestCheckReason_StoreErrorIsItsOwnReason(t *testing.T) {
	quebrado := newFakeCostStore()
	quebrado.err = errors.New("pool esgotado")

	razão, err := NewCapChecker(quebrado).CheckReason(context.Background(), "tn-x", PlanStarter, MetricBuildMinutes)
	if err != nil {
		t.Fatalf("erro de store libera (fail-open): %v", err)
	}
	if razão != AllowedStoreError {
		t.Errorf("erro de store: razao %v, esperava AllowedStoreError", razão)
	}
}

// Estourar o teto continua bloqueando, e a razao nao mascara isso.
func TestCheckReason_ExceededStillBlocks(t *testing.T) {
	cheio := newFakeCostStore()
	cheio.put(CostCounter{TenantID: "tn-x", Metric: MetricBuildMinutes, Value: 1 << 30, PeriodStart: time.Now().Unix()})

	razão, err := NewCapChecker(cheio).CheckReason(context.Background(), "tn-x", PlanStarter, MetricBuildMinutes)
	var ex *ExceededError
	if !errors.As(err, &ex) {
		t.Fatalf("acima do teto deve bloquear, veio %v (razao %v)", err, razão)
	}
	if razão != Blocked {
		t.Errorf("bloqueado: razao %v, esperava Blocked", razão)
	}
}

// `Check` continua valendo com o mesmo contrato: quem so quer saber se pode nao precisa
// aprender a razao.
func TestCheck_KeepsItsContract(t *testing.T) {
	if err := NewCapChecker(newFakeCostStore()).Check(context.Background(), "tn-x", PlanStarter, MetricBuildMinutes); err != nil {
		t.Errorf("Check deveria liberar com linha ausente, veio %v", err)
	}
}
