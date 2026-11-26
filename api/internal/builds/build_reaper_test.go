// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
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
	result  []StaleBuild
	err     error
	gotCell *string // the cell scope the reaper asked for, when a test wants it
}

func (f *fakeStaleChecker) FindStaleBuilds(_ context.Context, cellID string, _, _, _, _ time.Duration) ([]StaleBuild, error) {
	if f.gotCell != nil {
		*f.gotCell = cellID
	}
	return f.result, f.err
}

// The API runs the reaper with no cell, which backgroundloops.Deps documents as every
// cell. It used to become cell-0, so a build stuck on any other cell was never timed out
// (#450).
func TestBuildReaper_EmptyCellScansEveryCell(t *testing.T) {
	got := "unset"
	checker := &fakeStaleChecker{gotCell: &got}
	reaper := NewBuildReaper(newFakeBuildStoreReaper(), checker, &fakeLockerReaper{}, nil, nil,
		slog.Default(), DefaultBuildReaperConfig(), &fakeReconcileChecker{})

	reaper.ReapOnce(context.Background())

	if got != "" {
		t.Errorf("reaper asked for cell %q, want \"\" (every cell)", got)
	}
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

// TestBuildReaper_ReapsRunningBuildWhenPodsStuckInImagePullBackOff — Red Team loopzero-cloud#133.
//
// Root cause: reaper.reap() M-03 defensive check (build_reaper.go:189-200) short-circuits
// on wfStatus.Phase == "Running", designed to protect legitimately slow builds. But when
// workflow pods are wedged in ImagePullBackOff / ErrImagePull / CreateContainerError for
// the entire RunningTimeout window (Red Team scenario: 29+ min in ImagePullBackOff pulling
// registry.digitalocean.com/loopzero-system/loopzero-cosign-signer:v0.1.0 that never authorizes),
// Argo still reports Phase=Running while the workflow makes zero real progress. Result:
// build stays RUNNING forever until the CLI's 40s connection timeout mis-blames "connection".
//
// Fix contract: when wfStatus.StuckPodReason != "" AND state is RUNNING AND RunningTimeout
// budget is exceeded → the reaper MUST reap with a specific ImagePullBackOff-shaped
// reason that names the failing container image + suggests actionable operator next steps.
func TestBuildReaper_ReapsRunningBuildWhenPodsStuckInImagePullBackOff(t *testing.T) {
	bs := newFakeBuildStoreReaper()
	bs.builds["build-133"] = &db.Build{
		ID: "build-133", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "RUNNING", WorkflowID: "wf-133",
	}

	checker := &fakeStaleChecker{
		result: []StaleBuild{
			{ID: "build-133", State: statemachine.BPhRunning, Event: statemachine.BPEventTimeout},
		},
	}

	// Argo workflow reports Phase=Running but the underlying pod is stuck.
	// StuckPodReason is populated by WorkflowSubmitter.GetStatus from the
	// workflow node message ("Back-off pulling image ..." / "ImagePullBackOff").
	wf := &fakeWorkflowReaper{
		statuses: map[string]*deploycontracts.WorkflowStatus{
			"wf-133": {
				Phase:          "Running",
				StuckPodReason: `ImagePullBackOff: Back-off pulling image "registry.digitalocean.com/loopzero-system/loopzero-cosign-signer:v0.1.0"`,
			},
		},
	}
	es := &fakeEventStoreReaper{}

	reaper := NewBuildReaper(bs, checker, &fakeLockerReaper{}, wf, es,
		slog.Default(), DefaultBuildReaperConfig(), &fakeReconcileChecker{})

	reaped := reaper.ReapOnce(context.Background())

	if reaped != 1 {
		t.Fatalf("expected 1 reaped (stuck pod overrides M-03), got %d", reaped)
	}
	if bs.lastStatus["build-133"] != string(statemachine.BPEventTimeout) {
		t.Errorf("expected timeout event, got %q", bs.lastStatus["build-133"])
	}
	// Reason MUST name the pod-level failure so the operator knows to fix
	// the container image / registry auth rather than blame the CLI network.
	msg := bs.lastError["build-133"]
	if msg == "" {
		t.Fatal("expected reaper error message, got empty")
	}
	if !containsAny(msg, "ImagePullBackOff", "image pull", "container image") {
		t.Errorf("expected reason to mention image-pull failure, got %q", msg)
	}
	if len(es.events) != 1 || es.events[0].EventType != "build.failed" {
		t.Fatalf("expected build.failed SSE event, got %v", es.events)
	}
}

// TestBuildReaper_RespectsM03GateWhenWorkflowRunningWithoutStuckPods — Red Team #133 regression-protector.
// The #133 fix MUST NOT break the M-03 protection for legitimately slow builds. When the
// workflow reports Phase=Running with NO StuckPodReason, the reaper MUST continue to skip
// the reap (existing TestBuildReaper_SkipsActiveWorkflow contract — this test locks the
// invariant against future refactors that might collapse the two paths).
func TestBuildReaper_RespectsM03GateWhenWorkflowRunningWithoutStuckPods(t *testing.T) {
	bs := newFakeBuildStoreReaper()
	bs.builds["build-slow"] = &db.Build{
		ID: "build-slow", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "RUNNING", WorkflowID: "wf-slow",
	}

	checker := &fakeStaleChecker{
		result: []StaleBuild{
			{ID: "build-slow", State: statemachine.BPhRunning, Event: statemachine.BPEventTimeout},
		},
	}

	// Phase=Running + no StuckPodReason → M-03 gate MUST hold, build is legitimately slow.
	wf := &fakeWorkflowReaper{
		statuses: map[string]*deploycontracts.WorkflowStatus{
			"wf-slow": {Phase: "Running"},
		},
	}

	reaper := NewBuildReaper(bs, checker, &fakeLockerReaper{}, wf, nil,
		slog.Default(), DefaultBuildReaperConfig(), &fakeReconcileChecker{})

	reaped := reaper.ReapOnce(context.Background())
	if reaped != 0 {
		t.Fatalf("expected 0 reaped (M-03 gate protects slow-but-progressing build), got %d", reaped)
	}
}

// containsAny reports whether s contains at least one of the substrings.
// Local helper — avoids pulling strings package into every assertion site.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
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
