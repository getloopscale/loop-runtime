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

// A tenant with no row in `subscriptions` is the NORMAL state of a tenant that
// just signed up — not a platform fault.
//
// Measured 2026-08-18 (B-140): `POST /api/v1/projects` answered
// `503 PLATFORM_DEGRADED — plan tier resolution failed`, because
// `QueryRow().Scan()` returned `sql.ErrNoRows` and `ResolvePlanTier` treats
// every error as fatal. The onboarding path `ResolvePlanTier` documents
// (`raw == ""` → Starter without an error) was unreachable.
func TestGetTenantPlan_NoSubscriptionIsNotAnError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT plan FROM subscriptions").
		WithArgs("tn-new").
		WillReturnError(sql.ErrNoRows)

	s := &sqlLimitsStore{db: db}
	plan, err := s.GetTenantPlan(context.Background(), "tn-new")
	if err != nil {
		t.Fatalf("a missing row is a new tenant, not an error: %v", err)
	}
	if plan != "" {
		t.Fatalf("want the empty plan (ResolvePlanTier maps it to Starter), got %q", plan)
	}
}

// The boundary: a REAL database error stays an error. Without this, the fix
// above would turn a database outage into "new tenant".
func TestGetTenantPlan_DatabaseErrorStaysAnError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	boom := errors.New("connection refused")
	mock.ExpectQuery("SELECT plan FROM subscriptions").WithArgs("tn-x").WillReturnError(boom)

	s := &sqlLimitsStore{db: db}
	if _, err := s.GetTenantPlan(context.Background(), "tn-x"); err == nil {
		t.Fatal("a database outage is an error; treating it as a new tenant would hide it")
	}
}
