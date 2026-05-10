// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// BillingStore abstracts subscription lookups for the billing HTTP handler
// (DIP). T5.2 of deep-review-2026-05-10-remediation-plan (find_arch_billing_raw_sql MEDIUM):
// the raw SQL previously lived inside `routes/billing.go::fetchPlan` and
// `fetchStripeCustomerID`. Extraction keeps the route handler free of
// `SELECT ... FROM subscriptions` strings and centralises the canonical
// schema knowledge in the db package.
type BillingStore interface {
	FetchPlan(ctx context.Context, tenantID string) (string, error)
	FetchStripeCustomerID(ctx context.Context, tenantID string) (string, error)
}

// BillingRow is the minimal pgx.Row-shaped interface BillingStore needs.
// Mirrors the routes-package interface so callers can wire either a real
// pool or a fake for unit tests.
type BillingRow interface {
	Scan(dest ...any) error
}

// BillingPool is the minimal interface BillingStore depends on. It is
// satisfied by *pgxpool.Pool, pgx.Conn, pgx.Tx — same shape as
// routes.BillingPool to make migration of HTTP handler call sites a
// type-narrow.
type BillingPool interface {
	QueryRow(ctx context.Context, sql string, args ...any) BillingRow
}

// PostgresBillingStore implements BillingStore against pgx.
type PostgresBillingStore struct {
	pool BillingPool
}

// NewPostgresBillingStore constructs a store bound to a pool.
func NewPostgresBillingStore(pool BillingPool) *PostgresBillingStore {
	return &PostgresBillingStore{pool: pool}
}

// FetchPlan returns the active subscription's plan column for a tenant.
// Returns empty string when no subscription row exists (caller defaults
// to starter).
func (s *PostgresBillingStore) FetchPlan(ctx context.Context, tenantID string) (string, error) {
	var plan string
	err := s.pool.QueryRow(ctx,
		`SELECT plan FROM subscriptions WHERE tenant_id = $1 LIMIT 1`,
		tenantID,
	).Scan(&plan)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return plan, nil
}

// FetchStripeCustomerID returns the tenant's stripe_customer_id or empty
// string if missing. Used by the Portal handler to enforce EC-8 (free
// tier without Stripe customer → 400 NO_STRIPE_CUSTOMER, not 500).
func (s *PostgresBillingStore) FetchStripeCustomerID(ctx context.Context, tenantID string) (string, error) {
	var id *string
	err := s.pool.QueryRow(ctx,
		`SELECT stripe_customer_id FROM subscriptions WHERE tenant_id = $1 LIMIT 1`,
		tenantID,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if id == nil {
		return "", nil
	}
	return *id, nil
}
