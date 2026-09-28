// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package plantier is the one policy the HTTP layer applies when it needs the
// plan a tenant is on. A leaf: it serves no route.
//
// The plan itself is read by pkgdomain.ResolvePlanTier over a PlanTierStore
// (the enforcement limits store in production, which counts active and
// trialing subscriptions). What this package owns is what a route does when
// that read cannot answer, which used to be written separately in each
// handler — and had drifted: billing read the subscriptions table itself,
// without the status filter, and showed a canceled Pro tenant as "pro".
package plantier

import (
	"context"
	"log/slog"

	pkgdomain "github.com/getloopscale/loopzero/pkg/domain"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Resolve returns the tenant's canonical plan tier.
//
// With no store wired (tests, no-database boots) the answer is Starter, the
// onboarding tier. When the store is wired and cannot answer — a transport
// error, or a plan value the enum does not know — it returns
// ErrPlatformDegraded: a decision about a paying tenant is never made on a
// guessed Starter (ADR D4, the H7 silent-downgrade closure).
func Resolve(ctx context.Context, store pkgdomain.PlanTierStore, tenantID string) (pkgdomain.PlanTier, *loopzeroerrors.LoopZeroError) {
	if store == nil {
		return pkgdomain.PlanTierStarter, nil
	}
	tier, err := pkgdomain.ResolvePlanTier(ctx, store, tenantID)
	if err != nil {
		slog.ErrorContext(ctx, "plan tier resolution failed", "tenant_id", tenantID, "error", err)
		return pkgdomain.PlanTierUnknown, loopzeroerrors.ErrPlatformDegraded.Wrap(
			"plan tier resolution failed — retry or contact support")
	}
	return tier, nil
}
