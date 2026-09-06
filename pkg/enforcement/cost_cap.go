// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"

	"github.com/getloopscale/loopzero-contracts/plan"
)

// T1.9 of plans/2026-05-12-launch-readiness-waves-plan.md (v1.4).
//
// Per-tenant cost cap enforcement. Distinct from PlanLimiter (which gates
// rate-style limits like builds-per-month and concurrent deploys): CostCap
// gates *quantity* metrics (bytes egressed, registry pulls, build minutes)
// against per-plan caps. Both share the HTTP 402 "Payment Required"
// surfacing pattern.
//
// M1.2 (plan m1-decouple-in-place D2/D4, v1.2 amendment): the VALUE VOCABULARY
// (CostMetric, Plan, CostCounter, CapFor, Unlimited, ExceededError,
// ErrCostCapExceeded) moved to pkg/plan — the contracts-bound surface. All
// EXTERNAL consumers import pkg/plan directly (Phase 3). The aliases below are
// retained as enforcement-INTERNAL shorthand for this package's own cap-logic
// (CostStore, CapChecker — which stay here per ISP); they are no longer a
// re-export path for external code. A future cleanup may inline plan.X when
// enforcement's internals/tests are next touched.

// --- enforcement-internal vocabulary aliases (single source of truth: pkg/plan) ---

// CostMetric identifies the kind of cost being tracked. Alias of plan.CostMetric.
type CostMetric = plan.CostMetric

// Canonical cost metrics (re-exported).
const (
	MetricEgressBytes  = plan.MetricEgressBytes
	MetricDOCRPulls    = plan.MetricDOCRPulls
	MetricBuildMinutes = plan.MetricBuildMinutes
	// Compute metrics (observe-only; plan metering-compute-loop T1.1).
	MetricCPUCoreSeconds = plan.MetricCPUCoreSeconds
	MetricMemGBSeconds   = plan.MetricMemGBSeconds
)

// Plan identifies a subscription tier. Alias of plan.Plan.
type Plan = plan.Plan

// Canonical plan tiers (re-exported).
const (
	PlanStarter    = plan.PlanStarter
	PlanPro        = plan.PlanPro
	PlanTeam       = plan.PlanTeam
	PlanEnterprise = plan.PlanEnterprise
)

// Unlimited is the sentinel for "no cap" (re-exported).
const Unlimited = plan.Unlimited

// CapFor returns the cap for (plan, metric) (re-exported).
var CapFor = plan.CapFor

// CostCounter is the read-side projection (alias of plan.CostCounter).
type CostCounter = plan.CostCounter

// ErrCostCapExceeded is the sentinel (re-exported).
var ErrCostCapExceeded = plan.ErrCostCapExceeded

// ExceededError carries HTTP 402 context (alias of plan.ExceededError) — a
// TRUE alias so errors.As(err, &enforcement.ExceededError{}) keeps matching.
type ExceededError = plan.ExceededError

// --- cap-enforcement logic (stays engine-side, ISP) ---

// CostStore is the DIP interface enforcement uses to read counters.
// Production implementation lives in api/internal/db (or services); tests
// implement in-memory.
type CostStore interface {
	// GetCounter returns the current counter value for (tenant, metric)
	// in the current period. Returns (counter, true, nil) on hit,
	// (zero, false, nil) on miss (no counter row yet — treat as 0),
	// (zero, false, err) on DB error.
	GetCounter(ctx context.Context, tenantID string, metric CostMetric) (CostCounter, bool, error)
}

// CapChecker is the high-level enforcement type. Consumes a CostStore and
// returns ExceededError when a counter has reached its cap.
type CapChecker struct {
	store CostStore
}

// NewCapChecker constructs a CapChecker bound to a store.
func NewCapChecker(store CostStore) *CapChecker {
	return &CapChecker{store: store}
}

// Check verifies that the tenant is under cap for (metric). Returns nil
// when allowed; *ExceededError when blocked.
//
// Semantics:
//   - Counter missing (no row yet) → ALLOW (current value 0).
//   - Cap = Unlimited (Enterprise or unknown plan) → ALLOW.
//   - Counter value < cap → ALLOW.
//   - Counter value >= cap → BLOCK with ExceededError.
//
// Note the >= : reaching the cap blocks the NEXT mutation.
func (c *CapChecker) Check(ctx context.Context, tenantID string, p Plan, metric CostMetric) error {
	_, err := c.CheckReason(ctx, tenantID, p, metric)
	return err
}

// AllowReason diz POR QUE o portao decidiu como decidiu.
//
// # Por que existe
//
// B-033, medido no app-dev em 2026-08-11: o agregador que preenche `cost_counters` estava
// desligado, a tabela tinha zero linhas, e o contrato abaixo trata linha ausente como
// consumo zero. Resultado: **todo** teto de custo liberava, e nada distinguia isso de "o
// tenant realmente nao consumiu". As tres metricas de custo ficavam sem teto no ambiente
// que serve o painel publico, e o unico sinal era uma linha de log no boot de OUTRO
// processo.
//
// Devolver a razao NAO muda veredito nenhum — libera exatamente o mesmo. Muda o que o
// sistema consegue observar sobre a propria protecao: a degradacao silenciosa vira estado
// observavel, que e o que o item pedia.
//
// A razao para aqui, em `pkg/`: este pacote nao conhece Prometheus nem logger. Quem
// traduz a razao em metrica e a camada de middleware, que ja tem os dois.
type AllowReason int

const (
	// AllowedUnderCap — havia contador, e ele esta abaixo do teto. A unica razao em que a
	// protecao de fato avaliou alguma coisa.
	AllowedUnderCap AllowReason = iota
	// AllowedUnmeasured — nao havia linha de contador. Pode ser tenant novo sem consumo, ou
	// agregador que nunca rodou. O portao NAO distingue os dois, e por isso o chamador
	// precisa saber que caiu aqui.
	AllowedUnmeasured
	// AllowedUnlimited — o plano nao tem teto para esta metrica.
	AllowedUnlimited
	// AllowedStoreError — a leitura falhou e o portao liberou (fail-open deliberado: um
	// agregador ruidoso nao deve barrar producao). Segunda porta silenciosa.
	AllowedStoreError
	// Blocked — o contador alcancou o teto.
	Blocked
)

// String descreve a razao para log e rotulo de metrica.
func (r AllowReason) String() string {
	switch r {
	case AllowedUnderCap:
		return "under_cap"
	case AllowedUnmeasured:
		return "unmeasured"
	case AllowedUnlimited:
		return "unlimited"
	case AllowedStoreError:
		return "store_error"
	case Blocked:
		return "blocked"
	}
	return "unknown"
}

// CheckReason e o `Check` que tambem devolve a razao da decisao.
func (c *CapChecker) CheckReason(ctx context.Context, tenantID string, p Plan, metric CostMetric) (AllowReason, error) {
	counter, found, err := c.store.GetCounter(ctx, tenantID, metric)
	if err != nil {
		// Fail-open on store errors. A noisy aggregator should not block
		// production traffic. The middleware logs the error.
		return AllowedStoreError, nil
	}

	cap := CapFor(p, metric)
	if cap == Unlimited {
		return AllowedUnlimited, nil
	}

	// Missing counter row = 0 usage so far this period = allow.
	if !found {
		return AllowedUnmeasured, nil
	}

	if counter.Value >= cap {
		return Blocked, &ExceededError{
			TenantID: tenantID,
			Plan:     p,
			Metric:   metric,
			Cap:      cap,
			Current:  counter.Value,
			Period:   formatPeriod(counter.PeriodStart),
		}
	}

	return AllowedUnderCap, nil
}

// formatPeriod converts a unix-seconds period start into "YYYY-MM" UTC.
func formatPeriod(unixSec int64) string {
	return formatPeriodImpl(unixSec)
}
