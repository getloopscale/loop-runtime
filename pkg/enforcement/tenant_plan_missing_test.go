// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// Um tenant sem linha em `subscriptions` é o estado NORMAL de quem acabou de entrar — não é
// degradação de plataforma.
//
// Medido em 2026-08-18 no droplet: `POST /api/v1/projects` devolvia
// `503 PLATFORM_DEGRADED — plan tier resolution failed for tn-e2etest`, porque
// `QueryRow().Scan()` devolve `sql.ErrNoRows` e `ResolvePlanTier` trata todo erro como fatal.
//
// E o código documentava o contrário em TRÊS lugares que não concordavam entre si: o contrato
// da interface pedia `("starter", nil)`, o doc do método dizia `("starter", error)`, e a
// implementação fazia `("", ErrNoRows)`. O caminho de onboarding que `ResolvePlanTier`
// documenta (`raw == ""` → Starter sem erro) era inalcançável.
func TestGetTenantPlanSemAssinaturaNaoEErro(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-novo").
		WillReturnError(sql.ErrNoRows)

	s := &sqlLimitsStore{db: db}
	plan, err := s.GetTenantPlan(context.Background(), "tn-novo")
	if err != nil {
		t.Fatalf("linha ausente não é erro — é tenant novo: %v", err)
	}
	if plan != "" {
		t.Fatalf("precisa devolver vazio, não %q — é o vazio que faz o OnFallbackStarter "+
			"disparar (limiter.go:259) E o ResolvePlanTier cair para Starter sem erro. "+
			"Devolver \"starter\" aqui silenciaria a métrica", plan)
	}
}

// A fronteira: um erro REAL de banco continua sendo erro. Sem isto, o conserto acima
// transformaria indisponibilidade de banco em "tenant novo".
func TestGetTenantPlanErroRealContinuaErro(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	boom := errors.New("connection refused")
	mock.ExpectQuery("SELECT plan FROM subscriptions").WithArgs("tn-x").WillReturnError(boom)

	s := &sqlLimitsStore{db: db}
	if _, err := s.GetTenantPlan(context.Background(), "tn-x"); err == nil {
		t.Fatal("banco fora do ar É erro, e tratá-lo como tenant novo esconderia indisponibilidade")
	}
}
