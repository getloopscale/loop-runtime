// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package processing provides PG-backed deduplication claims for the
// extracted controller loops (roadmap 02). See claim_store.go for the
// full contract.
//
// # Why a separate package?
//
// The existing BuildReconcileLoop and ReleaseConvergenceLoop dedupe with
// `sync.Map` held in-process. That map is lost the instant the controller
// is extracted to a separate binary, so two controller replicas can each
// happily start processing the same build. This package implements the
// PG-level claim that replaces `sync.Map` while remaining a strict
// superset of the old semantics (i.e. a single controller instance with
// no peers sees exactly the same behavior it does today).
//
// The claim is NOT a lock — it is a compare-and-swap assertion on two
// columns (`processing_by`, `processing_since`). A worker that wants to
// process build `b` runs:
//
//	UPDATE builds
//	   SET processing_by = $me, processing_since = now()
//	 WHERE id = $b
//	   AND (processing_by IS NULL OR processing_since < $stale)
//
// If `RowsAffected == 1`, the claim is ours. If `RowsAffected == 0`,
// another live worker holds the claim and we back off. When the worker is
// done (terminal state or error path), it calls `Release` which clears
// the columns by matching on the holder name; a release attempted by the
// wrong holder is a no-op, which is the desired behavior when the lease
// has already been fenced out.
//
// Orphan detection: `FindOrphanedClaims` returns ids whose claim is older
// than `staleAfter`. The reaper path calls this at its normal interval.
// When a claim is orphaned, any future Claim call will succeed via the
// CAS because the WHERE predicate treats a stale `processing_since` as
// equivalent to NULL.
//
// The store is deliberately stateless — one instance per package can be
// shared across goroutines.
package processing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// ErrClaimHeld is returned by Claim when another live worker already
// holds the claim. Callers should back off and retry on the next tick.
var ErrClaimHeld = errors.New("processing: claim already held by a live peer")

// ErrNotClaimHolder is returned by Release when the caller's holder id
// does not match the current claim. This usually indicates the lease was
// already fenced out and the caller should abort its current work.
var ErrNotClaimHolder = errors.New("processing: release by non-holder — lease fenced?")

// Kind names the table a claim operates on. Only the builds and releases
// tables currently support the columns; adding a new kind requires the
// matching migration.
type Kind string

const (
	KindBuild   Kind = "builds"
	KindRelease Kind = "releases"
)

// StaleAfter is the default window after which a claim is treated as
// orphaned. Must be strictly greater than the longest expected single-
// step processing time. Default: 2 minutes.
const StaleAfter = 2 * time.Minute

// ClaimStore wraps a DBTX with typed claim operations.
type ClaimStore struct {
	tx         db.DBTX
	staleAfter time.Duration
}

// NewClaimStore builds a claim store with the default stale window.
func NewClaimStore(tx db.DBTX) *ClaimStore {
	return &ClaimStore{tx: tx, staleAfter: StaleAfter}
}

// NewClaimStoreWithStale allows tests to shrink the stale window.
func NewClaimStoreWithStale(tx db.DBTX, stale time.Duration) *ClaimStore {
	if stale <= 0 {
		stale = StaleAfter
	}
	return &ClaimStore{tx: tx, staleAfter: stale}
}

// Claim atomically asserts processing ownership of (kind, id) for the
// given holder. Returns nil on success, ErrClaimHeld if a live peer holds
// the claim. The WHERE predicate treats stale claims as open, so an
// orphaned claim is automatically reclaimable without a separate cleanup
// step.
func (c *ClaimStore) Claim(ctx context.Context, kind Kind, id, holder string) error {
	if err := validateKind(kind); err != nil {
		return err
	}
	// #nosec G202 — kind is validated against a closed set, not user input.
	query := fmt.Sprintf(`
UPDATE %s
   SET processing_by    = $1,
       processing_since = now()
 WHERE id = $2
   AND (processing_by IS NULL
     OR processing_since < now() - ($3 * interval '1 second'))
`, string(kind))

	staleSec := int64(c.staleAfter.Seconds())
	res, err := c.tx.Exec(ctx, query, holder, id, staleSec)
	if err != nil {
		return fmt.Errorf("processing claim (%s id=%s holder=%s): %w", kind, id, holder, err)
	}
	if res.RowsAffected == 0 {
		return ErrClaimHeld
	}
	return nil
}

// Release clears the claim for (kind, id) iff the caller is the current
// holder. Safe to call in a defer: releasing a non-owned row returns
// ErrNotClaimHolder which most callers can log and ignore.
func (c *ClaimStore) Release(ctx context.Context, kind Kind, id, holder string) error {
	if err := validateKind(kind); err != nil {
		return err
	}
	// #nosec G202 — kind is validated against a closed set.
	query := fmt.Sprintf(`
UPDATE %s
   SET processing_by    = NULL,
       processing_since = NULL
 WHERE id = $1
   AND processing_by = $2
`, string(kind))

	res, err := c.tx.Exec(ctx, query, id, holder)
	if err != nil {
		return fmt.Errorf("processing release (%s id=%s holder=%s): %w", kind, id, holder, err)
	}
	if res.RowsAffected == 0 {
		return ErrNotClaimHolder
	}
	return nil
}

// FindOrphanedClaims returns the ids whose processing claim is older than
// the stale window. Callers typically compare this list against the
// reaper's stale state list and take over if the claim is orphaned.
func (c *ClaimStore) FindOrphanedClaims(ctx context.Context, kind Kind) ([]string, error) {
	if err := validateKind(kind); err != nil {
		return nil, err
	}
	// #nosec G202 — kind is validated against a closed set.
	query := fmt.Sprintf(`
SELECT id
  FROM %s
 WHERE processing_by IS NOT NULL
   AND processing_since < now() - ($1 * interval '1 second')
 ORDER BY processing_since ASC
 LIMIT 100
`, string(kind))

	staleSec := int64(c.staleAfter.Seconds())
	rows, err := c.tx.Query(ctx, query, staleSec)
	if err != nil {
		return nil, fmt.Errorf("processing find orphaned (%s): %w", kind, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("processing scan orphaned: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("processing orphaned rows: %w", err)
	}
	return ids, nil
}

func validateKind(kind Kind) error {
	switch kind {
	case KindBuild, KindRelease:
		return nil
	default:
		return fmt.Errorf("processing: unsupported kind %q (expected %q or %q)", kind, KindBuild, KindRelease)
	}
}
