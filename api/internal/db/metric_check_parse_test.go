// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"sort"
	"strings"
	"testing"
)

// Fixtures reduzidas das migrations reais. Cada uma existe porque UMA delas quebrava o parser
// anterior (#241) — nao sao variacoes hipoteticas.
const (
	// 000017 — a coluna nasce inline no CREATE TABLE.
	fxCreateCostCounters = `
CREATE TABLE IF NOT EXISTS cost_counters (
    tenant_id TEXT NOT NULL,
    plan_tier TEXT NOT NULL CHECK (plan_tier IN ('starter', 'pro')),
    metric TEXT NOT NULL CHECK (metric IN ('egress_bytes', 'docr_pulls', 'build_minutes'))
);`

	// 000046 — o CHECK e alargado por DROP/ADD CONSTRAINT.
	fxWidenCostCounters = `
ALTER TABLE cost_counters DROP CONSTRAINT cost_counters_metric_check;
ALTER TABLE cost_counters ADD CONSTRAINT cost_counters_metric_check
    CHECK (metric IN ('egress_bytes', 'docr_pulls', 'build_minutes', 'cpu_core_seconds', 'mem_gb_seconds'));`

	// 000056 — OUTRA tabela ganha uma coluna `metric`, e ainda referencia a primeira por FK.
	// Esta e a fixture do defeito: vem DEPOIS, entao virava "a ultima ocorrencia".
	fxCreateLensFired = `
CREATE TABLE lens_fired_alerts (
    id            TEXT PRIMARY KEY,
    rule_id       TEXT NOT NULL REFERENCES lens_alert_rules(id) ON DELETE CASCADE,
    metric        TEXT NOT NULL CHECK (metric IN ('error_rate', 'cost', 'p95_latency'))
);`

	// 000059 — e depois ainda troca o CHECK dela por ALTER, tambem depois do cost_counters.
	fxRenameLensMetric = `
ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach'));`

	// 000032 — `metric IN (...)` que NAO e restricao. Se entrar, contamina o conjunto.
	fxUpdateWhereMetricIn = `
UPDATE plan_limits SET hard_limit = NULL
WHERE plan = 'enterprise' AND metric IN ('environments', 'projects');`

	// Comentario em prosa citando o CHECK antigo — presente de verdade nas migrations do lens.
	fxCommentMentionsCheck = `
-- CHECK (metric IN ('valor_antigo_so_no_comentario'))
ALTER TABLE cost_counters ADD CONSTRAINT noop_check CHECK (length(tenant_id) > 0);`
)

// O conjunto que o guard de drift precisa ler: o de cost_counters, e so ele — mesmo com quatro
// migrations POSTERIORES mexendo em `metric` de outras tabelas.
func TestMetricCheckSetFor_ScopesToTheRequestedTable(t *testing.T) {
	// Arrange — ordem cronologica real: cost_counters primeiro, lens depois.
	sqls := []string{
		fxCreateCostCounters,
		fxWidenCostCounters,
		fxUpdateWhereMetricIn,
		fxCreateLensFired,
		fxRenameLensMetric,
	}

	// Act
	got := metricCheckSetFor("cost_counters", sqls)

	// Assert
	want := []string{"build_minutes", "cpu_core_seconds", "docr_pulls", "egress_bytes", "mem_gb_seconds"}
	assertSet(t, got, want)

	// O vocabulario do lens NAO pode vazar: era exatamente isto que o guard reportava como drift.
	for _, leaked := range []string{"error_rate", "cost", "p95_latency", "monitor_breach", "environments"} {
		if got[leaked] {
			t.Errorf("valor %q de OUTRA tabela vazou para o conjunto de cost_counters", leaked)
		}
	}
}

// O mesmo parser lido do outro lado: pedir a tabela do lens devolve o vocabulario do lens, e nao o
// de custo. Sem esta direcao, um parser que sempre devolvesse a PRIMEIRA ocorrencia passaria no
// teste acima pelo motivo errado.
func TestMetricCheckSetFor_ReadsTheOtherTableToo(t *testing.T) {
	sqls := []string{fxCreateCostCounters, fxWidenCostCounters, fxRenameLensMetric}

	got := metricCheckSetFor("lens_alert_rules", sqls)

	assertSet(t, got, []string{"cost", "error_rate", "monitor_breach", "p95_latency"})
}

// `UPDATE ... WHERE metric IN (...)` nao restringe coluna alguma; se for lido como CHECK, o guard
// acusa drift inexistente.
func TestMetricCheckSetFor_IgnoresNonConstraintMetricIn(t *testing.T) {
	got := metricCheckSetFor("plan_limits", []string{fxUpdateWhereMetricIn})

	if got != nil {
		t.Errorf("UPDATE ... WHERE metric IN (...) foi lido como CHECK: %v", got)
	}
}

// Comentario e prosa, nao esquema. Editar um comentario nao pode mudar o conjunto lido.
func TestMetricCheckSetFor_IgnoresCommentedOutCheck(t *testing.T) {
	got := metricCheckSetFor("cost_counters", []string{fxCreateCostCounters, fxCommentMentionsCheck})

	if got["valor_antigo_so_no_comentario"] {
		t.Error("valor citado apenas em comentario entrou no conjunto")
	}
	assertSet(t, got, []string{"build_minutes", "docr_pulls", "egress_bytes"})
}

// Tabela que nao aparece em migration alguma: nil, nao um conjunto vazio silencioso — quem chama
// precisa conseguir distinguir "sem CHECK" de "CHECK vazio".
func TestMetricCheckSetFor_ReturnsNilWhenTableAbsent(t *testing.T) {
	if got := metricCheckSetFor("tabela_inexistente", []string{fxCreateCostCounters}); got != nil {
		t.Errorf("esperava nil para tabela ausente, veio %v", got)
	}
}

func assertSet(t *testing.T, got map[string]bool, want []string) {
	t.Helper()
	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sort.Strings(want)
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("conjunto lido = [%s]; esperado [%s]", strings.Join(keys, ","), strings.Join(want, ","))
	}
}
