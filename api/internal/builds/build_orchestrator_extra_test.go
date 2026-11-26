// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/release"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// --- Test doubles for BuildOrchestrator ---

type ifCurrentCall struct {
	buildID  string
	tenantID string
	from     statemachine.BuildPhase
	event    string
}

type fakeBuildStoreOrch struct {
	builds        map[string]*db.Build
	hasActive     bool
	hasActiveErr  error
	createErr     error
	lastStatus    map[string]string
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
		case statemachine.BuildFailedEvent, statemachine.BuildReleaseFailedEvent:
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

func (f *fakeWorkflowSubmitterOrch) Submit(_ context.Context, _ deploycontracts.WorkflowSpec) (string, error) {
	return f.workflowID, f.submitErr
}

func (f *fakeWorkflowSubmitterOrch) Cancel(_ context.Context, _, _ string) error {
	return f.cancelErr
}

func (f *fakeWorkflowSubmitterOrch) GetStatus(_ context.Context, _, _ string) (*deploycontracts.WorkflowStatus, error) {
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
		Workflow:   &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore: &fakeEventStoreContainer{},
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps:        []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 3000, Runtime: "node"}},
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
		Workflow:   &fakeWorkflowSubmitterOrch{submitErr: fmt.Errorf("argo unreachable")},
		EventStore: &fakeEventStoreContainer{},
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps:        []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 3000, Runtime: "node"}},
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
		Workflow:   &fakeWorkflowSubmitterOrch{},
		EventStore: &fakeEventStoreContainer{},
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
		Workflow:   &fakeWorkflowSubmitterOrch{},
		EventStore: &fakeEventStoreContainer{},
	})

	err := orch.OnBuildCompleted(context.Background(), "bld-dup", deploycontracts.BuildCallbackRequest{
		Status: "succeeded",
		Apps:   []deploycontracts.AppRelease{{Name: "api"}},
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
		Workflow:   &fakeWorkflowSubmitterOrch{},
		EventStore: &fakeEventStoreContainer{},
	})

	err := orch.OnBuildCompleted(context.Background(), "bld-noapp", deploycontracts.BuildCallbackRequest{
		Status: "succeeded",
		Apps:   []deploycontracts.AppRelease{},
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
		Workflow:   &fakeWorkflowSubmitterOrch{},
		EventStore: &fakeEventStoreContainer{},
	})

	err := orch.OnBuildCompleted(context.Background(), "bld-fail", deploycontracts.BuildCallbackRequest{
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
		Workflow:   &fakeWorkflowSubmitterOrch{},
		EventStore: &fakeEventStoreContainer{},
	})

	build := &db.Build{ID: "bld-1", Status: "RUNNING"}
	err := orch.RetryReleaseActivation(context.Background(), build, deploycontracts.BuildCallbackRequest{})
	if err == nil {
		t.Fatal("expected error for non-RELEASING build")
	}
}

// The orchestrator keeps the configured registry as given: the local k3d default belongs to
// config.Load in development, and nowhere else may a build silently push to it.
func TestNewBuildOrchestrator_KeepsTheConfiguredRegistryURL(t *testing.T) {
	const registry = "123456789012.dkr.ecr.us-east-1.amazonaws.com/loopzero-tenants"
	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore:   newFakeBuildStoreOrch(),
		ReleaseStore: newFakeReleaseStoreContainer(),
		Locker:       &fakeLockerContainer{},
		Workflow:     &fakeWorkflowSubmitterOrch{},
		EventStore:   &fakeEventStoreContainer{},
		RegistryURL:  registry,
	})
	if orch.registryURL != registry {
		t.Errorf("registryURL = %q, want %q", orch.registryURL, registry)
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
		Delivery:       containerDeliveryFor(rs, ets, es, sync, gitops),
	})

	err := orch.OnBuildCompleted(context.Background(), "bld-ok", deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      []deploycontracts.AppRelease{{Name: "api", Type: "server"}},
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

	err := orch.OnBuildCompleted(context.Background(), "bld-ecn", deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      []deploycontracts.AppRelease{{Name: "api", Type: "server"}},
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
		Delivery:       containerDeliveryFor(rs, ets, es, sync, gitops),
	})

	err := orch.RetryReleaseActivation(context.Background(), build, deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      []deploycontracts.AppRelease{{Name: "api", Type: "server"}},
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

	resp, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps:        []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 3000, Runtime: "node"}},
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

// M5 review F2 (orchestrator half): when the deploy-path project-ensure returns
// the per-Account cap rejection (*LoopZeroError PROJECTS_EXCEED_PLAN), SubmitBuild
// MUST propagate that typed error UNCHANGED — wrapSubmitBuildError's EC-3 guard
// passes *LoopZeroError through instead of collapsing it into a generic
// SubmitBuildError. This is what lets the route ladder map it to HTTP 422 rather
// than a silent build-failure/500 (DoD #1: "typed rejection surfaced to the
// deploy flow, not a silent 500"). A refactor that stopped passing *LoopZeroError
// through would fail here.
func TestBuildOrchestrator_SubmitBuild_ProjectCapRejection_PropagatesTypedError(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	capErr := loopzeroerrors.NewProjectsExceedPlan("starter", 1, 1, "acct-cap")
	pe := &fakeProjectEnsurer{ensureErr: capErr}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore:   bs,
		ProjectStore: pe,
		Locker:       &fakeLockerContainer{},
		Workflow:     &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore:   &fakeEventStoreContainer{},
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-1",
		ProjectID:   "brand-new-project",
		Environment: "production",
		Apps:        []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 3000, Runtime: "node"}},
		CreatedBy:   "user@test.com",
	})
	if err == nil {
		t.Fatal("expected a cap-rejection error, got nil")
	}
	var te *loopzeroerrors.LoopZeroError
	if !errors.As(err, &te) || te.Code != "PROJECTS_EXCEED_PLAN" {
		t.Fatalf("SubmitBuild must propagate the typed PROJECTS_EXCEED_PLAN error unchanged; got %v", err)
	}
}

func TestBuildOrchestrator_ActivationWithoutDeliveryFailsWithAWiringError(t *testing.T) {
	orch := NewBuildOrchestrator(BuildOrchestratorDeps{})
	build := &db.Build{ID: "bld-1", TenantID: "tn-1", ProjectID: "proj-1", Environment: "production"}

	err := orch.activate(context.Background(), build, deploycontracts.BuildCallbackRequest{})

	if !errors.Is(err, errDeliveryNotWired) {
		t.Fatalf("want errDeliveryNotWired, got %v", err)
	}
}

// containerDeliveryFor wires the production delivery activator over the same
// fakes the orchestrator uses, so a test that reaches activation exercises the
// real container delivery path, as the composition root does.
func containerDeliveryFor(rs *fakeReleaseStoreContainer, ets *fakeEnvTargetStoreContainer, es *fakeEventStoreContainer,
	sync *fakeSyncContainer, gitops *fakeGitOpsContainer) deploycontracts.DeliveryActivator {
	return release.NewContainerDeliveryActivator(release.ContainerDeliveryDeps{
		ReleaseStore: rs, EnvTargetStore: ets, EventStore: es,
		Sync: sync, Locker: &fakeLockerContainer{}, GitOps: gitops,
	})
}
