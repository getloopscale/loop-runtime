// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package processing

import (
	"context"
	"errors"
	"log/slog"
)

// PGClaimDedup provides a ClaimStore-backed deduplication layer that
// can replace the in-memory sync.Map used by BuildReconcileLoop and
// ReleaseConvergenceLoop.
//
// When enabled (LOOP_RUNTIME_USE_PG_CLAIMS=true), claim/release operations go
// through PG (CAS on processing_by/processing_since columns), making
// the dedup state visible across controller replicas.
//
// When disabled (the default), it falls back to a no-op that always
// succeeds — the sync.Map in the loop struct continues to function.
//
// Phase 0.2 of scale-50k-plan.
type PGClaimDedup struct {
	store   *ClaimStore
	enabled bool
	holder  string // pod name — identifies this replica
}

// NewPGClaimDedup creates a PG-backed dedup layer. When enabled=false,
// all operations are no-ops (the loop's sync.Map remains authoritative).
func NewPGClaimDedup(store *ClaimStore, enabled bool, holder string) *PGClaimDedup {
	return &PGClaimDedup{
		store:   store,
		enabled: enabled,
		holder:  holder,
	}
}

// TryClaim attempts to claim a build/release for this replica. It returns
// (true, nil) when this replica now owns the work and (false, nil) when a
// live peer already holds it. A database failure is (false, err): the caller
// must not read an outage as contention. When PG claims are disabled it always
// grants (the sync.Map in the caller handles dedup locally).
func (d *PGClaimDedup) TryClaim(ctx context.Context, kind Kind, id string) (bool, error) {
	if !d.enabled || d.store == nil {
		return true, nil
	}
	err := d.store.Claim(ctx, kind, id, d.holder)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrClaimHeld):
		return false, nil
	default:
		return false, err
	}
}

// Release drops the claim for a completed/failed build/release. When PG claims
// are disabled, this is a no-op (the sync.Map in the caller handles cleanup
// via active.Delete). A failed release leaves the row claimed until the claim
// goes stale, so it is logged with its context.
func (d *PGClaimDedup) Release(ctx context.Context, kind Kind, id string) {
	if !d.enabled || d.store == nil {
		return
	}
	if err := d.store.Release(ctx, kind, id, d.holder); err != nil {
		slog.WarnContext(ctx, "processing: claim release failed; the claim stays until it goes stale",
			"kind", kind, "id", id, "holder", d.holder, "stale_after", StaleAfter, "error", err)
	}
}
