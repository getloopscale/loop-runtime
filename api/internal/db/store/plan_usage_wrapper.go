// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

// PostgresPlanUsageStore measures the usage the plan-limit middleware gates:
// builds this month, permanent environments and seats. Each method has the
// shape of enforcement.UsageCounter, so the router passes the method values
// straight to enforcement.Middleware.
//
// A query failure is returned, never read as zero: the middleware fails
// closed on it. The queries live in the api schema's sqlc files, next to the
// tables they count — pkg/enforcement knows the metric, not the schema.
type PostgresPlanUsageStore struct {
	DB db.DBTX
	// Now is the clock that decides which billing month is current. Nil means
	// time.Now.
	Now func() time.Time
}

// NewPostgresPlanUsageStore builds a usage store over the given DBTX.
func NewPostgresPlanUsageStore(dbtx db.DBTX) *PostgresPlanUsageStore {
	return &PostgresPlanUsageStore{DB: dbtx}
}

func (s *PostgresPlanUsageStore) bridge() (generated.DBTX, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("plan usage store: no database configured")
	}
	bridge, ok := s.DB.(db.SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("plan usage store: DBTX %T does not implement SqlcBridge", s.DB)
	}
	return bridge.GeneratedDB(), nil
}

func (s *PostgresPlanUsageStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// BuildsThisMonth counts the builds the tenant started since the first
// instant of the current UTC month.
func (s *PostgresPlanUsageStore) BuildsThisMonth(ctx context.Context, tenantID string) (int, error) {
	dbtx, err := s.bridge()
	if err != nil {
		return 0, err
	}
	now := s.now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	n, err := generated.New().CountTenantBuildsSince(ctx, dbtx, generated.CountTenantBuildsSinceParams{
		TenantID:  tenantID,
		CreatedAt: pgtype.Timestamptz{Time: monthStart, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("plan usage store: count builds since %s for tenant %s: %w",
			monthStart.Format(time.RFC3339), tenantID, err)
	}
	return int(n), nil
}

// PermanentEnvironments counts the tenant's permanent (project, environment)
// tuples. Preview environments are not counted.
func (s *PostgresPlanUsageStore) PermanentEnvironments(ctx context.Context, tenantID string) (int, error) {
	dbtx, err := s.bridge()
	if err != nil {
		return 0, err
	}
	n, err := generated.New().CountPermanentEnvironments(ctx, dbtx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("plan usage store: count permanent environments for tenant %s: %w", tenantID, err)
	}
	return int(n), nil
}

// Seats counts the members of the tenant.
func (s *PostgresPlanUsageStore) Seats(ctx context.Context, tenantID string) (int, error) {
	dbtx, err := s.bridge()
	if err != nil {
		return 0, err
	}
	n, err := generated.New().CountTenantMembers(ctx, dbtx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("plan usage store: count seats for tenant %s: %w", tenantID, err)
	}
	return int(n), nil
}
