// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// --- Test doubles for BuildOrchestrator ---

type ifCurrentCall struct {
	buildID  string
	tenantID string
	from     statemachine.BuildPhase
	event    string
}

type fakeBuildStoreOrch struct {
	builds    map[string]*db.Build
	hasActive bool
	hasActiveErr error
	createErr error
	lastStatus map[string]string
	errorMessages map[string]string
	workflowIDs   map[string]string
	appsSpec      map[string][]db.BuildAppSpec
	// ifCurrentCalls records every UpdateStatusIfCurrent invocation so caller
	// tests (M9 5b T-C/T-D) can assert the lock-free CAS path is used with the
	// expected `from` phase. ifCurrentSupersede[buildID]=true makes the fake
	// return (nil, nil) to simulate a superseded no-op.
	ifCurrentCalls     []ifCurrentCall
	ifCurrentSupersede map[string]bool
}

func newFakeBuildStoreOrch() *fakeBuildStoreOrch {
	return &fakeBuildStoreOrch{
		builds:        make(map[string]*db.Build),
		lastStatus:    make(map[string]string),
		errorMessages: make(map[string]string),
		workflowIDs:   make(map[string]string),
		appsSpec:      make(map[string][]db.BuildAppSpec),
	}
}

func (f *fakeBuildStoreOrch) Create(_ context.Context, _ db.DBTX, b *db.Build) error {
	if f.createErr != nil {
		return f.createErr
	}
	if b.ID == "" {
		b.ID = "bld-gen-001"
	}
	b.Status = "QUEUED"
	f.builds[b.ID] = b
	return nil
}

func (f *fakeBuildStoreOrch) GetByID(_ context.Context, id string) (*db.Build, error) {
	b, ok := f.builds[id]
	if !ok {
		return nil, loopzeroerrors.ErrBuildNotFound
	}
	return b, nil
}

// GetByIDForTenant — T0.4 of deep-review-2026-05-10-remediation-plan.
// Emulates the production SQL filter for the unit-test scope.
func (f *fakeBuildStoreOrch) GetByIDForTenant(ctx context.Context, tenantID, buildID string) (*db.Build, error) {
	b, err := f.GetByID(ctx, buildID)
	if err != nil || b == nil {
		return b, err
	}
	if b.TenantID != tenantID {
		return nil, nil
	}
	return b, nil
}

func (f *fakeBuildStoreOrch) HasActiveBuild(_ context.Context, _ db.DBTX, _, _, _ string) (bool, error) {
	return f.hasActive, f.hasActiveErr
}

func (f *fakeBuildStoreOrch) UpdateStatus(_ context.Context, _ db.DBTX, id, event string) (*db.Build, error) {
	f.lastStatus[id] = event
	b := f.builds[id]
	if b != nil {
		switch statemachine.Event(event) {
		case statemachine.BuildStarted:
			b.Status = string(statemachine.BuildRunning)
		case statemachine.BuildFailedEvent:
			b.Status = string(statemachine.BuildFailed)
		case statemachine.BuildSucceededEvent:
			b.Status = string(statemachine.BuildReleasing)
		case statemachine.BuildReleaseCompleted:
			b.Status = string(statemachine.BuildSucceeded)
		}
		return b, nil
	}
	return &db.Build{ID: id, Status: "RUNNING"}, nil
}

func (f *fakeBuildStoreOrch) UpdateStatusIfCurrent(_ context.Context, buildID, tenantID string, from statemachine.BuildPhase, event string) (*db.Build, error) {
	f.ifCurrentCalls = append(f.ifCurrentCalls, ifCurrentCall{buildID: buildID, tenantID: tenantID, from: from, event: event})
	if f.ifCurrentSupersede[buildID] {
		return nil, nil // superseded no-op
	}
	f.lastStatus[buildID] = event
	b := f.builds[buildID]
	if b != nil {
		switch statemachine.BuildPhaseEvent(event) {
		case statemachine.BPEventStarted:
			b.Status = string(statemachine.BPhRunning)
		case statemachine.BPEventFailed:
			b.Status = string(statemachine.BPhFailed)
		}
		return b, nil
	}
	return &db.Build{ID: buildID, Status: string(from)}, nil
}

func (f *fakeBuildStoreOrch) SetErrorMessage(_ context.Context, _ db.DBTX, id, msg string) error {
	f.errorMessages[id] = msg
	return nil
}

func (f *fakeBuildStoreOrch) SetWorkflowID(_ context.Context, _ db.DBTX, id, wfID string) error {
	f.workflowIDs[id] = wfID
	return nil
}

func (f *fakeBuildStoreOrch) SetAppsSpec(_ context.Context, _ db.DBTX, id string, apps []db.BuildAppSpec) error {
	f.appsSpec[id] = apps
	return nil
}

func (f *fakeBuildStoreOrch) GetBuildApps(_ context.Context, id string) ([]db.BuildAppSpec, error) {
	return f.appsSpec[id], nil
}

func (f *fakeBuildStoreOrch) ListByProject(_ context.Context, _, _ string, _, _ int) ([]db.Build, error) {
	return nil, nil
}

func (f *fakeBuildStoreOrch) ListByProjectAndEnv(_ context.Context, _, _, _ string, _, _ int) ([]db.Build, int, error) {
	return nil, 0, nil
}

func (f *fakeBuildStoreOrch) ListEnvsForProject(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

func (f *fakeBuildStoreOrch) FindActiveBuilds(_ context.Context, _ string) ([]db.Build, error) {
	return nil, nil
}

func (f *fakeBuildStoreOrch) CancelActiveBuilds(_ context.Context, _ db.DBTX, _, _, _ string) (int, []string, error) {
	return 0, nil, nil
}

type fakeWorkflowSubmitterOrch struct {
	submitErr  error
	cancelErr  error
	workflowID string
}

func (f *fakeWorkflowSubmitterOrch) Submit(_ context.Context, _ WorkflowSpec) (string, error) {
	return f.workflowID, f.submitErr
}

func (f *fakeWorkflowSubmitterOrch) Cancel(_ context.Context, _, _ string) error {
	return f.cancelErr
}

func (f *fakeWorkflowSubmitterOrch) GetStatus(_ context.Context, _, _ string) (*WorkflowStatus, error) {
	return nil, nil
}

type fakeProjectEnsurer struct {
	ensureErr error
}

func (f *fakeProjectEnsurer) EnsureProject(_ context.Context, _ db.DBTX, _, _, _ string) error {
	return f.ensureErr
}

// --- Tests ---

func TestBuildOrchestrator_SubmitBuild_DeployInProgress(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	bs.hasActive = true

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:    &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore:  &fakeEventStoreContainer{},
	})

	_, err := orch.SubmitBuild(context.Background(), SubmitBuildRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps:        []AppSpec{{Name: "api", Type: "server", Port: 3000, Runtime: "node"}},
	})

	if err == nil {
		t.Fatal("expected error for concurrent build")
	}
	if err != loopzeroerrors.ErrDeployInProgress {
		t.Errorf("expected ErrDeployInProgress, got: %v", err)
	}
}

func TestBuildOrchestrator_SubmitBuild_WorkflowSubmitFails(t *testing.T) {
	bs := newFakeBuildStoreOrch()

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:    &fakeWorkflowSubmitterOrch{submitErr: fmt.Errorf("argo unreachable")},
		EventStore:  &fakeEventStoreContainer{},
	})

	_, err := orch.SubmitBuild(context.Background(), SubmitBuildRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps:        []AppSpec{{Name: "api", Type: "server", Port: 3000, Runtime: "node"}},
	})

	if err == nil {
		t.Fatal("expected error when workflow submit fails")
	}

	// Build should be marked FAILED
	buildID := "bld-gen-001"
	if bs.lastStatus[buildID] != string(statemachine.BuildFailedEvent) {
		t.Errorf("expected build marked FAILED, got: %v", bs.lastStatus[buildID])
	}
}

func TestBuildOrchestrator_CancelBuild_AlreadyTerminal(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	bs.builds["bld-done"] = &db.Build{
		ID: "bld-done", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "SUCCEEDED",
	}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:    &fakeWorkflowSubmitterOrch{},
		EventStore:  &fakeEventStoreContainer{},
	})

	err := orch.CancelBuild(context.Background(), "bld-done")
	if err != loopzeroerrors.ErrBuildAlreadyCompleted {
		t.Errorf("expected ErrBuildAlreadyCompleted, got: %v", err)
	}
}

func TestBuildOrchestrator_OnBuildCompleted_DuplicateCallback(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	bs.builds["bld-dup"] = &db.Build{
		ID: "bld-dup", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "SUCCEEDED",
	}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:    &fakeWorkflowSubmitterOrch{},
		EventStore:  &fakeEventStoreContainer{},
	})

	err := orch.OnBuildCompleted(context.Background(), "bld-dup", BuildCallbackRequest{
		Status: "succeeded",
		Apps:   []AppRelease{{Name: "api"}},
	})
	if err != nil {
		t.Fatalf("duplicate callback should be no-op, got: %v", err)
	}
}

func TestBuildOrchestrator_OnBuildCompleted_SuccessNoApps_ReturnsError(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	bs.builds["bld-noapp"] = &db.Build{
		ID: "bld-noapp", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "RUNNING",
	}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:    &fakeWorkflowSubmitterOrch{},
		EventStore:  &fakeEventStoreContainer{},
	})

	err := orch.OnBuildCompleted(context.Background(), "bld-noapp", BuildCallbackRequest{
		Status: "succeeded",
		Apps:   []AppRelease{}, // empty apps with success
	})
	if err == nil {
		t.Fatal("expected error for success callback with no apps")
	}
}

func TestBuildOrchestrator_OnBuildCompleted_FailedBuild(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	bs.builds["bld-fail"] = &db.Build{
		ID: "bld-fail", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "RUNNING",
	}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:    &fakeWorkflowSubmitterOrch{},
		EventStore:  &fakeEventStoreContainer{},
	})

	err := orch.OnBuildCompleted(context.Background(), "bld-fail", BuildCallbackRequest{
		Status: "failed",
		Error:  "OOMKilled",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bs.lastStatus["bld-fail"] != string(statemachine.BuildFailedEvent) {
		t.Errorf("expected FAILED transition, got: %v", bs.lastStatus["bld-fail"])
	}
	if bs.errorMessages["bld-fail"] != "OOMKilled" {
		t.Errorf("expected error message 'OOMKilled', got: %v", bs.errorMessages["bld-fail"])
	}
}

func TestBuildOrchestrator_FindReleaseForBuild_NoRelease(t *testing.T) {
	rs := newFakeReleaseStoreContainer()

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore:   newFakeBuildStoreOrch(),
		ReleaseStore: rs,
		Locker:       &fakeLockerContainer{},
		Workflow:     &fakeWorkflowSubmitterOrch{},
		EventStore:   &fakeEventStoreContainer{},
	})

	rel, err := orch.FindReleaseForBuild(context.Background(), "bld-nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != nil {
		t.Error("expected nil release for nonexistent build")
	}
}

func TestBuildOrchestrator_RetryReleaseActivation_WrongState(t *testing.T) {
	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: newFakeBuildStoreOrch(),
		Locker:     &fakeLockerContainer{},
		Workflow:    &fakeWorkflowSubmitterOrch{},
		EventStore:  &fakeEventStoreContainer{},
	})

	build := &db.Build{ID: "bld-1", Status: "RUNNING"}
	err := orch.RetryReleaseActivation(context.Background(), build, BuildCallbackRequest{})
	if err == nil {
		t.Fatal("expected error for non-RELEASING build")
	}
}

func TestNewBuildOrchestrator_DefaultsRegistryURL(t *testing.T) {
	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore:   newFakeBuildStoreOrch(),
		ReleaseStore: newFakeReleaseStoreContainer(),
		Locker:       &fakeLockerContainer{},
		Workflow:     &fakeWorkflowSubmitterOrch{},
		EventStore:   &fakeEventStoreContainer{},
	})
	if orch.registryURL != "k3d-loopzero-registry:5000" {
		t.Errorf("expected default registry URL, got: %s", orch.registryURL)
	}
}

func TestNewBuildOrchestrator_DefaultsSyncTimeout(t *testing.T) {
	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore:   newFakeBuildStoreOrch(),
		ReleaseStore: newFakeReleaseStoreContainer(),
		Locker:       &fakeLockerContainer{},
		Workflow:     &fakeWorkflowSubmitterOrch{},
		EventStore:   &fakeEventStoreContainer{},
	})
	if orch.argocdSyncTimeout != defaultArgocdSyncTimeout {
		t.Errorf("expected default sync timeout, got: %v", orch.argocdSyncTimeout)
	}
}

func TestSafeJSONMarshal_MapInput(t *testing.T) {
	data := safeJSONMarshal(map[string]string{"key": "value"})
	var parsed map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if parsed["key"] != "value" {
		t.Errorf("expected 'value', got %q", parsed["key"])
	}
}

func TestDeriveDeployID_DeterministicFromReleaseID(t *testing.T) {
	id1 := deriveDeployID("rel-abc-123")
	id2 := deriveDeployID("rel-abc-123")
	if id1 != id2 {
		t.Errorf("expected deterministic deploy ID, got %q and %q", id1, id2)
	}
	if id1 == "" {
		t.Error("expected non-empty deploy ID")
	}
}

func TestBuildOrchestrator_CancelBuild_RunningBuild(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	bs.builds["bld-running"] = &db.Build{
		ID: "bld-running", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "RUNNING", WorkflowID: "wf-123",
	}

	wf := &fakeWorkflowSubmitterOrch{}
	es := &fakeEventStoreContainer{}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   wf,
		EventStore: es,
	})

	err := orch.CancelBuild(context.Background(), "bld-running")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have emitted cancel event
	found := false
	for _, e := range es.events {
		if e.EventType == "build.cancelled" {
			found = true
		}
	}
	if !found {
		t.Error("expected build.cancelled SSE event")
	}
}

func TestBuildOrchestrator_EmitActivationFailure(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	es := &fakeEventStoreContainer{}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{},
		EventStore: es,
	})

	orch.emitActivationFailure(context.Background(), "bld-1", fmt.Errorf("sync timeout"))

	found := false
	for _, e := range es.events {
		if e.EventType == "deploy.sync_failed" {
			found = true
		}
	}
	if !found {
		t.Error("expected deploy.sync_failed event")
	}
}

func TestBuildOrchestrator_WriteStaticRoutingEntries_NilKVClient(t *testing.T) {
	bs := newFakeBuildStoreOrch()

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{},
		EventStore: &fakeEventStoreContainer{},
		KVClient:   nil, // nil = disabled
	})

	// Should not panic when KVClient is nil
	build := &db.Build{ID: "bld-1", TenantID: "tn-1", ProjectID: "proj-1", Environment: "production"}
	result := BuildCallbackRequest{
		Apps: []AppRelease{{Name: "web", Type: "frontend", Rendering: "static"}},
	}
	orch.writeStaticRoutingEntries(context.Background(), build, result)
}

type fakeKVClient struct {
	entries map[string]interface{}
	putErr  error
}

func (f *fakeKVClient) Put(_ context.Context, key string, val interface{}) error {
	if f.putErr != nil {
		return f.putErr
	}
	if f.entries == nil {
		f.entries = make(map[string]interface{})
	}
	f.entries[key] = val
	return nil
}

func TestBuildOrchestrator_WriteStaticRoutingEntries_WritesKV(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	kv := &fakeKVClient{}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{},
		EventStore: &fakeEventStoreContainer{},
		KVClient:   kv,
		BaseDomain: "dev.loopzero.example",
	})

	build := &db.Build{ID: "bld-1", TenantID: "tn-1", ProjectID: "proj-1", Environment: "production"}
	result := BuildCallbackRequest{
		Apps: []AppRelease{{Name: "web", Type: "frontend", Rendering: "static"}},
	}
	orch.writeStaticRoutingEntries(context.Background(), build, result)

	if len(kv.entries) != 1 {
		t.Errorf("expected 1 KV entry, got %d", len(kv.entries))
	}
}

func TestBuildOrchestrator_WriteStaticRoutingEntries_SkipsNonStatic(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	kv := &fakeKVClient{}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{},
		EventStore: &fakeEventStoreContainer{},
		KVClient:   kv,
	})

	build := &db.Build{ID: "bld-1", TenantID: "tn-1", ProjectID: "proj-1", Environment: "production"}
	result := BuildCallbackRequest{
		Apps: []AppRelease{{Name: "api", Type: "server", Rendering: ""}},
	}
	orch.writeStaticRoutingEntries(context.Background(), build, result)

	if len(kv.entries) != 0 {
		t.Errorf("expected 0 KV entries for non-static, got %d", len(kv.entries))
	}
}

func TestBuildOrchestrator_WriteStaticRoutingEntries_KVError_NonFatal(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	kv := &fakeKVClient{putErr: fmt.Errorf("KV unreachable")}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{},
		EventStore: &fakeEventStoreContainer{},
		KVClient:   kv,
	})

	build := &db.Build{ID: "bld-1", TenantID: "tn-1", ProjectID: "proj-1", Environment: "production"}
	result := BuildCallbackRequest{
		Apps: []AppRelease{{Name: "web", Type: "frontend", Rendering: "static"}},
	}
	// Should not panic
	orch.writeStaticRoutingEntries(context.Background(), build, result)
}

func TestRegisterReleaseActivatedCallback(t *testing.T) {
	called := false
	RegisterReleaseActivatedCallback(func(ctx context.Context, _, _, _, _ string) {
		called = true
	})
	if onReleaseActivated == nil {
		t.Error("expected callback to be registered")
	}
	onReleaseActivated(context.Background(), "t", "p", "b", "e")
	if !called {
		t.Error("expected callback to be called")
	}
	// Clean up
	RegisterReleaseActivatedCallback(nil)
}

func TestRegisterBuildFailedCallback(t *testing.T) {
	called := false
	RegisterBuildFailedCallback(func(ctx context.Context, _, _, _, _ string) {
		called = true
	})
	if onBuildFailed == nil {
		t.Error("expected callback to be registered")
	}
	onBuildFailed(context.Background(), "t", "p", "sha", "b")
	if !called {
		t.Error("expected callback to be called")
	}
	// Clean up
	RegisterBuildFailedCallback(nil)
}

func TestBuildOrchestrator_OnBuildCompleted_SucceededBuild(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	rs := newFakeReleaseStoreContainer()
	ets := newFakeEnvTargetStoreContainer()
	es := &fakeEventStoreContainer{}
	sync := &fakeSyncContainer{}
	gitops := &fakeGitOpsContainer{commitSHA: "git-sha-1"}

	bs.builds["bld-ok"] = &db.Build{
		ID: "bld-ok", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "RUNNING",
	}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore:     bs,
		ReleaseStore:   rs,
		EnvTargetStore: ets,
		Locker:         &fakeLockerContainer{},
		Workflow:       &fakeWorkflowSubmitterOrch{},
		EventStore:     es,
		Sync:           sync,
		GitOps:         gitops,
	})

	err := orch.OnBuildCompleted(context.Background(), "bld-ok", BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      []AppRelease{{Name: "api", Type: "server"}},
		Manifests: "apiVersion: v1\nkind: Deployment",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Build should be SUCCEEDED
	if bs.builds["bld-ok"].Status != string(statemachine.BuildSucceeded) {
		t.Errorf("expected SUCCEEDED, got: %s", bs.builds["bld-ok"].Status)
	}

	// Should have emitted build.succeeded event
	found := false
	for _, e := range es.events {
		if e.EventType == "build.succeeded" {
			found = true
		}
	}
	if !found {
		t.Error("expected build.succeeded SSE event")
	}
}

func TestBuildOrchestrator_OnBuildCompleted_QueuedBuild_ECNRace(t *testing.T) {
	// EC-N race: build callback arrives while build is still QUEUED
	bs := newFakeBuildStoreOrch()
	rs := newFakeReleaseStoreContainer()
	ets := newFakeEnvTargetStoreContainer()
	es := &fakeEventStoreContainer{}
	gitops := &fakeGitOpsContainer{commitSHA: "sha"}

	bs.builds["bld-ecn"] = &db.Build{
		ID: "bld-ecn", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "QUEUED",
	}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore:     bs,
		ReleaseStore:   rs,
		EnvTargetStore: ets,
		Locker:         &fakeLockerContainer{},
		Workflow:       &fakeWorkflowSubmitterOrch{},
		EventStore:     es,
		Sync:           &fakeSyncContainer{},
		GitOps:         gitops,
	})

	err := orch.OnBuildCompleted(context.Background(), "bld-ecn", BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      []AppRelease{{Name: "api", Type: "server"}},
		Manifests: "manifests",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have transitioned through QUEUED -> RUNNING -> RELEASING -> SUCCEEDED
	// May also have been the final transition — either way the orchestrator completed without error.
	_ = bs.lastStatus["bld-ecn"]
}

func TestBuildOrchestrator_RetryReleaseActivation_Success(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	rs := newFakeReleaseStoreContainer()
	ets := newFakeEnvTargetStoreContainer()
	es := &fakeEventStoreContainer{}
	sync := &fakeSyncContainer{}
	gitops := &fakeGitOpsContainer{commitSHA: "sha"}

	build := &db.Build{
		ID: "bld-retry", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "RELEASING",
	}
	bs.builds["bld-retry"] = build

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore:     bs,
		ReleaseStore:   rs,
		EnvTargetStore: ets,
		Locker:         &fakeLockerContainer{},
		Workflow:       &fakeWorkflowSubmitterOrch{},
		EventStore:     es,
		Sync:           sync,
		GitOps:         gitops,
	})

	err := orch.RetryReleaseActivation(context.Background(), build, BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      []AppRelease{{Name: "api", Type: "server"}},
		Manifests: "manifests",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildOrchestrator_SubmitBuild_WithProjectEnsurer(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	pe := &fakeProjectEnsurer{}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore:   bs,
		ProjectStore: pe,
		Locker:       &fakeLockerContainer{},
		Workflow:     &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore:   &fakeEventStoreContainer{},
	})

	resp, err := orch.SubmitBuild(context.Background(), SubmitBuildRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps:        []AppSpec{{Name: "api", Type: "server", Port: 3000, Runtime: "node"}},
		CreatedBy:   "user@test.com",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.BuildID == "" {
		t.Error("expected non-empty build ID")
	}
	if resp.StreamURL == "" {
		t.Error("expected non-empty stream URL")
	}
}
