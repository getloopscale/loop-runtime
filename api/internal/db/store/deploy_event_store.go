// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// DeployEventStore is the write side of the deploy_events transactional outbox
// (migration 000051). The release-activation kernel calls AppendDeployEvent on
// the SAME tx as the state transition, so the event is captured atomically with
// the deploy — no lost/duplicate events on crash (Option C, MEET-2026-07-03-005).
//
// There is deliberately NO read method here: loop-console drains the outbox
// (a cross-tenant system query outside this engine), the engine only writes.
type DeployEventStore interface {
	AppendDeployEvent(ctx context.Context, tx db.DBTX, ev DeployEvent) error
}

// DeployEvent is the domain-level outbox row the kernel emits on a terminal
// deploy transition. Payload MUST carry customer-safe fields only (Rule 3) —
// no internal terms; the emit site builds it.
type DeployEvent struct {
	// TransitionID is the cloud fan-out dedup key: one value per ENTRY into
	// ACTIVE (a fresh UUID per activation), NOT the release id — a Release
	// re-enters ACTIVE after rollback recovery and each entry is a distinct
	// deploy.succeeded. UNIQUE(transition_id) rejects a double emit.
	TransitionID  string
	EventType     string         // "deploy.succeeded" | "deploy.failed"
	TenantID      string         // outbox column — used by the cloud fan-out to match webhooks
	ProjectID     string         // project NAME (per the project-name-not-UUID convention)
	DeployID      string         // release id (succeeded) or build id (failed)
	CorrelationID string         // "" = none (maps to NULL)
	Payload       map[string]any // customer-safe fields only (Rule 3)
}

// Compile-time assertion: the sqlc-backed impl satisfies the interface.
var _ DeployEventStore = (*DeployEventStoreSqlc)(nil)

// DeployEventStoreSqlc is the sqlc-backed outbox writer.
type DeployEventStoreSqlc struct {
	q *generated.Queries
}

// NewDeployEventStoreSqlc constructs the outbox writer. The INSERT always runs
// on the caller's tx (no pool field) — the transactional-outbox guarantee.
func NewDeployEventStoreSqlc() *DeployEventStoreSqlc {
	return &DeployEventStoreSqlc{q: generated.New()}
}

// gen resolves the sqlc handle (db.Generated). A DBTX that is not a real
// pgx-backed connection makes the INSERT fail with db.ErrNotSqlcBridge: the
// outbox row MUST run on the activation tx, never on a fake that would
// silently drop the write.
func (s *DeployEventStoreSqlc) gen(tx db.DBTX) generated.DBTX {
	return db.Generated(tx)
}

// AppendDeployEvent inserts one outbox row on the caller's transaction. A nil tx
// is rejected (fail-fast): auto-commit via a pool would defeat the whole point
// of a transactional outbox — the event must commit/rollback WITH the deploy.
func (s *DeployEventStoreSqlc) AppendDeployEvent(ctx context.Context, tx db.DBTX, ev DeployEvent) error {
	if tx == nil {
		return fmt.Errorf("deploy_event_store: AppendDeployEvent requires the activation tx (transactional outbox)")
	}
	payloadJSON, err := json.Marshal(ev.Payload)
	if err != nil {
		return fmt.Errorf("deploy_event_store: marshal payload: %w", err)
	}
	var corr *string
	if ev.CorrelationID != "" {
		c := ev.CorrelationID
		corr = &c
	}
	return s.q.InsertDeployEvent(ctx, s.gen(tx), generated.InsertDeployEventParams{
		TransitionID:  ev.TransitionID,
		EventType:     ev.EventType,
		TenantID:      ev.TenantID,
		ProjectID:     ev.ProjectID,
		DeployID:      ev.DeployID,
		CorrelationID: corr,
		Payload:       payloadJSON,
	})
}
