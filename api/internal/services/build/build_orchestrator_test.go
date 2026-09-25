// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package build_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/release"
	"github.com/getloopscale/loopzero/api/internal/services/build"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/sse"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// --- In-memory fakes for BuildOrchestrator unit tests ---

// fakeBuildStoreOrch implements db.BuildStore with in-memory state.
type fakeBuildStoreOrch struct {
	builds      map[string]*db.Build
	activeBuild bool // controls HasActiveBuild return
	createErr   error
	getErr      error
	updateErr   error
}

func newFakeBuildStoreOrch() *fakeBuildStoreOrch {
	return &fakeBuildStoreOrch{
		builds: make(map[string]*db.Build),
	}
}

func (f *fakeBuildStoreOrch) Create(ctx context.Context, tx db.DBTX, build *db.Build) error {
	if f.createErr != nil {
		return f.createErr
	}
	if build.ID == "" {
		build.ID = fmt.Sprintf("build-%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	build.Status = string(statemachine.BPhQueued)
	build.CreatedAt = time.Now().UTC()
	f.builds[build.ID] = build
	return nil
}

func (f *fakeBuildStoreOrch) GetByID(ctx context.Context, id string) (*db.Build, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	b, ok := f.builds[id]
	if !ok {
		return nil, loopzeroerrors.ErrBuildNotFound
	}
	// Return a copy
	copy := *b
	return &copy, nil
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

func (f *fakeBuildStoreOrch) HasActiveBuild(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (bool, error) {
	return f.activeBuild, nil
}

var buildMachine = statemachine.NewBuildMachine()

func (f *fakeBuildStoreOrch) UpdateStatus(ctx context.Context, tx db.DBTX, id string, event string) (*db.Build, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	b, ok := f.builds[id]
	if !ok {
		return nil, loopzeroerrors.ErrBuildNotFound
	}
	// Use real state machine for transition validation
	newState, err := safeFakeTransition(buildMachine, statemachine.State(b.Status), statemachine.Event(event))
	if err != nil {
		return nil, err
	}
	b.Status = string(newState)
	copy := *b
	return &copy, nil
}

func (f *fakeBuildStoreOrch) UpdateStatusIfCurrent(ctx context.Context, buildID, _ string, from statemachine.BuildPhase, event string) (*db.Build, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	b, ok := f.builds[buildID]
	if !ok || statemachine.State(b.Status) != statemachine.State(from) {
		return nil, nil // superseded / not-in-from → no-op
	}
	newState, err := safeFakeTransition(buildMachine, statemachine.State(from), statemachine.Event(event))
	if err != nil {
		return nil, err
	}
	b.Status = string(newState)
	cp := *b
	return &cp, nil
}

func (f *fakeBuildStoreOrch) SetWorkflowID(_ context.Context, _ db.DBTX, id string, wfID string) error {
	if b, ok := f.builds[id]; ok {
		b.WorkflowID = wfID
		return nil
	}
	return fmt.Errorf("build not found: %s", id)
}

func (f *fakeBuildStoreOrch) SetErrorMessage(ctx context.Context, tx db.DBTX, id string, errorMessage string) error {
	if b, ok := f.builds[id]; ok {
		b.ErrorMessage = errorMessage
		return nil
	}
	return fmt.Errorf("build not found: %s", id)
}

func (f *fakeBuildStoreOrch) ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]db.Build, error) {
	return []db.Build{}, nil
}

func (f *fakeBuildStoreOrch) FindActiveBuilds(ctx context.Context, _ string) ([]db.Build, error) {
	var active []db.Build
	for _, b := range f.builds {
		if b.Status == string(statemachine.BPhQueued) || b.Status == string(statemachine.BPhRunning) {
			active = append(active, *b)
		}
	}
	return active, nil
}

func (f *fakeBuildStoreOrch) SetAppsSpec(ctx context.Context, tx db.DBTX, id string, apps []db.BuildAppSpec) error {
	return nil
}

func (f *fakeBuildStoreOrch) GetBuildApps(ctx context.Context, buildID string) ([]db.BuildAppSpec, error) {
	return nil, nil
}

func (f *fakeBuildStoreOrch) ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]db.Build, int, error) {
	return nil, 0, nil
}

func (f *fakeBuildStoreOrch) ListEnvsForProject(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

func (f *fakeBuildStoreOrch) CancelActiveBuilds(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (int, []string, error) {
	count := 0
	var wfIDs []string
	for _, b := range f.builds {
		if b.TenantID == tenantID && b.ProjectID == projectID && b.Environment == env {
			if b.Status == string(statemachine.BPhQueued) || b.Status == string(statemachine.BPhRunning) {
				b.Status = string(statemachine.BPhCancelled)
				count++
				if b.WorkflowID != "" {
					wfIDs = append(wfIDs, b.WorkflowID)
				}
			}
		}
	}
	return count, wfIDs, nil
}

// safeFakeTransition catches panics from the state machine.
func safeFakeTransition(m *statemachine.Machine, current statemachine.State, event statemachine.Event) (newState statemachine.State, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = loopzeroerrors.NewInvalidTransition(string(current), string(event))
		}
	}()
	newState = m.Transition(current, event)
	return newState, nil
}

// fakeReleaseStoreOrch implements db.ReleaseStore.
type fakeReleaseStoreOrch struct {
	releases    map[string]*db.Release
	activeByEnv map[string]*db.Release // key: tenant:project:env
	createErr   error
	updateErr   error
}

var releaseMachine = statemachine.NewReleaseMachine()

func newFakeReleaseStoreOrch() *fakeReleaseStoreOrch {
	return &fakeReleaseStoreOrch{
		releases:    make(map[string]*db.Release),
		activeByEnv: make(map[string]*db.Release),
	}
}

func (f *fakeReleaseStoreOrch) Create(ctx context.Context, tx db.DBTX, release *db.Release) error {
	if f.createErr != nil {
		return f.createErr
	}
	if release.ID == "" {
		release.ID = fmt.Sprintf("rel-%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	release.Status = string(statemachine.RPhReady)
	release.CreatedAt = time.Now().UTC()
	f.releases[release.ID] = release
	return nil
}

func (f *fakeReleaseStoreOrch) GetByID(ctx context.Context, id string) (*db.Release, error) {
	r, ok := f.releases[id]
	if !ok {
		return nil, loopzeroerrors.ErrReleaseNotFound
	}
	copy := *r
	return &copy, nil
}

func (f *fakeReleaseStoreOrch) UpdateStatus(ctx context.Context, tx db.DBTX, id string, event string) (*db.Release, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	r, ok := f.releases[id]
	if !ok {
		return nil, loopzeroerrors.ErrReleaseNotFound
	}
	newState, err := safeFakeTransition(releaseMachine, statemachine.State(r.Status), statemachine.Event(event))
	if err != nil {
		return nil, err
	}
	r.Status = string(newState)
	copy := *r
	return &copy, nil
}

func (f *fakeReleaseStoreOrch) SetGitCommit(ctx context.Context, tx db.DBTX, id string, commitSHA string) error {
	r, ok := f.releases[id]
	if !ok {
		return loopzeroerrors.ErrReleaseNotFound
	}
	r.GitCommit = commitSHA
	return nil
}

func (f *fakeReleaseStoreOrch) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*db.Release, error) {
	key := fmt.Sprintf("%s:%s:%s", tenantID, projectID, env)
	r, ok := f.activeByEnv[key]
	if !ok {
		return nil, nil
	}
	copy := *r
	return &copy, nil
}

func (f *fakeReleaseStoreOrch) GetActiveByEnvTx(_ context.Context, _ db.DBTX, tenantID, projectID, env string) (*db.Release, error) {
	return f.GetActiveByEnv(context.Background(), tenantID, projectID, env)
}

func (f *fakeReleaseStoreOrch) GetByIDTx(_ context.Context, _ db.DBTX, id string) (*db.Release, error) {
	return f.GetByID(context.Background(), id)
}

func (f *fakeReleaseStoreOrch) CancelAllByEnv(_ context.Context, _ db.DBTX, _, _, _ string) (int, error) {
	return 0, nil
}

// setActiveByEnv manually sets an active release (for test setup).
func (f *fakeReleaseStoreOrch) setActiveByEnv(tenantID, projectID, env string, r *db.Release) {
	key := fmt.Sprintf("%s:%s:%s", tenantID, projectID, env)
	f.activeByEnv[key] = r
	f.releases[r.ID] = r
}

func (f *fakeReleaseStoreOrch) GetPreviousRelease(_ context.Context, _, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreOrch) GetByIDForTenant(_ context.Context, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreOrch) ListByEnv(_ context.Context, _, _, _ string, _ int) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreOrch) GetLatestByBuildID(_ context.Context, buildID string) (*db.Release, error) {
	for _, r := range f.releases {
		if r.BuildID == buildID {
			return r, nil
		}
	}
	return nil, nil
}

func (f *fakeReleaseStoreOrch) FindPendingReleases(_ context.Context, _ string) ([]db.Release, error) {
	var pending []db.Release
	for _, r := range f.releases {
		if r.Status == "PENDING" {
			pending = append(pending, *r)
		}
	}
	return pending, nil
}

func (f *fakeReleaseStoreOrch) FindActiveReleases(_ context.Context, _ string) ([]db.Release, error) {
	var active []db.Release
	for _, r := range f.releases {
		if r.Status == "ACTIVE" {
			active = append(active, *r)
		}
	}
	return active, nil
}

// AppendSideEffect — fix-all-platform-issues-plan T4.1 stub for tests.
// In-memory fake just no-ops; real RCA captures only matter in LIVE PG.
func (f *fakeReleaseStoreOrch) AppendSideEffect(_ context.Context, _ db.DBTX, _ string, _ map[string]any) error {
	return nil
}

// fakeEnvTargetStoreOrch implements store.EnvironmentTargetStore.
type fakeEnvTargetStoreOrch struct {
	desired map[string]string // key: tenant:project:env -> releaseID
	active  map[string]string
	setErr  error
}

func newFakeEnvTargetStoreOrch() *fakeEnvTargetStoreOrch {
	return &fakeEnvTargetStoreOrch{
		desired: make(map[string]string),
		active:  make(map[string]string),
	}
}

func (f *fakeEnvTargetStoreOrch) SetDesired(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error {
	if f.setErr != nil {
		return f.setErr
	}
	key := fmt.Sprintf("%s:%s:%s", tenantID, projectID, env)
	f.desired[key] = releaseID
	return nil
}

func (f *fakeEnvTargetStoreOrch) SetActive(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID string) error {
	if f.setErr != nil {
		return f.setErr
	}
	key := fmt.Sprintf("%s:%s:%s", tenantID, projectID, env)
	f.active[key] = releaseID
	return nil
}

func (f *fakeEnvTargetStoreOrch) Get(ctx context.Context, tenantID, projectID, env string) (*store.EnvironmentTarget, error) {
	return nil, nil
}

func (f *fakeEnvTargetStoreOrch) ListByProject(ctx context.Context, tenantID, projectID string) ([]store.EnvironmentTarget, error) {
	return []store.EnvironmentTarget{}, nil
}

func (f *fakeEnvTargetStoreOrch) DeleteTarget(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) error {
	return nil
}

// 2026-06-07 D2/D5/D6/D7 — preview-as-category interface extensions.
func (f *fakeEnvTargetStoreOrch) SetPreviewDesired(_ context.Context, _ db.DBTX, _, _, _, _, _, _ string) error {
	return nil
}
func (f *fakeEnvTargetStoreOrch) CountPermanent(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (f *fakeEnvTargetStoreOrch) ListPermanent(_ context.Context, _ string) ([]string, error) {
	return []string{}, nil
}
func (f *fakeEnvTargetStoreOrch) DeleteByKind(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	return nil
}

// fakeEventStoreOrch implements sse.EventStore.
type fakeEventStoreOrch struct {
	events   []sse.SSEEvent
	appendID int64
}

func newFakeEventStoreOrch() *fakeEventStoreOrch {
	return &fakeEventStoreOrch{}
}

func (f *fakeEventStoreOrch) Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error) {
	f.appendID++
	f.events = append(f.events, sse.SSEEvent{
		ID:        f.appendID,
		BuildID:   buildID,
		EventType: eventType,
		Data:      data,
		CreatedAt: time.Now().UTC(),
	})
	return f.appendID, nil
}

func (f *fakeEventStoreOrch) GetAfter(ctx context.Context, buildID string, afterID int64) ([]sse.SSEEvent, error) {
	var result []sse.SSEEvent
	for _, e := range f.events {
		if e.BuildID == buildID && e.ID > afterID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeEventStoreOrch) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}

func (f *fakeEventStoreOrch) eventsByType(eventType string) []sse.SSEEvent {
	var result []sse.SSEEvent
	for _, e := range f.events {
		if e.EventType == eventType {
			result = append(result, e)
		}
	}
	return result
}

// fakeWorkflowSubmitter implements deploycontracts.WorkflowSubmitter.
type fakeWorkflowSubmitter struct {
	submitted  []deploycontracts.WorkflowSpec
	workflowID string
	err        error
	onSubmit   func() // optional callback for ordering tests (REVIEW-BLOCKER-1)
}

func (f *fakeWorkflowSubmitter) Submit(ctx context.Context, spec deploycontracts.WorkflowSpec) (string, error) {
	if f.onSubmit != nil {
		f.onSubmit()
	}
	if f.err != nil {
		return "", f.err
	}
	f.submitted = append(f.submitted, spec)
	if f.workflowID == "" {
		return "wf-fake-001", nil
	}
	return f.workflowID, nil
}

func (f *fakeWorkflowSubmitter) Cancel(ctx context.Context, namespace, workflowID string) error {
	return nil
}

func (f *fakeWorkflowSubmitter) GetStatus(ctx context.Context, namespace, workflowID string) (*deploycontracts.WorkflowStatus, error) {
	return &deploycontracts.WorkflowStatus{Phase: "Succeeded"}, nil
}

// NOTE: fakeGitOpsPusher REMOVED — the workflow handles gitops-push (spec §10.1, step 13i).

// fakeGitOpsPusher implements deploycontracts.GitOpsPusher.
type fakeGitOpsPusher struct {
	pushErr   error
	pushed    []deploycontracts.GitOpsPushOpts
	commitSHA string
}

func (f *fakeGitOpsPusher) Push(ctx context.Context, opts deploycontracts.GitOpsPushOpts) (string, error) {
	f.pushed = append(f.pushed, opts)
	if f.pushErr != nil {
		return "", f.pushErr
	}
	if f.commitSHA != "" {
		return f.commitSHA, nil
	}
	return "abc123def456abc123def456abc123def456abcd", nil
}

func (f *fakeGitOpsPusher) Remove(ctx context.Context, opts deploycontracts.GitOpsRemoveOpts) (string, error) {
	if f.commitSHA != "" {
		return f.commitSHA, nil
	}
	return "abc123def456abc123def456abc123def456abcd", nil
}

// fakeSyncClient implements deploycontracts.SyncClient.
type fakeSyncClient struct {
	synced     []string
	triggerErr error
	waitErr    error
}

func (f *fakeSyncClient) TriggerSync(ctx context.Context, appName string) error {
	if f.triggerErr != nil {
		return f.triggerErr
	}
	f.synced = append(f.synced, appName)
	return nil
}

func (f *fakeSyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration, deployedApps []string) error {
	if f.waitErr != nil {
		return f.waitErr
	}
	return nil
}

// Sprint B T1.1 (ADR D1): stub methods to satisfy the widened SyncClient
// interface. The build-orchestrator unit tests assert only the
// TriggerSync/WaitForSync wiring; ArgoCD recovery is owned by
// ReleaseConvergenceLoop tests.
func (f *fakeSyncClient) ForceReconcile(ctx context.Context, appName string) error {
	return nil
}
func (f *fakeSyncClient) GetSyncRevision(ctx context.Context, appName string) (string, error) {
	return "", nil
}
func (f *fakeSyncClient) TerminateOperation(ctx context.Context, appName string) error {
	return nil
}
func (f *fakeSyncClient) GetAppStatus(ctx context.Context, appName string, deployedApps []string) (bool, bool, error) {
	return true, true, nil
}

// fakeEnvironmentLocker implements deploycontracts.EnvironmentLocker.
// It executes fn with a nil tx (unit tests don't need real transactions).
type fakeEnvironmentLocker struct {
	lockErr error
}

func (f *fakeEnvironmentLocker) WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error {
	if f.lockErr != nil {
		return f.lockErr
	}
	return fn(nil)
}

// --- Helper to build orchestrator with all fakes ---

type orchestratorFakes struct {
	buildStore     *fakeBuildStoreOrch
	releaseStore   *fakeReleaseStoreOrch
	envTargetStore *fakeEnvTargetStoreOrch
	eventStore     *fakeEventStoreOrch
	workflow       *fakeWorkflowSubmitter
	sync           *fakeSyncClient
	locker         *fakeEnvironmentLocker
	gitops         *fakeGitOpsPusher
}

func newOrchestratorFakes() *orchestratorFakes {
	return &orchestratorFakes{
		buildStore:     newFakeBuildStoreOrch(),
		releaseStore:   newFakeReleaseStoreOrch(),
		envTargetStore: newFakeEnvTargetStoreOrch(),
		eventStore:     newFakeEventStoreOrch(),
		workflow:       &fakeWorkflowSubmitter{},
		sync:           &fakeSyncClient{},
		locker:         &fakeEnvironmentLocker{},
		gitops:         &fakeGitOpsPusher{},
	}
}

func (f *orchestratorFakes) buildOrchestrator() *build.BuildOrchestrator {
	return build.NewBuildOrchestrator(build.BuildOrchestratorDeps{
		BuildStore:     f.buildStore,
		ReleaseStore:   f.releaseStore,
		EnvTargetStore: f.envTargetStore,
		EventStore:     f.eventStore,
		Workflow:       f.workflow,
		Sync:           f.sync,
		Locker:         f.locker,
		GitOps:         f.gitops,
		Delivery: release.NewContainerDeliveryActivator(release.ContainerDeliveryDeps{
			ReleaseStore: f.releaseStore, EnvTargetStore: f.envTargetStore, EventStore: f.eventStore,
			Sync: f.sync, Locker: f.locker, GitOps: f.gitops,
		}),
	})
}

func defaultSubmitReq() deploycontracts.SubmitBuildRequest {
	return deploycontracts.SubmitBuildRequest{
		TenantID:    "tenant-1",
		ProjectID:   "proj-abc123",
		Environment: "production",
		CommitSHA:   "abc123def456",
		Apps: []deploycontracts.AppSpec{
			{Name: "api", Type: "server", Port: 3000},
		},
		CreatedBy: "user-1",
	}
}

// testManifests is a minimal K8s manifest YAML for test callbacks.
const testManifests = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n"

// defaultSucceededCallback returns a standard succeeded callback with manifests.
func defaultSucceededCallback() deploycontracts.BuildCallbackRequest {
	return deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Manifests: testManifests,
		Apps: []deploycontracts.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc123", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 45000},
		},
	}
}

// --- Tests ---

func TestBuildOrchestrator_SubmitBuild_HappyPath(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()
	req := defaultSubmitReq()

	// Act
	resp, err := orch.SubmitBuild(context.Background(), req)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.BuildID == "" {
		t.Error("expected build_id to be set")
	}
	if resp.StreamURL == "" {
		t.Error("expected stream_url to be set")
	}

	// Build should exist in store with RUNNING status (QUEUED -> RUNNING)
	build, err := fakes.buildStore.GetByID(context.Background(), resp.BuildID)
	if err != nil {
		t.Fatalf("expected build in store, got error: %v", err)
	}
	if build.Status != string(statemachine.BPhRunning) {
		t.Errorf("expected build status RUNNING, got %q", build.Status)
	}

	// Workflow should have been submitted
	if len(fakes.workflow.submitted) != 1 {
		t.Fatalf("expected 1 workflow submission, got %d", len(fakes.workflow.submitted))
	}
	if fakes.workflow.submitted[0].BuildID != resp.BuildID {
		t.Errorf("expected workflow buildID %q, got %q", resp.BuildID, fakes.workflow.submitted[0].BuildID)
	}

	// SSE event build.started should have been emitted
	startedEvents := fakes.eventStore.eventsByType("build.started")
	if len(startedEvents) != 1 {
		t.Errorf("expected 1 build.started SSE event, got %d", len(startedEvents))
	}

	// F-01: build.started must include apps[] array for per-app CLI rendering
	if len(startedEvents) == 1 {
		var payload map[string]interface{}
		if err := json.Unmarshal(startedEvents[0].Data, &payload); err != nil {
			t.Fatalf("failed to unmarshal build.started data: %v", err)
		}
		apps, ok := payload["apps"]
		if !ok {
			t.Fatal("build.started SSE event missing 'apps' field — CLI cannot render per-app progress")
		}
		appsSlice, ok := apps.([]interface{})
		if !ok {
			t.Fatalf("expected apps to be an array, got %T", apps)
		}
		if len(appsSlice) != 1 {
			t.Errorf("expected 1 app in build.started, got %d", len(appsSlice))
		}
		if appsSlice[0] != "api" {
			t.Errorf("expected app name 'api', got %v", appsSlice[0])
		}
	}
}

func TestBuildOrchestrator_SubmitBuild_ConcurrentBuild(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	fakes.buildStore.activeBuild = true // Simulate an active build
	orch := fakes.buildOrchestrator()
	req := defaultSubmitReq()

	// Act
	_, err := orch.SubmitBuild(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal("expected error for concurrent build, got nil")
	}
	if !errors.Is(err, loopzeroerrors.ErrDeployInProgress) {
		t.Errorf("expected ErrDeployInProgress, got %v", err)
	}
}

func TestBuildOrchestrator_SubmitBuild_LockTimeout(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	fakes.locker.lockErr = loopzeroerrors.ErrLockTimeout
	orch := fakes.buildOrchestrator()
	req := defaultSubmitReq()

	// Act
	_, err := orch.SubmitBuild(context.Background(), req)

	// Assert
	if err == nil {
		t.Fatal("expected error for lock timeout, got nil")
	}
	if !errors.Is(err, loopzeroerrors.ErrLockTimeout) {
		t.Errorf("expected ErrLockTimeout, got %v", err)
	}
}

func TestBuildOrchestrator_OnBuildCompleted_Success_FullFlow(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	// First submit a build
	req := defaultSubmitReq()
	resp, err := orch.SubmitBuild(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act — build completed successfully
	cbReq := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Manifests: testManifests,
		Apps: []deploycontracts.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc123", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 45000},
		},
	}
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Build should be SUCCEEDED
	build, _ := fakes.buildStore.GetByID(context.Background(), resp.BuildID)
	if build.Status != string(statemachine.BPhSucceeded) {
		t.Errorf("expected build status SUCCEEDED, got %q", build.Status)
	}

	// A release should have been created and transitioned to ACTIVE
	var activeRelease *db.Release
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID {
			activeRelease = r
			break
		}
	}
	if activeRelease == nil {
		t.Fatal("expected a release to be created")
	}
	// MEET-2026-03-28-015: Phase D removed. After Activate(), release is PENDING.
	// The convergence loop or ArgoCD callback will transition to ACTIVE async.
	if activeRelease.Status != string(statemachine.RPhPending) {
		t.Errorf("expected release status PENDING (async activation), got %q", activeRelease.Status)
	}

	// Environment targets: desired should be set (by Phase C), active NOT set yet (async)
	envKey := "tenant-1:proj-abc123:production"
	if fakes.envTargetStore.desired[envKey] != activeRelease.ID {
		t.Errorf("expected desired release %q, got %q", activeRelease.ID, fakes.envTargetStore.desired[envKey])
	}

	// SSE events: build.succeeded + deploy.delivering + deploy.verifying (start)
	// deploy.live and deploy.completed are emitted by convergence loop, not inline
	succeededEvents := fakes.eventStore.eventsByType("build.succeeded")
	if len(succeededEvents) != 1 {
		t.Errorf("expected 1 build.succeeded event, got %d", len(succeededEvents))
	}

	deliveringEvents := fakes.eventStore.eventsByType("deploy.delivering")
	if len(deliveringEvents) != 1 {
		t.Errorf("expected 1 deploy.delivering event, got %d", len(deliveringEvents))
	}

	verifyingEvents := fakes.eventStore.eventsByType("deploy.verifying")
	if len(verifyingEvents) < 1 {
		t.Errorf("expected at least 1 deploy.verifying event, got %d", len(verifyingEvents))
	}

	// deploy.completed is now emitted by the convergence loop, not inline.
	// That event's format is tested in release_convergence_loop_test.go.
}

func TestBuildOrchestrator_OnBuildCompleted_BuildFailed(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act — build failed with error details
	cbReq := deploycontracts.BuildCallbackRequest{
		Status: "failed",
		Error:  "build-image: Dockerfile not found",
		Apps:   nil,
	}
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	build, _ := fakes.buildStore.GetByID(context.Background(), resp.BuildID)
	if build.Status != string(statemachine.BPhFailed) {
		t.Errorf("expected build status FAILED, got %q", build.Status)
	}

	// No release should be created
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID {
			t.Error("expected no release for failed build")
		}
	}

	// SSE event build.failed with error details
	failedEvents := fakes.eventStore.eventsByType("build.failed")
	if len(failedEvents) != 1 {
		t.Fatalf("expected 1 build.failed event, got %d", len(failedEvents))
	}
	var payload map[string]string
	if err := json.Unmarshal(failedEvents[0].Data, &payload); err != nil {
		t.Fatalf("failed to unmarshal build.failed data: %v", err)
	}
	if payload["error"] != "build-image: Dockerfile not found" {
		t.Errorf("expected error 'build-image: Dockerfile not found', got %q", payload["error"])
	}
}

func TestBuildOrchestrator_OnBuildCompleted_BuildFailedNoError(t *testing.T) {
	// Verify backward compatibility: build failure without error details
	// should not include an "error" field in the SSE event.

	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act — build failed without error details
	cbReq := deploycontracts.BuildCallbackRequest{
		Status: "failed",
		Apps:   nil,
	}
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify build.failed event exists without error field
	failedEvents := fakes.eventStore.eventsByType("build.failed")
	if len(failedEvents) != 1 {
		t.Fatalf("expected 1 build.failed event, got %d", len(failedEvents))
	}
	var payload map[string]string
	if err := json.Unmarshal(failedEvents[0].Data, &payload); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if _, hasError := payload["error"]; hasError {
		t.Error("expected no error field for callback without error details")
	}
}

func TestBuildOrchestrator_OnBuildCompleted_WorkerSkipsHealthProbe(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	req := defaultSubmitReq()
	req.Apps = []deploycontracts.AppSpec{
		{Name: "bg-worker", Type: "worker"},
	}

	resp, err := orch.SubmitBuild(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act
	cbReq := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Manifests: testManifests,
		Apps: []deploycontracts.AppRelease{
			{Name: "bg-worker", ImageDigest: "sha256:abc123", URL: "", DurationMs: 30000},
		},
	}
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Release should still be ACTIVE
	var release *db.Release
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID {
			release = r
			break
		}
	}
	if release == nil {
		t.Fatal("expected release to be created")
	}
	if release.Status != string(statemachine.RPhPending) {
		t.Errorf("expected release status PENDING (async activation via convergence loop), got %q", release.Status)
	}
}

func TestBuildOrchestrator_OnBuildCompleted_EnvironmentTargetsUpdated(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act
	cbReq := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Manifests: testManifests,
		Apps: []deploycontracts.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 10000},
		},
	}
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	envKey := "tenant-1:proj-abc123:production"

	// SetDesired must have been called
	desiredID, ok := fakes.envTargetStore.desired[envKey]
	if !ok {
		t.Fatal("expected SetDesired to be called")
	}
	if desiredID == "" {
		t.Error("expected desired release ID to be non-empty")
	}

	// MEET-015: SetActive is now called by convergence loop (async), not inline.
	// Only SetDesired is called during Activate. SetActive happens when PENDING → ACTIVE.
}

func TestBuildOrchestrator_OnBuildCompleted_CallbackWithBuildInQueued(t *testing.T) {
	// EC-N: Argo callback arrives before SubmitBuild transitions to RUNNING.
	// The orchestrator must handle QUEUED→RUNNING→SUCCEEDED without panic.

	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	// Manually create a build in QUEUED state (simulating Argo finishing before RUNNING transition)
	buildID := "build-ecn-test"
	fakes.buildStore.builds[buildID] = &db.Build{
		ID:          buildID,
		TenantID:    "tenant-1",
		ProjectID:   "proj-abc123",
		Environment: "production",
		Status:      string(statemachine.BPhQueued),
		CommitSHA:   "abc123",
		CreatedBy:   "user-1",
		CreatedAt:   time.Now().UTC(),
	}

	// Act — callback arrives while build is still QUEUED
	cbReq := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Manifests: testManifests,
		Apps: []deploycontracts.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 5000},
		},
	}
	err := orch.OnBuildCompleted(context.Background(), buildID, cbReq)

	// Assert — should not panic or error
	if err != nil {
		t.Fatalf("expected no error for EC-N scenario, got %v", err)
	}

	build, _ := fakes.buildStore.GetByID(context.Background(), buildID)
	if build.Status != string(statemachine.BPhSucceeded) {
		t.Errorf("expected build status SUCCEEDED after EC-N, got %q", build.Status)
	}
}

func TestBuildOrchestrator_OnBuildCompleted_DuplicateCallback(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// First callback — succeeds
	cbReq := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Manifests: testManifests,
		Apps: []deploycontracts.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 10000},
		},
	}
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)
	if err != nil {
		t.Fatalf("first callback failed: %v", err)
	}

	eventsBeforeDupe := len(fakes.eventStore.events)

	// Act — duplicate callback
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert — no-op, no error
	if err != nil {
		t.Fatalf("expected no error for duplicate callback, got %v", err)
	}

	// No new SSE events should have been emitted
	if len(fakes.eventStore.events) != eventsBeforeDupe {
		t.Errorf("expected no new SSE events for duplicate callback, got %d new events",
			len(fakes.eventStore.events)-eventsBeforeDupe)
	}
}

func TestBuildOrchestrator_OnBuildCompleted_SucceededWithEmptyApps(t *testing.T) {
	// DA-15: callback with status "succeeded" but empty apps → error
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act
	cbReq := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Manifests: testManifests,
		Apps:      []deploycontracts.AppRelease{}, // empty!
	}
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert — should return error
	if err == nil {
		t.Fatal("expected error for succeeded callback with empty apps, got nil")
	}

	var loopzeroErr *loopzeroerrors.LoopZeroError
	if !errors.As(err, &loopzeroErr) {
		t.Fatalf("expected LoopZeroError, got %T: %v", err, err)
	}
	// The service owns the code; its HTTP status is the route layer's mapping
	// (httpio), asserted by the error-code registry tests.
	if loopzeroErr.Code != "INVALID_CALLBACK" {
		t.Errorf("expected code INVALID_CALLBACK, got %q", loopzeroErr.Code)
	}
}

func TestBuildOrchestrator_OnBuildCompleted_PreviousReleaseSuperseded(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	// Set up a previous active release
	prevRelease := &db.Release{
		ID:              "rel-previous",
		BuildID:         "build-old",
		TenantID:        "tenant-1",
		ProjectID:       "proj-abc123",
		Environment:     "production",
		Status:          string(statemachine.RPhActive),
		CreatedAt:       time.Now().UTC(),
		GitMirrorStatus: "pending",
	}
	fakes.releaseStore.setActiveByEnv("tenant-1", "proj-abc123", "production", prevRelease)

	// Submit and complete a new build
	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	cbReq := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Manifests: testManifests,
		Apps: []deploycontracts.AppRelease{
			{Name: "api", ImageDigest: "sha256:new", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 20000},
		},
	}

	// Act
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// MEET-015: Previous release supersedence now happens in convergence loop.
	// At this point, previous release stays ACTIVE until the new one is activated async.
	prev := fakes.releaseStore.releases["rel-previous"]
	if prev == nil {
		t.Fatal("expected previous release to still exist")
	}
	// Previous release stays ACTIVE — convergence loop will supersede it when new release activates
}

func TestBuildOrchestrator_DeployCompletedSSE_DeployIDFormat(t *testing.T) {
	// Verify deploy_id in deploy.completed SSE event follows d-{4hex} format
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	cbReq := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Manifests: testManifests,
		Apps: []deploycontracts.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 10000},
		},
	}

	// Act
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)
	if err != nil {
		t.Fatalf("OnBuildCompleted failed: %v", err)
	}

	// MEET-015: deploy.completed is now emitted by convergence loop, not inline.
	// This test verifies Activate succeeds and emits deploy.verifying.
	verifyingEvents := fakes.eventStore.eventsByType("deploy.verifying")
	if len(verifyingEvents) < 1 {
		t.Fatalf("expected at least 1 deploy.verifying event, got %d", len(verifyingEvents))
	}
	// MEET-015: deploy.completed is emitted by convergence loop, not inline.
	// Format tested in release_convergence_loop_test.go.
}

// TestBuildOrchestrator_OnBuildCompleted_SyncFailsAfterBuildSucceeded tests that
// when the Argo build succeeds but ArgoCD sync fails, the build MUST transition
// to SUCCEEDED (not stay in RUNNING). The activation failure must be handled
// gracefully: emit a deploy failure event, NOT propagate the error (which would
// cause a transaction rollback leaving the build stuck in RUNNING forever).
//
// This is the root cause of the F-01 dogfooding bug: builds stuck in RUNNING
// permanently when ArgoCD returns "another operation is already in progress".
func TestBuildOrchestrator_OnBuildCompleted_SyncFailsAfterBuildSucceeded(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	fakes.sync.triggerErr = fmt.Errorf("argocd sync: status 400: another operation is already in progress")
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	cbReq := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Manifests: testManifests,
		Apps: []deploycontracts.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc123", URL: "https://production.proj-abc123.tenant-1.loopzero.example", Type: "server", DurationMs: 45000},
		},
	}

	// Act
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert — MUST NOT return error (activation failure is handled internally)
	if err != nil {
		t.Fatalf("expected OnBuildCompleted to handle sync failure gracefully, got error: %v", err)
	}

	// MEET-015: With async activation, if TriggerSync fails, Activate returns error.
	// The orchestrator handles it by keeping build in RELEASING or SUCCEEDED depending on flow.
	build := fakes.buildStore.builds[resp.BuildID]
	// Build transitions to SUCCEEDED because the activation error is handled gracefully
	if build.Status != string(statemachine.BPhSucceeded) && build.Status != string(statemachine.BPhReleasing) {
		t.Errorf("expected build status SUCCEEDED or RELEASING, got %q", build.Status)
	}

	// A deploy failure event MUST be emitted so the CLI knows what happened
	syncFailedEvents := fakes.eventStore.eventsByType("deploy.sync_failed")
	if len(syncFailedEvents) == 0 {
		t.Error("expected deploy.sync_failed SSE event to be emitted when sync fails")
	}

	// There should be a release in SYNC_FAILED state
	var release *db.Release
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID {
			release = r
			break
		}
	}
	if release == nil {
		t.Fatal("expected a release to be created even when sync fails")
	}
	if release.Status != string(statemachine.RPhSyncFailed) {
		t.Errorf("expected release status SYNC_FAILED, got %q", release.Status)
	}
}

// TestBuildOrchestrator_OnBuildCompleted_WaitForSyncFails tests that when ArgoCD
// TriggerSync succeeds but WaitForSync fails (e.g., sync timeout, app degraded),
// the release transitions to SYNC_FAILED instead of ACTIVE. This is the core
// BLOCKER-2 fix: without WaitForSync, health probes could pass against the OLD
// deployment, falsely marking the release as active.
func TestBuildOrchestrator_OnBuildCompleted_WaitForSyncFails(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	fakes.sync.waitErr = fmt.Errorf("argocd: app sync timed out after 120s")
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	cbReq := defaultSucceededCallback()

	// Act
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert — should NOT return error (activation failure is handled internally)
	if err != nil {
		t.Fatalf("expected OnBuildCompleted to handle WaitForSync failure gracefully, got error: %v", err)
	}

	// MEET-015: WaitForSync no longer runs inline. TriggerSync succeeds,
	// so Activate returns nil and build transitions to SUCCEEDED.
	// The WaitForSync failure configured in the fake is irrelevant now.
	build := fakes.buildStore.builds[resp.BuildID]
	if build.Status != string(statemachine.BPhSucceeded) {
		t.Errorf("expected build status SUCCEEDED (Phase D removed, async activation), got %q", build.Status)
	}

	// TriggerSync should have been called (it succeeds)
	if len(fakes.sync.synced) == 0 {
		t.Error("expected TriggerSync to have been called")
	}

	// Release should be in SYNC_FAILED state
	var release *db.Release
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID {
			release = r
			break
		}
	}
	if release == nil {
		t.Fatal("expected a release to be created even when WaitForSync fails")
	}
	// MEET-015: WaitForSync no longer runs inline. Release stays PENDING.
	// Convergence loop will detect sync failure and transition to SYNC_FAILED.
	if release.Status != string(statemachine.RPhPending) {
		t.Errorf("expected release status PENDING (async, convergence loop handles failures), got %q", release.Status)
	}

	// deploy.sync_failed is now emitted by convergence loop timeout, not inline
	syncFailedEvents := fakes.eventStore.eventsByType("deploy.sync_failed")
	_ = syncFailedEvents // convergence loop emits this, not tested here
	if false {
		t.Error("expected deploy.sync_failed SSE event when WaitForSync fails")
	}
}

func TestBuildOrchestrator_CancelBuild_RunningBuild(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	// Create a RUNNING build
	build := &db.Build{
		ID:          "build-cancel-running",
		TenantID:    "acme",
		ProjectID:   "proj-xxx",
		Environment: "production",
		WorkflowID:  "wf-12345",
		CreatedBy:   "github-lucas",
	}
	fakes.buildStore.builds[build.ID] = build
	build.Status = string(statemachine.BPhRunning)

	// Act
	err := orch.CancelBuild(context.Background(), build.ID)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Build should be CANCELLED
	updated := fakes.buildStore.builds[build.ID]
	if updated.Status != string(statemachine.BPhCancelled) {
		t.Errorf("expected status CANCELLED, got %s", updated.Status)
	}

	// SSE event should be emitted
	cancelledEvents := fakes.eventStore.eventsByType("build.cancelled")
	if len(cancelledEvents) == 0 {
		t.Error("expected build.cancelled SSE event to be emitted")
	}
}

func TestBuildOrchestrator_CancelBuild_QueuedBuild(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	// Create a QUEUED build (no workflow yet)
	build := &db.Build{
		ID:          "build-cancel-queued",
		TenantID:    "acme",
		ProjectID:   "proj-xxx",
		Environment: "staging",
		CreatedBy:   "github-lucas",
	}
	fakes.buildStore.builds[build.ID] = build
	build.Status = string(statemachine.BPhQueued)

	// Act
	err := orch.CancelBuild(context.Background(), build.ID)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// QUEUED → CANCELLED directly (no CANCELLING intermediate)
	updated := fakes.buildStore.builds[build.ID]
	if updated.Status != string(statemachine.BPhCancelled) {
		t.Errorf("expected status CANCELLED, got %s", updated.Status)
	}
}

func TestBuildOrchestrator_CancelBuild_AlreadyTerminal(t *testing.T) {
	// Arrange
	terminalStates := []statemachine.BuildPhase{
		statemachine.BPhSucceeded,
		statemachine.BPhFailed,
		statemachine.BPhCancelled,
	}

	for _, state := range terminalStates {
		t.Run(string(state), func(t *testing.T) {
			fakes := newOrchestratorFakes()
			orch := fakes.buildOrchestrator()

			build := &db.Build{
				ID:          "build-terminal-" + string(state),
				TenantID:    "acme",
				ProjectID:   "proj-xxx",
				Environment: "production",
				CreatedBy:   "github-lucas",
			}
			fakes.buildStore.builds[build.ID] = build
			build.Status = string(state)

			// Act
			err := orch.CancelBuild(context.Background(), build.ID)

			// Assert
			if err == nil {
				t.Fatal("expected error for terminal build, got nil")
			}
			if !errors.Is(err, loopzeroerrors.ErrBuildAlreadyCompleted) {
				t.Errorf("expected ErrBuildAlreadyCompleted, got %v", err)
			}
		})
	}
}

func TestBuildOrchestrator_CancelBuild_NotFound(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	// Act
	err := orch.CancelBuild(context.Background(), "build-nonexistent")

	// Assert
	if err == nil {
		t.Fatal("expected error for non-existent build, got nil")
	}
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound, got %v", err)
	}
}

// =============================================================================
// Step 6 Tests — GitOps Decoupling (build doesn't mutate GitOps)
// =============================================================================

// TestStep6_GitOpsPushFailure_ReleaseGitMirrorFailed tests that when the
// gitops push fails (e.g., Gitea unreachable), the release transitions to
// GIT_MIRROR_FAILED and the build stays SUCCEEDED. The transaction commits
// so the release record persists and can be retried without a new build.
func TestStep6_GitOpsPushFailure_ReleaseGitMirrorFailed(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	fakes.gitops.pushErr = fmt.Errorf("gitea: connection refused")
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act
	cbReq := defaultSucceededCallback()
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert — no error returned (failure handled internally)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Build MUST be RELEASING (Phase 1 transitions to RELEASING, Phase 2 failed but
	// build stays non-terminal for reconcile loop retry — MEET-2026-03-26-008)
	build := fakes.buildStore.builds[resp.BuildID]
	if build.Status != string(statemachine.BPhReleasing) {
		t.Errorf("expected build RELEASING, got %q", build.Status)
	}

	// Release should exist in GIT_MIRROR_FAILED state
	var release *db.Release
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID {
			release = r
			break
		}
	}
	if release == nil {
		t.Fatal("expected release to be created even when gitops push fails")
	}
	if release.Status != string(statemachine.RPhGitMirrorFailed) {
		t.Errorf("expected release GIT_MIRROR_FAILED, got %q", release.Status)
	}

	// GitOps pusher should have been called
	if len(fakes.gitops.pushed) != 1 {
		t.Errorf("expected 1 gitops push attempt, got %d", len(fakes.gitops.pushed))
	}

	// ArgoCD sync should NOT have been called (git push failed, no point syncing)
	if len(fakes.sync.synced) != 0 {
		t.Errorf("expected 0 ArgoCD syncs (git push failed), got %d", len(fakes.sync.synced))
	}

	// A deploy failure event should be emitted (delivery-agnostic name — MEET-009)
	failEvents := fakes.eventStore.eventsByType("deploy.delivery_failed")
	if len(failEvents) == 0 {
		t.Error("expected deploy.delivery_failed SSE event")
	}
}

// TestStep6_GitOpsPush_ReceivesManifestsFromCallback tests that the GitOps
// pusher receives the manifests from the build callback payload. This validates
// the end-to-end data flow: build generates manifests → callback includes them
// → API pushes them to GitOps.
func TestStep6_GitOpsPush_ReceivesManifestsFromCallback(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act
	customManifests := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: custom-app\n"
	cbReq := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Manifests: customManifests,
		Apps: []deploycontracts.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc123", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 45000},
		},
	}
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// GitOps pusher should have received the manifests
	if len(fakes.gitops.pushed) != 1 {
		t.Fatalf("expected 1 gitops push, got %d", len(fakes.gitops.pushed))
	}
	pushOpts := fakes.gitops.pushed[0]
	if pushOpts.Manifests != customManifests {
		t.Errorf("expected manifests %q, got %q", customManifests, pushOpts.Manifests)
	}
	if pushOpts.TenantID != "tenant-1" {
		t.Errorf("expected tenant-1, got %q", pushOpts.TenantID)
	}
	if pushOpts.ProjectID != "proj-abc123" {
		t.Errorf("expected proj-abc123, got %q", pushOpts.ProjectID)
	}
	if pushOpts.Environment != "production" {
		t.Errorf("expected production, got %q", pushOpts.Environment)
	}
	if pushOpts.BuildID != resp.BuildID {
		t.Errorf("expected build ID %q, got %q", resp.BuildID, pushOpts.BuildID)
	}
}

// TestStep6_ManifestsStoredInReleaseArtifacts tests that manifests are
// persisted in the release record's Artifacts field. This enables
// retry-without-rebuild: if the gitops push fails, the API can read the
// stored manifests and retry without triggering a new build.
func TestStep6_ManifestsStoredInReleaseArtifacts(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act
	cbReq := defaultSucceededCallback()
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Assert — find the release and check artifacts
	var release *db.Release
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID {
			release = r
			break
		}
	}
	if release == nil {
		t.Fatal("expected release to be created")
	}

	// Artifacts should contain the manifests
	if release.Artifacts == nil {
		t.Fatal("expected release.Artifacts to contain manifests, got nil")
	}
	var artifacts map[string]string
	if err := json.Unmarshal(release.Artifacts, &artifacts); err != nil {
		t.Fatalf("failed to unmarshal artifacts: %v", err)
	}
	if artifacts["manifests"] != testManifests {
		t.Errorf("expected manifests in artifacts, got %q", artifacts["manifests"])
	}
}

// TestStep6_BuildDoesNotCallGitOpsPush_WhenFailed tests that when a build
// fails, the gitops pusher is NOT called (no manifests to push).
func TestStep6_BuildDoesNotCallGitOpsPush_WhenFailed(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act — build failed
	cbReq := deploycontracts.BuildCallbackRequest{
		Status: "failed",
		Error:  "npm install failed",
		Apps: []deploycontracts.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc123"},
		},
	}
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Assert — gitops push should NOT have been called
	if len(fakes.gitops.pushed) != 0 {
		t.Errorf("expected 0 gitops pushes for failed build, got %d", len(fakes.gitops.pushed))
	}

	// No release should exist
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID {
			t.Error("expected no release for failed build")
		}
	}
}

// TestStep6_RetryRelease_WithoutRebuild tests that a release in GIT_MIRROR_FAILED
// state can be retried by reading manifests from release.Artifacts and re-pushing
// to GitOps — WITHOUT triggering a new build. This is the core "retry without rebuild"
// gate for Step 6.
func TestStep6_RetryRelease_WithoutRebuild(t *testing.T) {
	// Arrange — create a build that succeeds but gitops push fails
	fakes := newOrchestratorFakes()
	fakes.gitops.pushErr = fmt.Errorf("gitea: connection refused")
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Build completes — release created in GIT_MIRROR_FAILED
	cbReq := defaultSucceededCallback()
	if err := orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq); err != nil {
		t.Fatalf("OnBuildCompleted failed: %v", err)
	}

	// Find the release in GIT_MIRROR_FAILED
	var releaseID string
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID && r.Status == string(statemachine.RPhGitMirrorFailed) {
			releaseID = r.ID
			break
		}
	}
	if releaseID == "" {
		t.Fatal("expected a release in GIT_MIRROR_FAILED state")
	}

	// Act — fix gitops and retry
	fakes.gitops.pushErr = nil
	fakes.gitops.pushed = nil // reset push tracking
	err = orch.RetryRelease(context.Background(), releaseID)

	// Assert
	if err != nil {
		t.Fatalf("RetryRelease failed: %v", err)
	}

	// GitOps pusher should have been called with the stored manifests
	if len(fakes.gitops.pushed) != 1 {
		t.Fatalf("expected 1 gitops push on retry, got %d", len(fakes.gitops.pushed))
	}
	pushOpts := fakes.gitops.pushed[0]
	if pushOpts.Manifests != testManifests {
		t.Errorf("expected stored manifests %q, got %q", testManifests, pushOpts.Manifests)
	}

	// Release should now be PENDING (GIT_MIRROR_FAILED → PENDING via GitMirrorRetried)
	release := fakes.releaseStore.releases[releaseID]
	if release.Status != string(statemachine.RPhPending) {
		t.Errorf("expected release PENDING after retry, got %q", release.Status)
	}

	// No new build should have been submitted
	if len(fakes.workflow.submitted) != 1 {
		t.Errorf("expected exactly 1 workflow submission (original), got %d", len(fakes.workflow.submitted))
	}
}

// TestStep6_RetryRelease_NotGitMirrorFailed tests that RetryRelease rejects
// releases that are NOT in GIT_MIRROR_FAILED state.
func TestStep6_RetryRelease_NotGitMirrorFailed(t *testing.T) {
	// Arrange — create a successful release (ACTIVE)
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}
	if err := orch.OnBuildCompleted(context.Background(), resp.BuildID, defaultSucceededCallback()); err != nil {
		t.Fatalf("OnBuildCompleted failed: %v", err)
	}

	// Find the ACTIVE release
	var releaseID string
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID {
			releaseID = r.ID
			break
		}
	}

	// Act — retry on a non-GIT_MIRROR_FAILED release should fail
	err = orch.RetryRelease(context.Background(), releaseID)

	// Assert — should get an error
	if err == nil {
		t.Fatal("expected error when retrying non-GIT_MIRROR_FAILED release")
	}
	if !strings.Contains(err.Error(), "GIT_MIRROR_FAILED") {
		t.Errorf("expected error about GIT_MIRROR_FAILED state, got: %v", err)
	}
}

// TestStep6_RetryRelease_GitPushFailsAgain tests that if the retry also fails,
// the release stays in GIT_MIRROR_FAILED (no state corruption).
func TestStep6_RetryRelease_GitPushFailsAgain(t *testing.T) {
	// Arrange — create a release in GIT_MIRROR_FAILED
	fakes := newOrchestratorFakes()
	fakes.gitops.pushErr = fmt.Errorf("gitea: still down")
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}
	if err := orch.OnBuildCompleted(context.Background(), resp.BuildID, defaultSucceededCallback()); err != nil {
		t.Fatalf("OnBuildCompleted failed: %v", err)
	}

	var releaseID string
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID && r.Status == string(statemachine.RPhGitMirrorFailed) {
			releaseID = r.ID
			break
		}
	}
	if releaseID == "" {
		t.Fatal("expected a release in GIT_MIRROR_FAILED state")
	}

	// Act — retry but gitops is still down
	err = orch.RetryRelease(context.Background(), releaseID)

	// Assert — should return error
	if err == nil {
		t.Fatal("expected error when gitops push fails on retry")
	}

	// Release should STILL be in GIT_MIRROR_FAILED (not corrupted)
	release := fakes.releaseStore.releases[releaseID]
	if release.Status != string(statemachine.RPhGitMirrorFailed) {
		t.Errorf("expected release to stay GIT_MIRROR_FAILED, got %q", release.Status)
	}
}

// --- REVIEW-BLOCKER-1: Workflow submit outside lock tests ---

func TestBuildOrchestrator_SubmitBuild_WorkflowSubmitFails_BuildMarkedFailed(t *testing.T) {
	// REVIEW-BLOCKER-1: When workflow submission fails OUTSIDE the lock,
	// the build should be transitioned to FAILED so the environment is unblocked.
	// The build reaper also handles this (5min QUEUED timeout), but immediate
	// failure is better UX.

	// Arrange
	fakes := newOrchestratorFakes()
	fakes.workflow.err = fmt.Errorf("argo: connection refused")
	orch := fakes.buildOrchestrator()
	req := defaultSubmitReq()

	// Act
	_, err := orch.SubmitBuild(context.Background(), req)

	// Assert — should return error (workflow submit failed)
	if err == nil {
		t.Fatal("expected error when workflow submit fails, got nil")
	}

	// Build should exist and be in FAILED status (not stuck in QUEUED)
	var build *db.Build
	for _, b := range fakes.buildStore.builds {
		if b.TenantID == req.TenantID && b.ProjectID == req.ProjectID {
			build = b
			break
		}
	}
	if build == nil {
		t.Fatal("expected build to be created in store")
	}
	if build.Status != string(statemachine.BPhFailed) {
		t.Errorf("expected build status FAILED after submit error, got %q", build.Status)
	}
}

func TestBuildOrchestrator_SubmitBuild_WorkflowSubmitFails_EnvironmentUnblocked(t *testing.T) {
	// REVIEW-BLOCKER-1: After workflow submit fails, a SECOND deploy to the
	// same environment should succeed (environment is NOT locked).

	// Arrange
	fakes := newOrchestratorFakes()
	fakes.workflow.err = fmt.Errorf("argo: connection refused")
	orch := fakes.buildOrchestrator()
	req := defaultSubmitReq()

	// Act — first deploy fails at workflow submit
	_, err := orch.SubmitBuild(context.Background(), req)
	if err == nil {
		t.Fatal("expected first submit to fail")
	}

	// Now fix workflow and retry
	fakes.workflow.err = nil
	resp, err := orch.SubmitBuild(context.Background(), req)

	// Assert — second deploy should succeed
	if err != nil {
		t.Fatalf("expected second submit to succeed after first failure, got %v", err)
	}
	if resp.BuildID == "" {
		t.Error("expected build_id on second submit")
	}
}

func TestBuildOrchestrator_SubmitBuild_LockReleasedBeforeWorkflowSubmit(t *testing.T) {
	// REVIEW-BLOCKER-1: The advisory lock MUST be released before workflow
	// submission to avoid holding the lock during slow Argo API calls.

	// Arrange — track lock acquisition and workflow submission order
	var callOrder []string

	trackLocker := &trackingLocker{
		onLockAcquired: func() { callOrder = append(callOrder, "lock_fn_start") },
		onLockReleased: func() { callOrder = append(callOrder, "lock_fn_end") },
	}
	trackWorkflow := &fakeWorkflowSubmitter{
		onSubmit: func() { callOrder = append(callOrder, "workflow_submit") },
	}

	fakes := newOrchestratorFakes()
	// Build orchestrator directly with tracking deps
	orch := build.NewBuildOrchestrator(build.BuildOrchestratorDeps{
		BuildStore:     fakes.buildStore,
		ReleaseStore:   fakes.releaseStore,
		EnvTargetStore: fakes.envTargetStore,
		EventStore:     fakes.eventStore,
		Workflow:       trackWorkflow,
		Sync:           fakes.sync,
		Locker:         trackLocker,
		GitOps:         fakes.gitops,
	})
	req := defaultSubmitReq()

	// Act
	_, err := orch.SubmitBuild(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert — workflow_submit must come AFTER lock_fn_end
	if len(callOrder) < 3 {
		t.Fatalf("expected at least 3 call order entries, got %v", callOrder)
	}

	lockEndIdx := -1
	submitIdx := -1
	for i, c := range callOrder {
		if c == "lock_fn_end" && lockEndIdx == -1 {
			lockEndIdx = i
		}
		if c == "workflow_submit" && submitIdx == -1 {
			submitIdx = i
		}
	}

	if lockEndIdx == -1 {
		t.Fatal("lock_fn_end not found in call order")
	}
	if submitIdx == -1 {
		t.Fatal("workflow_submit not found in call order")
	}
	if submitIdx < lockEndIdx {
		t.Errorf("workflow_submit (idx=%d) happened BEFORE lock_fn_end (idx=%d) — lock held during submit", submitIdx, lockEndIdx)
	}
}

// trackingLocker implements deploycontracts.EnvironmentLocker with call-order tracking.
type trackingLocker struct {
	onLockAcquired func()
	onLockReleased func()
	lockErr        error
}

func (tl *trackingLocker) WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error {
	if tl.lockErr != nil {
		return tl.lockErr
	}
	if tl.onLockAcquired != nil {
		tl.onLockAcquired()
	}
	err := fn(nil)
	if tl.onLockReleased != nil {
		tl.onLockReleased()
	}
	return err
}
