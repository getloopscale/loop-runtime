// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// rollbackLogFake keeps the rollback records in memory, closing each one once
// as the store does.
type rollbackLogFake struct {
	mu      sync.Mutex
	records map[string]*db.Rollback
}

func newRollbackLogFake() *rollbackLogFake {
	return &rollbackLogFake{records: map[string]*db.Rollback{}}
}

func (l *rollbackLogFake) Start(_ context.Context, _ db.DBTX, r *db.Rollback) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	stored := *r
	stored.Status = db.RollbackInProgress
	l.records[r.ID] = &stored
	return nil
}

func (l *rollbackLogFake) Finish(_ context.Context, tenantID, id, status, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.records[id]
	if r == nil || r.TenantID != tenantID || r.Status != db.RollbackInProgress {
		return nil
	}
	r.Status, r.FailureReason = status, reason
	return nil
}

func (l *rollbackLogFake) only(t *testing.T) db.Rollback {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.records) != 1 {
		t.Fatalf("want 1 rollback recorded, got %d", len(l.records))
	}
	for _, r := range l.records {
		return *r
	}
	return db.Rollback{}
}

func recordedFixture(t *testing.T) (*rollbackFixture, *rollbackLogFake) {
	t.Helper()
	f := newRollbackFixture(t)
	f.releases.releases["rel-current"].BuildID = "build-current"
	f.releases.releases["rel-target"].BuildID = "build-target"
	log := newRollbackLogFake()
	f.orch.deps.Log = log
	return f, log
}

// A rollback is an operation with an id, an author and an outcome, so that it
// can be listed next to the deploys of the environment.
func TestRollback_IsRecordedWhileItRunsAndWhenItSucceeds(t *testing.T) {
	f, log := recordedFixture(t)

	result, err := f.orch.Rollback(context.Background(), fixtureRequest)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	<-f.delivery.awaiting

	running := log.only(t)
	if result.RollbackID == "" || result.RollbackID != running.ID {
		t.Errorf("result names rollback %q, the record is %q", result.RollbackID, running.ID)
	}
	if running.Status != db.RollbackInProgress {
		t.Errorf("status while the sync is awaited = %s", running.Status)
	}
	want := db.Rollback{
		TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
		FromReleaseID: "rel-current", FromBuildID: "build-current",
		ToReleaseID: "rel-target", ToBuildID: "build-target", CreatedBy: "user-1",
	}
	got := running
	got.ID, got.Status = "", ""
	if got != want {
		t.Errorf("record = %+v, want %+v", got, want)
	}

	close(f.delivery.release)
	f.orch.Wait()

	if done := log.only(t); done.Status != db.RollbackSucceeded || done.FailureReason != "" {
		t.Errorf("after the sync: status = %s, reason = %q", done.Status, done.FailureReason)
	}
}

func TestRollback_AFailedRollbackIsRecordedWithItsReason(t *testing.T) {
	f, log := recordedFixture(t)
	f.delivery.awaitErr = errors.New("sync timeout: the service did not become healthy")

	if _, err := f.orch.Rollback(context.Background(), fixtureRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	close(f.delivery.release)
	f.orch.Wait()

	failed := log.only(t)
	if failed.Status != db.RollbackFailed {
		t.Fatalf("status = %s, want %s", failed.Status, db.RollbackFailed)
	}
	if failed.FailureReason != "sync timeout: the service did not become healthy" {
		t.Errorf("reason = %q", failed.FailureReason)
	}
}

func TestRollback_ADeliveryThatDoesNotStartIsRecordedAsFailed(t *testing.T) {
	f, log := recordedFixture(t)
	f.delivery.startErr = errors.New("git push refused")

	if _, err := f.orch.Rollback(context.Background(), fixtureRequest); err == nil {
		t.Fatal("expected the rollback to be refused")
	}

	if failed := log.only(t); failed.Status != db.RollbackFailed || failed.FailureReason != "git push refused" {
		t.Errorf("status = %s, reason = %q", failed.Status, failed.FailureReason)
	}
}
