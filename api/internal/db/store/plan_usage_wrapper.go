// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// txBeginner opens the transaction a count runs in. *pgxpool.Pool satisfies it.
type txBeginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// PostgresPlanUsageStore measures the usage the plan-limit middleware gates:
// builds this month, permanent environments and seats. Each method has the
// shape of enforcement.UsageCounter, so the router passes the method values
// straight to policy.PlanLimits.
//
// Every count runs in a read-only transaction scoped to the counted tenant
// (app.tenant_id). builds, environment_targets and tenant_members are under
// fail-closed Row Level Security (migration 000062): a count issued on the
// request pool without that scope sees no rows and would report zero usage,
// which is exactly the fail-open this store exists to prevent.
//
// A query failure is returned, never read as zero: the middleware fails
// closed on it. The queries live in the api schema's sqlc files, next to the
// tables they count — pkg/enforcement knows the metric, not the schema.
type PostgresPlanUsageStore struct {
	Pool txBeginner
	// Now is the clock that decides which billing month is current. Nil means
	// time.Now.
	Now func() time.Time
}

// NewPostgresPlanUsageStore builds a usage store over the request pool.
func NewPostgresPlanUsageStore(pool txBeginner) *PostgresPlanUsageStore {
	return &PostgresPlanUsageStore{Pool: pool}
}

func (s *PostgresPlanUsageStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// countForTenant runs query inside a read-only transaction whose RLS scope is
// tenantID, and closes the transaction before returning.
func (s *PostgresPlanUsageStore) countForTenant(
	ctx context.Context,
	tenantID, what string,
	query func(generated.DBTX) (int32, error),
) (n int, err error) {
	if s == nil || s.Pool == nil {
		return 0, fmt.Errorf("plan usage store: no database configured")
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return 0, fmt.Errorf("plan usage store: begin %s count for tenant %s: %w", what, tenantID, err)
	}
	defer func() {
		// Read-only: rollback is the normal way to end it.
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("plan usage store: end %s count for tenant %s: %w", what, tenantID, rbErr))
		}
	}()

	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		return 0, fmt.Errorf("plan usage store: scope %s count to tenant %s: %w", what, tenantID, err)
	}
	count, err := query(tx)
	if err != nil {
		return 0, fmt.Errorf("plan usage store: count %s for tenant %s: %w", what, tenantID, err)
	}
	return int(count), nil
}

// BuildsThisMonth counts the builds the tenant started since the first
// instant of the current UTC month.
func (s *PostgresPlanUsageStore) BuildsThisMonth(ctx context.Context, tenantID string) (int, error) {
	now := s.now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return s.countForTenant(ctx, tenantID, "builds this month", func(dbtx generated.DBTX) (int32, error) {
		return generated.New().CountTenantBuildsSince(ctx, dbtx, generated.CountTenantBuildsSinceParams{
			TenantID:  tenantID,
			CreatedAt: pgtype.Timestamptz{Time: monthStart, Valid: true},
		})
	})
}

// PermanentEnvironments counts the tenant's permanent (project, environment)
// tuples. Preview environments are not counted.
func (s *PostgresPlanUsageStore) PermanentEnvironments(ctx context.Context, tenantID string) (int, error) {
	return s.countForTenant(ctx, tenantID, "permanent environments", func(dbtx generated.DBTX) (int32, error) {
		return generated.New().CountPermanentEnvironments(ctx, dbtx, tenantID)
	})
}

// Seats counts the members of the tenant.
func (s *PostgresPlanUsageStore) Seats(ctx context.Context, tenantID string) (int, error) {
	return s.countForTenant(ctx, tenantID, "seats", func(dbtx generated.DBTX) (int32, error) {
		return generated.New().CountTenantMembers(ctx, dbtx, tenantID)
	})
}
