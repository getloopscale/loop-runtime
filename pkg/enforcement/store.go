// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// LimitsStore is the read-only interface PlanLimiter uses to look up a
// tenant's plan and the (hard, soft) limit pair for a given metric.
//
// Per ADR-0033 (DIP via LimitsStore interface): pkg/enforcement no longer
// hard-couples to *sql.DB at the type level. The default constructor
// NewPlanLimiter still accepts *sql.DB for backward compatibility, but
// callers that want to mock, test, or back the limiter with a different
// store (in-memory cache, OPA policy fetcher) can implement this
// interface and use NewPlanLimiterWithStore.
//
// Implementations MUST:
//
//   - Return ("", nil) when the subscription row is missing. CORRIGIDO
//     2026-08-18 (B-140): este contrato pedia ("starter", nil), o doc do
//     metodo dizia ("starter", error), e a implementacao fazia
//     ("", ErrNoRows) — tres afirmacoes divergentes sobre o mesmo caso, e
//     nenhuma delas era o que rodava. O erro chegava a `ResolvePlanTier`
//     como fatal e virava 503 na criacao do primeiro projeto de todo
//     tenant novo.
//     "" e nao "starter" porque `OnFallbackStarter` (metrica de "caiu para
//     starter") so dispara quando GetTenantPlan devolve erro OU plano
//     vazio; devolver "starter" silenciaria a contagem.
//
//   - Return (-1, _, nil) for hardLimit when the metric has no
//     enforced cap for the plan (NULL hard_limit row).
//
//   - Return (0, _, nil) for hardLimit when the (plan, metric) row is
//     missing — the limiter treats this as fail-closed (denies the
//     request) per ADR-0033 enumeration.
type LimitsStore interface {
	// GetTenantPlan returns the active plan name (e.g., "starter",
	// "pro") for the given tenant, or ("", nil) when no subscription row
	// exists — ver a nota do contrato acima (B-140).
	GetTenantPlan(ctx context.Context, tenantID string) (string, error)

	// GetLimits returns the (hard, soft) limits for the given (plan,
	// metric) pair. Soft <= 0 means no soft cap; hard < 0 means
	// unlimited; hard == 0 means missing row (fail closed).
	GetLimits(ctx context.Context, plan, metric string) (hardLimit, softLimit int, err error)
}

// sqlLimitsStore is the canonical SQL adapter used internally when
// callers pass *sql.DB to NewPlanLimiter. Externalized so tests and
// future backends can implement LimitsStore directly.
type sqlLimitsStore struct {
	db *sql.DB
}

// NewSQLLimitsStore wraps a *sql.DB so it satisfies the LimitsStore
// interface. Callers in the api/operators modules can use this when
// constructing a PlanLimiter explicitly.
func NewSQLLimitsStore(db *sql.DB) LimitsStore {
	return &sqlLimitsStore{db: db}
}

// GetTenantPlan implements LimitsStore.
func (s *sqlLimitsStore) GetTenantPlan(ctx context.Context, tenantID string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("nil sql limits store")
	}
	var plan string
	err := s.db.QueryRowContext(ctx,
		"SELECT plan FROM subscriptions WHERE tenant_id = $1 AND status IN ('active', 'trialing') LIMIT 1",
		tenantID,
	).Scan(&plan)
	// Linha ausente NAO e erro: e o estado normal de um tenant que acabou de entrar.
	//
	// Medido em 2026-08-18 (B-140): propagar `ErrNoRows` fazia `ResolvePlanTier` tratar a
	// condicao como fatal, e `POST /api/v1/projects` devolvia 503 PLATFORM_DEGRADED — a
	// PRIMEIRA acao da jornada central do produto, para todo tenant novo.
	//
	// Devolve "" e nao "starter", apesar de o contrato da interface acima pedir "starter",
	// e a razao foi medida: `limiter.go:259` dispara `OnFallbackStarter` quando
	// `err != nil || plan == ""`. Devolver "starter" silenciaria essa metrica — ninguem
	// contaria quantos tenants caem no fallback. Com "", os DOIS consumidores ficam certos:
	// `ResolvePlanTier` cai para Starter sem erro (o caminho que ele ja documentava como o
	// unico legitimo, e que era inalcancavel), e a metrica continua disparando.
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return plan, nil
}

// GetAccountPlanByTenant implements LimitsStore. M4: resolves the plan of the
// tenant's Account (one subscription per Account since M2 — UNIQUE(account_id)),
// joining through tenants.account_id (NOT NULL since M3 000044).
func (s *sqlLimitsStore) GetAccountPlanByTenant(ctx context.Context, tenantID string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("nil sql limits store")
	}
	var plan string
	err := s.db.QueryRowContext(ctx,
		`SELECT plan FROM subscriptions
		 WHERE account_id = (SELECT account_id FROM tenants WHERE id = $1)
		   AND status IN ('active', 'trialing') LIMIT 1`,
		tenantID,
	).Scan(&plan)
	if err != nil {
		return "", err
	}
	return plan, nil
}

// GetLimits implements LimitsStore.
func (s *sqlLimitsStore) GetLimits(ctx context.Context, plan, metric string) (int, int, error) {
	if s == nil || s.db == nil {
		return 0, 0, fmt.Errorf("nil sql limits store")
	}
	var hard sql.NullInt64
	var soft sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT hard_limit, soft_limit FROM plan_limits WHERE plan = $1 AND metric = $2",
		plan, metric,
	).Scan(&hard, &soft)
	if err != nil {
		return 0, 0, err
	}
	hardVal := -1
	if hard.Valid {
		hardVal = int(hard.Int64)
	}
	softVal := 0
	if soft.Valid {
		softVal = int(soft.Int64)
	}
	return hardVal, softVal, nil
}
