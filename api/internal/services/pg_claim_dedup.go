// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"errors"

	"github.com/loopzero/loopzero/api/internal/processing"
)

// PGClaimDedup provides a ClaimStore-backed deduplication layer that
// can replace the in-memory sync.Map used by BuildReconcileLoop and
// ReleaseConvergenceLoop.
//
// When enabled (LOOPZERO_USE_PG_CLAIMS=true), claim/release operations go
// through PG (CAS on processing_by/processing_since columns), making
// the dedup state visible across controller replicas.
//
// When disabled (the default), it falls back to a no-op that always
// succeeds — the sync.Map in the loop struct continues to function.
//
// Phase 0.2 of docs/plans/scale-50k-plan.md.
type PGClaimDedup struct {
	store   *processing.ClaimStore
	enabled bool
	holder  string // pod name — identifies this replica
}

// NewPGClaimDedup creates a PG-backed dedup layer. When enabled=false,
// all operations are no-ops (the loop's sync.Map remains authoritative).
func NewPGClaimDedup(store *processing.ClaimStore, enabled bool, holder string) *PGClaimDedup {
	return &PGClaimDedup{
		store:   store,
		enabled: enabled,
		holder:  holder,
	}
}

// TryClaim attempts to claim a build/release for processing. Returns
// true if this replica now owns the work, false if another replica
// already claimed it. When PG claims are disabled, always returns true
// (the sync.Map in the caller handles dedup locally).
func (d *PGClaimDedup) TryClaim(ctx context.Context, kind processing.Kind, id string) bool {
	if !d.enabled || d.store == nil {
		return true
	}
	err := d.store.Claim(ctx, kind, id, d.holder)
	return err == nil
}

// Release drops the claim for a completed/failed build/release. When
// PG claims are disabled, this is a no-op (the sync.Map in the caller
// handles cleanup via active.Delete).
func (d *PGClaimDedup) Release(ctx context.Context, kind processing.Kind, id string) {
	if !d.enabled || d.store == nil {
		return
	}
	_ = d.store.Release(ctx, kind, id, d.holder)
}

// IsProcessingPG checks if ANY replica has claimed this build (used by
// the reaper to avoid competing with an active reconciliation). Returns
// false when PG claims are disabled — the reaper falls back to the
// sync.Map-based IsProcessing on the loop struct.
func (d *PGClaimDedup) IsProcessingPG(ctx context.Context, kind processing.Kind, id string) bool {
	if !d.enabled || d.store == nil {
		return false
	}
	// A build is "processing" if a claim exists and is not orphaned.
	// The simplest check: try to claim it ourselves. If we get
	// ErrClaimHeld, someone else has it.
	err := d.store.Claim(ctx, kind, id, d.holder+"-probe")
	if err == nil {
		// We accidentally claimed it with a probe holder — release immediately.
		_ = d.store.Release(ctx, kind, id, d.holder+"-probe")
		return false
	}
	// ErrClaimHeld means another replica has it.
	return errors.Is(err, processing.ErrClaimHeld)
}
