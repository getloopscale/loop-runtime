// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/dbconv"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

// TenantDeletionStoreSqlc is the sqlc-backed soft-delete lifecycle store
// (MEET-052): request deletion, cancel within the grace window, read the
// pending record, and write the permanent tombstone once deletion completes.
//
// # Why this file was written twice
//
// The type was created on 2026-05-29 by the sqlc pilot, which deleted the legacy
// `PostgresTenantDeletionStore` in the same commit — and never assigned the new
// one to `server.Config.TenantDeletionStore`. Nothing referenced it, so on
// 2026-06-16 a code-quality pass correctly identified it as dead and removed it,
// leaving its doc comments behind over no code at all.
//
// The consequence was not cosmetic. `DELETE /api/v1/tenants/{id}`,
// `POST /api/v1/tenants/{id}/restore` (and a bulk-delete route since removed) were
// guarded by `if cfg.TenantDeletionStore != nil`, so none of them was ever
// registered in any deployment — while the dashboard's account-deletion screen
// called the first one for real, and two rounds of client-side bug fixes were
// spent on a request that could not arrive (B-117).
//
// The dead-code detector was right about the code and wrong about the world: it
// measured reachability from the source, and what needed this type was a route
// the source never registered.
type TenantDeletionStoreSqlc struct {
	q    *generated.Queries
	pool db.DBTX
}

// NewTenantDeletionStoreSqlc constructs the soft-delete lifecycle store.
//
// `pool` MUST implement db.SqlcBridge (PgxAdapter does). A fake that does not
// panics at query time rather than at construction — same contract as every
// sibling store in this package, kept deliberately so the failure mode is
// uniform across them.
func NewTenantDeletionStoreSqlc(pool db.DBTX) *TenantDeletionStoreSqlc {
	return &TenantDeletionStoreSqlc{q: generated.New(), pool: pool}
}

// Compile-time proof that the adapter still satisfies the port it exists for.
// This is the assertion whose absence let the type be deleted as an orphan: it
// fails the moment the implementation stops existing, instead of the file
// quietly decaying into a paragraph about nothing.
var _ ports.TenantDeletionStore = (*TenantDeletionStoreSqlc)(nil)

func (s *TenantDeletionStoreSqlc) gen() generated.DBTX {
	return db.Generated(s.pool)
}

// RequestDeletion inserts a pending deletion record.
//
// Idempotent by contract: when the tenant already has a pending deletion the
// insert is a no-op (ON CONFLICT DO NOTHING) and the EXISTING record is
// returned, preserving the original requested_at/requested_by. That matters —
// a second delete request must not silently reset the grace window and buy the
// tenant another N days.
func (s *TenantDeletionStoreSqlc) RequestDeletion(
	ctx context.Context, input ports.DeletionRequest,
) (*ports.DeletionRecord, error) {
	err := s.q.InsertPendingDeletion(ctx, s.gen(), generated.InsertPendingDeletionParams{
		TenantID:    input.TenantID,
		RequestedBy: input.RequestedBy,
		Reason:      input.Reason,
		GraceUntil:  dbconv.TimePtrToTimestamptz(input.GraceUntil),
		Force:       input.Force,
	})
	if err != nil {
		return nil, fmt.Errorf("tenant deletion store: request deletion for %s: %w", input.TenantID, err)
	}
	// Read back rather than synthesising the record from the input: on the
	// conflict path the stored row is the FIRST request, and returning the input
	// would report a grace window that is not the one being enforced.
	return s.GetPendingDeletion(ctx, input.TenantID)
}

// CancelDeletion removes the pending deletion record — the restore path.
// Idempotent: cancelling a tenant with no pending deletion is not an error.
func (s *TenantDeletionStoreSqlc) CancelDeletion(ctx context.Context, tenantID string) error {
	if err := s.q.CancelPendingDeletion(ctx, s.gen(), tenantID); err != nil {
		return fmt.Errorf("tenant deletion store: cancel deletion for %s: %w", tenantID, err)
	}
	return nil
}

// GetPendingDeletion returns the pending deletion record, or nil when the tenant
// has none.
//
// "No pending deletion" is a legitimate answer, not a failure, so `no rows` maps
// to (nil, nil). Any other error propagates — a database that cannot answer is
// NOT the same as a tenant that is not being deleted, and collapsing the two
// would let an outage read as "nothing is pending".
func (s *TenantDeletionStoreSqlc) GetPendingDeletion(
	ctx context.Context, tenantID string,
) (*ports.DeletionRecord, error) {
	row, err := s.q.GetPendingDeletion(ctx, s.gen(), tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("tenant deletion store: get pending deletion for %s: %w", tenantID, err)
	}
	rec := deletionRowToRecord(
		row.TenantID, row.RequestedAt.Time, row.RequestedBy, row.Reason,
		timestamptzToTimePtr(row.GraceUntil), row.Force, int(row.AttemptCount),
	)
	return &rec, nil
}

// WriteTombstone writes the permanent audit record after deletion completes.
//
// The resource maps are stored as JSON. A nil map marshals to `null`, which the
// column accepts; encoding failure is returned rather than swallowed, because a
// tombstone that silently loses the record of what was cleaned and what failed
// is worse than no tombstone — it looks complete.
func (s *TenantDeletionStoreSqlc) WriteTombstone(
	ctx context.Context, tombstone ports.TombstoneRecord,
) error {
	cleaned, err := json.Marshal(tombstone.ResourcesCleaned)
	if err != nil {
		return fmt.Errorf("tenant deletion store: encode cleaned resources for %s: %w",
			tombstone.TenantID, err)
	}
	failed, err := json.Marshal(tombstone.ResourcesFailed)
	if err != nil {
		return fmt.Errorf("tenant deletion store: encode failed resources for %s: %w",
			tombstone.TenantID, err)
	}

	if err := s.q.InsertTombstone(ctx, s.gen(), generated.InsertTombstoneParams{
		TenantID:         tombstone.TenantID,
		TenantSlug:       tombstone.TenantSlug,
		RequestedBy:      tombstone.RequestedBy,
		Reason:           tombstone.Reason,
		ResourcesCleaned: cleaned,
		ResourcesFailed:  failed,
	}); err != nil {
		return fmt.Errorf("tenant deletion store: write tombstone for %s: %w", tombstone.TenantID, err)
	}

	// The legacy contract removed the pending entry after the tombstone landed.
	// Both statements are idempotent, so a partial failure is recoverable on
	// retry — the caller owns the transaction if it wants atomicity.
	return s.CancelDeletion(ctx, tombstone.TenantID)
}

// timestamptzToTimePtr maps an SQL timestamp to a domain pointer.
//
// INVALID becomes nil, never the zero time: a tenant with no grace deadline and
// a tenant whose deadline is the year zero are different facts, and the caller
// decides differently on each.
func timestamptzToTimePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

// deletionRowToRecord builds the domain record from already-converted values.
//
// It takes scalars rather than the generated row type so the conversion can be
// tested without constructing sqlc internals — the mapping is the part that has
// a nil case worth pinning, and the part an adapter like this usually gets wrong.
func deletionRowToRecord(
	tenantID string, requestedAt time.Time, requestedBy, reason string,
	graceUntil *time.Time, force bool, attemptCount int,
) ports.DeletionRecord {
	return ports.DeletionRecord{
		TenantID:     tenantID,
		RequestedAt:  requestedAt,
		RequestedBy:  requestedBy,
		Reason:       reason,
		GraceUntil:   graceUntil,
		Force:        force,
		AttemptCount: attemptCount,
	}
}
