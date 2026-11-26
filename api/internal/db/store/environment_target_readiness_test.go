// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/getloopscale/loop-runtime/api/internal/db/dbtest"
	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// execRecorder is the generated.DBTX side of the fake: it answers Exec with a
// fixed command tag or error and records the arguments.
type execRecorder struct {
	rowsAffected int64
	err          error
	args         []any
}

func (e *execRecorder) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	e.args = args
	if e.err != nil {
		return pgconn.CommandTag{}, e.err
	}
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", e.rowsAffected)), nil
}

func (e *execRecorder) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("execRecorder: Query not expected")
}

func (e *execRecorder) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("execRecorder: QueryRow not expected")
}

// bridgedFake is a db.DBTX whose sqlc handle is the recorder.
type bridgedFake struct {
	dbtest.FakeDBTX
	gen *execRecorder
}

func (b *bridgedFake) GeneratedDB() generated.DBTX { return b.gen }

func TestRecordReplicasReady_ReportsTheTargetFoundWhenARowWasUpdated(t *testing.T) {
	rec := &execRecorder{rowsAffected: 1}
	fake := &bridgedFake{gen: rec}
	store := NewPostgresEnvironmentTargetStore(fake, fake)
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	found, err := store.RecordReplicasReady(context.Background(), "tn-acme", "shop", "production", 3, at)

	if err != nil || !found {
		t.Fatalf("RecordReplicasReady = (%v, %v), want (true, nil)", found, err)
	}
	if got := *(rec.args[0].(*int32)); got != 3 {
		t.Errorf("replicas ready written = %d, want 3", got)
	}
	if rec.args[2] != "tn-acme" || rec.args[3] != "shop" || rec.args[4] != "production" {
		t.Errorf("key args = %v, want tn-acme/shop/production", rec.args[2:])
	}
}

func TestRecordReplicasReady_ReportsTheTargetMissingWhenNoRowMatched(t *testing.T) {
	fake := &bridgedFake{gen: &execRecorder{rowsAffected: 0}}
	store := NewPostgresEnvironmentTargetStore(fake, fake)

	found, err := store.RecordReplicasReady(context.Background(), "tn-acme", "shop", "production", 1, time.Now())

	if err != nil || found {
		t.Fatalf("RecordReplicasReady = (%v, %v), want (false, nil)", found, err)
	}
}

func TestRecordReplicasReady_ReturnsTheWriteError(t *testing.T) {
	cause := errors.New("pg down")
	fake := &bridgedFake{gen: &execRecorder{err: cause}}
	store := NewPostgresEnvironmentTargetStore(fake, fake)

	_, err := store.RecordReplicasReady(context.Background(), "tn-acme", "shop", "production", 1, time.Now())

	if !errors.Is(err, cause) {
		t.Fatalf("RecordReplicasReady err = %v, want it to wrap %v", err, cause)
	}
}
