// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package build

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/services/deploycontracts"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// --- Test doubles for BuildReaper ---

type fakeBuildStoreReaper struct {
	builds     map[string]*db.Build
	lastStatus map[string]string
	lastError  map[string]string
}

func newFakeBuildStoreReaper() *fakeBuildStoreReaper {
	return &fakeBuildStoreReaper{
		builds:     make(map[string]*db.Build),
		lastStatus: make(map[string]string),
		lastError:  make(map[string]string),
	}
}

func (f *fakeBuildStoreReaper) Create(_ context.Context, _ db.DBTX, b *db.Build) error {
	f.builds[b.ID] = b
	return nil
}
func (f *fakeBuildStoreReaper) GetByID(_ context.Context, id string) (*db.Build, error) {
	b, ok := f.builds[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return b, nil
}

// GetByIDForTenant — T0.4 of deep-review-2026-05-10-remediation-plan.
// Emulates the production SQL filter for the unit-test scope.
func (f *fakeBuildStoreReaper) GetByIDForTenant(ctx context.Context, tenantID, buildID string) (*db.Build, error) {
	b, err := f.GetByID(ctx, buildID)
	if err != nil || b == nil {
		return b, err
	}
	if b.TenantID != tenantID {
		return nil, nil
	}
	return b, nil
}
func (f *fakeBuildStoreReaper) HasActiveBuild(_ context.Context, _ db.DBTX, _, _, _ string) (bool, error) {
	return false, nil
}
func (f *fakeBuildStoreReaper) UpdateStatus(_ context.Context, _ db.DBTX, id, event string) (*db.Build, error) {
	f.lastStatus[id] = event
	if b, ok := f.builds[id]; ok {
		b.Status = "FAILED"
	}
	return f.builds[id], nil
}

func (f *fakeBuildStoreReaper) UpdateStatusIfCurrent(_ context.Context, _, _ string, _ statemachine.BuildPhase, _ string) (*db.Build, error) {
	return nil, nil
}
func (f *fakeBuildStoreReaper) SetErrorMessage(_ context.Context, _ db.DBTX, id, msg string) error {
	f.lastError[id] = msg
	return nil
}
func (f *fakeBuildStoreReaper) SetWorkflowID(_ context.Context, _ db.DBTX, _, _ string) error {
	return nil
}
func (f *fakeBuildStoreReaper) SetAppsSpec(_ context.Context, _ db.DBTX, _ string, _ []db.BuildAppSpec) error {
	return nil
}
func (f *fakeBuildStoreReaper) GetBuildApps(_ context.Context, _ string) ([]db.BuildAppSpec, error) {
	return nil, nil
}
func (f *fakeBuildStoreReaper) ListByProject(_ context.Context, _, _ string, _, _ int) ([]db.Build, error) {
	return nil, nil
}
func (f *fakeBuildStoreReaper) ListByProjectAndEnv(_ context.Context, _, _, _ string, _, _ int) ([]db.Build, int, error) {
	return nil, 0, nil
}
func (f *fakeBuildStoreReaper) ListEnvsForProject(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}
func (f *fakeBuildStoreReaper) FindActiveBuilds(_ context.Context, _ string) ([]db.Build, error) {
	return nil, nil
}
func (f *fakeBuildStoreReaper) CancelActiveBuilds(_ context.Context, _ db.DBTX, _, _, _ string) (int, []string, error) {
	return 0, nil, nil
}

type fakeStaleChecker struct {
	result []StaleBuild
	err    error
}

func (f *fakeStaleChecker) FindStaleBuilds(_ context.Context, _ string, _, _, _, _ time.Duration) ([]StaleBuild, error) {
	return f.result, f.err
}

type fakeReconcileChecker struct {
	processing map[string]bool
}

func (f *fakeReconcileChecker) IsProcessing(id string) bool {
	if f.processing == nil {
		return false
	}
	return f.processing[id]
}

type fakeLockerReaper struct{}

func (f *fakeLockerReaper) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

type fakeEventStoreReaper struct {
	events []struct {
		BuildID   string
		EventType string
		Data      json.RawMessage
	}
	nextID int64
}

func (f *fakeEventStoreReaper) Append(_ context.Context, buildID, eventType string, data json.RawMessage) (int64, error) {
	f.nextID++
	f.events = append(f.events, struct {
		BuildID   string
		EventType string
		Data      json.RawMessage
	}{buildID, eventType, data})
	return f.nextID, nil
}

type fakeWorkflowReaper struct {
	statuses map[string]*deploycontracts.WorkflowStatus
}

func (f *fakeWorkflowReaper) Submit(_ context.Context, _ deploycontracts.WorkflowSpec) (string, error) {
	return "", nil
}
func (f *fakeWorkflowReaper) Cancel(_ context.Context, _, _ string) error { return nil }
func (f *fakeWorkflowReaper) GetStatus(_ context.Context, _, id string) (*deploycontracts.WorkflowStatus, error) {
	if f.statuses == nil {
		return nil, fmt.Errorf("not found")
	}
	s, ok := f.statuses[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return s, nil
}

// --- Tests ---

func TestBuildReaper_ReapsStaleQueuedBuild(t *testing.T) {
	bs := newFakeBuildStoreReaper()
	bs.builds["build-1"] = &db.Build{
		ID: "build-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "QUEUED",
	}

	checker := &fakeStaleChecker{
		result: []StaleBuild{
			{ID: "build-1", State: statemachine.BPhQueued, Event: statemachine.BPEventQueueTimeout},
		},
	}
	es := &fakeEventStoreReaper{}

	reaper := NewBuildReaper(bs, checker, &fakeLockerReaper{}, &fakeWorkflowReaper{}, es,
		slog.Default(), DefaultBuildReaperConfig(), &fakeReconcileChecker{})

	reaped := reaper.ReapOnce(context.Background())

	if reaped != 1 {
		t.Fatalf("expected 1 reaped, got %d", reaped)
	}
	if bs.lastStatus["build-1"] != string(statemachine.BPEventQueueTimeout) {
		t.Errorf("expected status event %s, got %s", statemachine.BPEventQueueTimeout, bs.lastStatus["build-1"])
	}
	if bs.lastError["build-1"] == "" {
		t.Error("expected error message to be set")
	}
	if len(es.events) != 1 || es.events[0].EventType != "build.failed" {
		t.Errorf("expected build.failed SSE event, got %v", es.events)
	}
}

func TestBuildReaper_SkipsTerminalBuilds(t *testing.T) {
	bs := newFakeBuildStoreReaper()
	bs.builds["build-1"] = &db.Build{
		ID: "build-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "SUCCEEDED",
	}

	checker := &fakeStaleChecker{
		result: []StaleBuild{
			{ID: "build-1", State: statemachine.BPhQueued, Event: statemachine.BPEventQueueTimeout},
		},
	}

	reaper := NewBuildReaper(bs, checker, &fakeLockerReaper{}, nil, nil,
		slog.Default(), DefaultBuildReaperConfig(), &fakeReconcileChecker{})

	reaped := reaper.ReapOnce(context.Background())

	if reaped != 0 {
		t.Fatalf("expected 0 reaped (already terminal), got %d", reaped)
	}
}

func TestBuildReaper_SkipsBuildBeingReconciled(t *testing.T) {
	bs := newFakeBuildStoreReaper()
	bs.builds["build-1"] = &db.Build{
		ID: "build-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "QUEUED",
	}

	checker := &fakeStaleChecker{
		result: []StaleBuild{
			{ID: "build-1", State: statemachine.BPhQueued, Event: statemachine.BPEventQueueTimeout},
		},
	}

	reconciler := &fakeReconcileChecker{processing: map[string]bool{"build-1": true}}

	reaper := NewBuildReaper(bs, checker, &fakeLockerReaper{}, nil, nil,
		slog.Default(), DefaultBuildReaperConfig(), reconciler)

	reaped := reaper.ReapOnce(context.Background())

	if reaped != 0 {
		t.Fatalf("expected 0 reaped (being reconciled), got %d", reaped)
	}
}

func TestBuildReaper_SkipsActiveWorkflow(t *testing.T) {
	bs := newFakeBuildStoreReaper()
	bs.builds["build-1"] = &db.Build{
		ID: "build-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "RUNNING", WorkflowID: "wf-1",
	}

	checker := &fakeStaleChecker{
		result: []StaleBuild{
			{ID: "build-1", State: statemachine.BPhRunning, Event: statemachine.BPEventTimeout},
		},
	}

	wf := &fakeWorkflowReaper{
		statuses: map[string]*deploycontracts.WorkflowStatus{
			"wf-1": {Phase: "Running"},
		},
	}

	reaper := NewBuildReaper(bs, checker, &fakeLockerReaper{}, wf, nil,
		slog.Default(), DefaultBuildReaperConfig(), &fakeReconcileChecker{})

	reaped := reaper.ReapOnce(context.Background())

	if reaped != 0 {
		t.Fatalf("expected 0 reaped (workflow still running), got %d", reaped)
	}
}

func TestBuildReaper_NoStaleBuilds(t *testing.T) {
	checker := &fakeStaleChecker{result: nil}
	reaper := NewBuildReaper(newFakeBuildStoreReaper(), checker, &fakeLockerReaper{}, nil, nil,
		slog.Default(), DefaultBuildReaperConfig(), &fakeReconcileChecker{})

	reaped := reaper.ReapOnce(context.Background())
	if reaped != 0 {
		t.Fatalf("expected 0, got %d", reaped)
	}
}

func TestBuildReaper_CheckerError_ReturnsZero(t *testing.T) {
	checker := &fakeStaleChecker{err: fmt.Errorf("db down")}
	reaper := NewBuildReaper(newFakeBuildStoreReaper(), checker, &fakeLockerReaper{}, nil, nil,
		slog.Default(), DefaultBuildReaperConfig(), &fakeReconcileChecker{})

	reaped := reaper.ReapOnce(context.Background())
	if reaped != 0 {
		t.Fatalf("expected 0 on checker error, got %d", reaped)
	}
}
