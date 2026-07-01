// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// metering_wrapper.go — MeteringStore, the compute-metering money-writer
// (plan metering-compute-loop T2.3). Each write runs the ADR-4 (same-period) or
// ADR-9 (period-split) data-modifying CTE + the usage_records upsert(s) in ONE
// RLS-bypass tx on a DEDICATED worker pool (ADR-10), so a partial failure rolls
// back the cursor advance too. The values feed usage_records → Stripe.
//
// DIP: the public contract is the domain MeterInput (plain Go types); the
// generated/pgtype persistence types are converted internally and never exposed
// to the T3.1 worker.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

// MeterInput is the domain-shaped request the metering worker passes for one
// (tenant, metric, cell) tick. Plain Go types — no pgtype / generated leakage.
type MeterInput struct {
	TenantID     string
	Metric       string
	CellID       string
	Expected     time.Time     // the cursor value read this tick (CAS target)
	MaxDelta     time.Duration // clamp on a stale-cursor catch-up (ADR-8)
	ReqMillicore int64         // Σ reserved millicores for the tenant on this cell
	Gen          int64         // the loop's own Lease.GenerationID (SC-6 fence)
	LoopName     string
	LeaderCell   string
	PlanTier     string
}

// MeteringStore owns a dedicated bounded pgxpool (ADR-10), physically separate
// from the API pool, connected as loopzero_worker (EffectiveWorkerDatabaseURL) so the
// SET LOCAL app.tenant_id='' RLS bypass is load-bearing, not incidental.
type MeteringStore struct {
	pool *pgxpool.Pool
}

// NewMeteringStore wraps the worker's dedicated pool.
func NewMeteringStore(pool *pgxpool.Pool) *MeteringStore {
	return &MeteringStore{pool: pool}
}

func (in MeterInput) sameParams() generated.MeterComputeParams {
	return generated.MeterComputeParams{
		ReqMillicore: in.ReqMillicore,
		TenantID:     in.TenantID,
		Metric:       in.Metric,
		CellID:       in.CellID,
		Expected:     pgtype.Timestamptz{Time: in.Expected, Valid: true},
		Gen:          in.Gen,
		LoopName:     in.LoopName,
		LeaderCell:   in.LeaderCell,
		MaxDelta:     pgtype.Interval{Microseconds: in.MaxDelta.Microseconds(), Valid: true},
		PlanTier:     in.PlanTier,
	}
}

// MeterCompute runs the ADR-4 same-period writer + the usage_records upsert in
// ONE RLS-bypass tx. Returns advanced=true with the delta metered when the cursor
// advanced, or advanced=false (delta 0) when the write HELD (lost CAS / backward
// clock / period straddle / stale fence — pgx.ErrNoRows from the 0-row CTE). A
// zero-delta advance writes NO usage row (no spurious 0-quantity Stripe line).
func (s *MeteringStore) MeterCompute(ctx context.Context, in MeterInput) (advanced bool, delta int64, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = ignoreTxClosed(tx.Rollback(ctx)) }()

	if _, err = tx.Exec(ctx, setLocalNullTenant); err != nil {
		return false, 0, err
	}

	q := generated.New()
	row, err := q.MeterCompute(ctx, tx, in.sameParams())
	if errors.Is(err, pgx.ErrNoRows) {
		// HELD — the CTE guards rejected the advance; nothing to bill.
		return false, 0, ignoreTxClosed(tx.Commit(ctx))
	}
	if err != nil {
		return false, 0, err
	}

	if row.Delta > 0 {
		if err = q.UpsertUsageRecord(ctx, tx, generated.UpsertUsageRecordParams{
			ID:          uuid.NewString(),
			TenantID:    in.TenantID,
			Metric:      in.Metric,
			Quantity:    row.Delta,
			PeriodStart: row.PeriodStart,
			PeriodEnd:   row.PeriodEnd,
		}); err != nil {
			return false, 0, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return false, 0, err
	}
	return true, row.Delta, nil
}

// setLocalNullTenant is the transaction-scoped RLS bypass the worker uses to scan
// across tenants (mig 000023 :127-142). MUST be inside the same tx as the writes.
const setLocalNullTenant = `SET LOCAL app.tenant_id = ''`

// ignoreTxClosed collapses pgx's post-commit rollback no-op (ErrTxClosed) to nil,
// so the canonical `defer tx.Rollback` after a successful Commit is not an error.
func ignoreTxClosed(err error) error {
	if errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}
	return err
}
