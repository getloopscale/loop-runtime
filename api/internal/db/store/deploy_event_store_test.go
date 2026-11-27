// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// Hermetic guard tests (no DB). The exec/atomicity path is covered by the
// kernel integration test against a real Postgres (deploy_events is a
// transactional-outbox — its whole point is only provable against a real tx).

func TestDeployEventStore_NilTx_FailsFast(t *testing.T) {
	s := NewDeployEventStoreSqlc()
	err := s.AppendDeployEvent(context.Background(), nil, DeployEvent{
		TransitionID: "tr-1", EventType: "deploy.succeeded", TenantID: "tn-1",
		ProjectID: "proj", DeployID: "rel-1", Payload: map[string]any{"status": "success"},
	})
	if err == nil {
		t.Fatal("AppendDeployEvent(nil tx) = nil error; want fail-fast (a transactional outbox must not auto-commit via a pool)")
	}
}

// nonBridgeDBTX is a db.DBTX that is NOT a SqlcBridge — the outbox write must
// fail loudly rather than be silently dropped.
type nonBridgeDBTX struct{ db.DBTX }

func TestDeployEventStore_NonSqlcBridgeTx_ReturnsErrNotSqlcBridge(t *testing.T) {
	s := NewDeployEventStoreSqlc()
	err := s.AppendDeployEvent(context.Background(), nonBridgeDBTX{}, DeployEvent{
		TransitionID: "tr-2", EventType: "deploy.succeeded", TenantID: "tn-1",
		ProjectID: "proj", DeployID: "rel-1", Payload: map[string]any{"status": "success"},
	})
	if !errors.Is(err, db.ErrNotSqlcBridge) {
		t.Fatalf("AppendDeployEvent with a non-SqlcBridge tx = %v; want db.ErrNotSqlcBridge (the INSERT must run on a real pgx tx, never a fake that drops the write)", err)
	}
}
