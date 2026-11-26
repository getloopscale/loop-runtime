// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

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
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
	"github.com/getloopscale/loop-runtime/pkg/naming"
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
// SET LOCAL app.tenant_id=” RLS bypass is load-bearing, not incidental.
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

func (in MeterInput) splitParams() generated.MeterComputeSplitParams {
	return generated.MeterComputeSplitParams{
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

// MeterComputeSplit runs the ADR-9 period-rollover writer (SC-7 total-clamp) + up
// to TWO usage_records upserts in ONE RLS-bypass tx: the tail cost bills into P1
// (expected's period) and the head cost into P2 (the period right after — keyed
// off metering_period_end(expected), NEVER now()'s period, MEET-006). Each portion
// is written to its OWN period window (P1Start/P1End vs P2Start/P2End); a zero-cost
// portion writes no row. Returns advanced=true with the clamped total (tail+head),
// or advanced=false/delta 0 on HELD (no straddle / lost CAS / stale fence).
func (s *MeteringStore) MeterComputeSplit(ctx context.Context, in MeterInput) (advanced bool, delta int64, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = ignoreTxClosed(tx.Rollback(ctx)) }()

	if _, err = tx.Exec(ctx, setLocalNullTenant); err != nil {
		return false, 0, err
	}

	q := generated.New()
	row, err := q.MeterComputeSplit(ctx, tx, in.splitParams())
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, ignoreTxClosed(tx.Commit(ctx))
	}
	if err != nil {
		return false, 0, err
	}

	// tail → P1 (expected's period); head → P2 (the next period). Pair each cost
	// with its OWN window — swapping silently mis-bills across two Stripe periods.
	if row.TailCost > 0 {
		if err = q.UpsertUsageRecord(ctx, tx, generated.UpsertUsageRecordParams{
			ID: uuid.NewString(), TenantID: in.TenantID, Metric: in.Metric,
			Quantity: row.TailCost, PeriodStart: row.P1Start, PeriodEnd: row.P1End,
		}); err != nil {
			return false, 0, err
		}
	}
	if row.HeadCost > 0 {
		if err = q.UpsertUsageRecord(ctx, tx, generated.UpsertUsageRecordParams{
			ID: uuid.NewString(), TenantID: in.TenantID, Metric: in.Metric,
			Quantity: row.HeadCost, PeriodStart: row.P2Start, PeriodEnd: row.P2End,
		}); err != nil {
			return false, 0, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return false, 0, err
	}
	return true, row.TailCost + row.HeadCost, nil
}

// SeedMeteringCursor seeds the (tenant, metric, cell) cursor to now() on first
// sight (SC-5 / EC-10 — zero charge for the pre-existence window). Idempotent:
// an existing cursor is left untouched (ON CONFLICT DO NOTHING), so a re-seed can
// never rewind a live cursor. metering_cursor has no RLS — no SET LOCAL needed.
func (s *MeteringStore) SeedMeteringCursor(ctx context.Context, tenantID, metric, cellID string) error {
	return generated.New().SeedMeteringCursor(ctx, s.pool, generated.SeedMeteringCursorParams{
		TenantID: tenantID, Metric: metric, CellID: cellID,
	})
}

// GetMeteringCursor reads the current cursor value for (tenant, metric, cell) —
// the CAS target the worker threads into MeterInput.Expected each tick (ADR-4/ADR-9:
// the CTE advances only WHERE last_metered_at = @expected). found=false on NoRows
// (the cursor has not been seeded yet; the worker seeds first via SeedMeteringCursor,
// SC-5, then reads). metering_cursor has no RLS — no SET LOCAL needed.
func (s *MeteringStore) GetMeteringCursor(ctx context.Context, tenantID, metric, cellID string) (cursor time.Time, found bool, err error) {
	ts, err := generated.New().GetMeteringCursor(ctx, s.pool, generated.GetMeteringCursorParams{
		TenantID: tenantID, Metric: metric, CellID: cellID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return ts.Time, true, nil
}

// NamespacesForTenant returns the canonical Kubernetes namespaces the worker must
// meter for a tenant: naming.Namespace(tenantID, project.name, env) for EVERY
// (project, env) tuple the tenant owns (ADR-5). It composes the two existing
// PG reads — ListProjectsByTenant × ListEnvironmentTargetsByProject — and derives
// each namespace via the SAME canonical builder as tenant_cr_manager + the
// manifest renderer, so the metered namespaces byte-match the real ones (a
// mismatch would list pods in a nonexistent namespace → under-bill). This closes
// v1.1's single-namespace bug (metering only loopzero-<tenant> systematically
// under-billed multi-project tenants). No RLS on the read (control-plane pool).
//
// Edge cases: zero projects → empty slice, nil error (an Active CR whose PG rows
// were removed mid-race meters nothing). Zero env_targets for a project → that
// project contributes no namespace. A naming.Namespace error (a stored
// project/env whose combined name exceeds 63 chars — already rejected at
// AddEnvironment, so defensive) skips that namespace with a WARN and continues;
// an un-renderable namespace has no real pods, so skipping does not under-bill.
func (s *MeteringStore) NamespacesForTenant(ctx context.Context, tenantID string) ([]string, error) {
	q := generated.New()
	projects, err := q.ListProjectsByTenant(ctx, s.pool, tenantID)
	if err != nil {
		return nil, fmt.Errorf("metering: list projects for tenant %s: %w", tenantID, err)
	}

	namespaces := make([]string, 0, len(projects))
	for _, p := range projects {
		// environment_targets.project_id holds the project NAME (the deploy
		// path writes build.ProjectID, which the CLI fills with the name), not
		// projects.id. Querying by id matched nothing and metered no namespace.
		targets, terr := q.ListEnvironmentTargetsByProject(ctx, s.pool, generated.ListEnvironmentTargetsByProjectParams{
			TenantID:  tenantID,
			ProjectID: p.Name,
		})
		if terr != nil {
			return nil, fmt.Errorf("metering: list env targets for project %s (tenant %s): %w", p.Name, tenantID, terr)
		}
		for _, tgt := range targets {
			ns, nerr := naming.Namespace(tenantID, p.Name, tgt.Environment)
			if nerr != nil {
				slog.WarnContext(ctx, "metering: skipping un-renderable namespace (already rejected at env creation)",
					"tenant_id", tenantID, "project", p.Name, "env", tgt.Environment, "err", nerr)
				continue
			}
			namespaces = append(namespaces, ns)
		}
	}
	return namespaces, nil
}

// IsMeteringEnabled reads the observe-only kill-switch (metering_config singleton).
// The loop reads it per-tick and skips the pass when disabled. EC-5 fail-safe:
//   - absent row (pgx.ErrNoRows) → (false, found=false, nil) — DISABLED; the caller
//     WARNs (a seeded-then-deleted config row is an operational alarm).
//   - a real query error → (false, false, err) — the error is surfaced, and enabled
//     is NEVER true: a metering producer that cannot read its own kill-switch MUST
//     NOT bill (fail-safe, mirrors SC-3's read-failure HOLD). Never swallowed (Rule 8).
//
// metering_config has no RLS — no SET LOCAL needed.
func (s *MeteringStore) IsMeteringEnabled(ctx context.Context) (enabled bool, found bool, err error) {
	en, err := generated.New().GetMeteringEnabled(ctx, s.pool)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return en, true, nil
}

// GetPlanTierForTenant returns the tenant's active billing plan tier, defaulting
// to "starter" when no active subscription exists (EC-4 — a missing plan is not
// fatal; the tenant is metered at the free tier). subscriptions has no RLS.
func (s *MeteringStore) GetPlanTierForTenant(ctx context.Context, tenantID string) (string, error) {
	tier, err := generated.New().GetPlanTierForTenant(ctx, s.pool, tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "starter", nil
	}
	return tier, err
}

// GetCellForTenant returns the tenant's canonical cell placement (tenants.cell_id,
// the placement SoT per INV I2 — NEVER the CR spec.cellID mirror). found=false on
// NoRows (a CR Active whose PG tenants row was deleted mid-race has no projects
// anyway — the caller skips it, not fatal). tenants has no RLS.
func (s *MeteringStore) GetCellForTenant(ctx context.Context, tenantID string) (cell string, found bool, err error) {
	cell, err = generated.New().GetCellForTenant(ctx, s.pool, tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return cell, true, nil
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
