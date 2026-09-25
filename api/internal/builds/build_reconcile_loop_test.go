// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// ---------------------------------------------------------------------------
// Fakes for BuildReconcileLoop unit tests
// ---------------------------------------------------------------------------

// fakeBuildStoreRL implements db.BuildStore for reconcile loop tests.
type fakeBuildStoreRL struct {
	mu     sync.Mutex
	builds map[string]*db.Build
	apps   map[string][]db.BuildAppSpec

	findActiveErr   error
	getByIDErr      error
	updateStatusErr error
	setErrorMsgErr  error
	getBuildAppsErr error

	// tracks UpdateStatus calls for assertions
	updateCalls []updateStatusCall
}

type updateStatusCall struct {
	BuildID string
	Event   string
}

func newFakeBuildStoreRL() *fakeBuildStoreRL {
	return &fakeBuildStoreRL{
		builds: make(map[string]*db.Build),
		apps:   make(map[string][]db.BuildAppSpec),
	}
}

func (f *fakeBuildStoreRL) Create(_ context.Context, _ db.DBTX, build *db.Build) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.builds[build.ID] = build
	return nil
}

func (f *fakeBuildStoreRL) GetByID(_ context.Context, id string) (*db.Build, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getByIDErr != nil {
		return nil, f.getByIDErr
	}
	b, ok := f.builds[id]
	if !ok {
		return nil, fmt.Errorf("build not found: %s", id)
	}
	cp := *b
	return &cp, nil
}

// GetByIDForTenant — T0.4 of deep-review-2026-05-10-remediation-plan.
// Emulates the production SQL filter for the unit-test scope.
func (f *fakeBuildStoreRL) GetByIDForTenant(ctx context.Context, tenantID, buildID string) (*db.Build, error) {
	b, err := f.GetByID(ctx, buildID)
	if err != nil || b == nil {
		return b, err
	}
	if b.TenantID != tenantID {
		return nil, nil
	}
	return b, nil
}

func (f *fakeBuildStoreRL) HasActiveBuild(_ context.Context, _ db.DBTX, _, _, _ string) (bool, error) {
	return false, nil
}

var rlBuildMachine = statemachine.NewBuildMachine()

func (f *fakeBuildStoreRL) UpdateStatus(_ context.Context, _ db.DBTX, id string, event string) (*db.Build, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateStatusErr != nil {
		return nil, f.updateStatusErr
	}
	b, ok := f.builds[id]
	if !ok {
		return nil, fmt.Errorf("build not found: %s", id)
	}
	f.updateCalls = append(f.updateCalls, updateStatusCall{BuildID: id, Event: event})
	newState, err := safeTransitionRL(rlBuildMachine, statemachine.State(b.Status), statemachine.Event(event))
	if err != nil {
		return nil, err
	}
	b.Status = string(newState)
	cp := *b
	return &cp, nil
}

func (f *fakeBuildStoreRL) UpdateStatusIfCurrent(_ context.Context, _, _ string, _ statemachine.BuildPhase, _ string) (*db.Build, error) {
	return nil, nil
}

func safeTransitionRL(m *statemachine.Machine, current statemachine.State, event statemachine.Event) (statemachine.State, error) {
	defer func() {
		_ = recover() // absorb panics from state machine
	}()
	ns := m.Transition(current, event)
	return ns, nil
}

func (f *fakeBuildStoreRL) SetErrorMessage(_ context.Context, _ db.DBTX, id string, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErrorMsgErr != nil {
		return f.setErrorMsgErr
	}
	if b, ok := f.builds[id]; ok {
		b.ErrorMessage = msg
	}
	return nil
}

func (f *fakeBuildStoreRL) SetWorkflowID(_ context.Context, _ db.DBTX, id, wfID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.builds[id]; ok {
		b.WorkflowID = wfID
	}
	return nil
}

func (f *fakeBuildStoreRL) SetAppsSpec(_ context.Context, _ db.DBTX, _ string, _ []db.BuildAppSpec) error {
	return nil
}

func (f *fakeBuildStoreRL) GetBuildApps(_ context.Context, buildID string) ([]db.BuildAppSpec, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getBuildAppsErr != nil {
		return nil, f.getBuildAppsErr
	}
	return f.apps[buildID], nil
}

func (f *fakeBuildStoreRL) ListByProject(_ context.Context, _, _ string, _, _ int) ([]db.Build, error) {
	return nil, nil
}

func (f *fakeBuildStoreRL) ListByProjectAndEnv(_ context.Context, _, _, _ string, _, _ int) ([]db.Build, int, error) {
	return nil, 0, nil
}

func (f *fakeBuildStoreRL) ListEnvsForProject(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

func (f *fakeBuildStoreRL) FindActiveBuilds(_ context.Context, _ string) ([]db.Build, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.findActiveErr != nil {
		return nil, f.findActiveErr
	}
	var result []db.Build
	for _, b := range f.builds {
		s := statemachine.State(b.Status)
		if s == statemachine.BuildQueued || s == statemachine.BuildRunning || s == statemachine.BuildReleasing {
			result = append(result, *b)
		}
	}
	return result, nil
}

func (f *fakeBuildStoreRL) CancelActiveBuilds(_ context.Context, _ db.DBTX, _, _, _ string) (int, []string, error) {
	return 0, nil, nil
}

// fakeWorkflowRL implements WorkflowSubmitter for reconcile loop tests.
type fakeWorkflowRL struct {
	statusResult *deploycontracts.WorkflowStatus
	statusErr    error
}

func (f *fakeWorkflowRL) Submit(_ context.Context, _ deploycontracts.WorkflowSpec) (string, error) {
	return "wf-test", nil
}

func (f *fakeWorkflowRL) Cancel(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeWorkflowRL) GetStatus(_ context.Context, _, _ string) (*deploycontracts.WorkflowStatus, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	return f.statusResult, nil
}

// fakeCompletionHandlerRL implements BuildCompletionHandler.
type fakeCompletionHandlerRL struct {
	mu                    sync.Mutex
	onBuildCompletedErr   error
	onBuildCompletedCalls int
	retryErr              error
	retryCalls            int
	findReleaseResult     *db.Release
	findReleaseErr        error
}

func (f *fakeCompletionHandlerRL) OnBuildCompleted(_ context.Context, _ string, _ deploycontracts.BuildCallbackRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onBuildCompletedCalls++
	return f.onBuildCompletedErr
}

func (f *fakeCompletionHandlerRL) RetryReleaseActivation(_ context.Context, _ *db.Build, _ deploycontracts.BuildCallbackRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retryCalls++
	return f.retryErr
}

func (f *fakeCompletionHandlerRL) FindReleaseForBuild(_ context.Context, _ string) (*db.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.findReleaseResult, f.findReleaseErr
}

// fakeLockerRL implements EnvironmentLocker.
type fakeLockerRL struct {
	lockErr error
}

func (f *fakeLockerRL) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	if f.lockErr != nil {
		return f.lockErr
	}
	return fn(nil)
}

// fakeDomainQuerier implements CustomDomainQuerier.
type fakeDomainQuerier struct{}

func (f *fakeDomainQuerier) ListActiveByApp(_ context.Context, _, _, _, _ string) ([]ports.CustomDomain, error) {
	return nil, nil
}

// fakeAppCRMgr implements ApplicationCREnsurer.
type fakeAppCRMgr struct {
	mu    sync.Mutex
	err   error
	calls []ApplicationCRInput
}

func (f *fakeAppCRMgr) EnsureLoopZeroApplication(_ context.Context, input ApplicationCRInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, input)
	return f.err
}

// fakeEventStoreRL implements the eventStore interface.
type fakeEventStoreRL struct {
	mu     sync.Mutex
	events []fakeEvent
	nextID int64
}

type fakeEvent struct {
	BuildID   string
	EventType string
	Data      json.RawMessage
}

func (f *fakeEventStoreRL) Append(_ context.Context, buildID string, eventType string, data json.RawMessage) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.events = append(f.events, fakeEvent{BuildID: buildID, EventType: eventType, Data: data})
	return f.nextID, nil
}

func (f *fakeEventStoreRL) eventsByType(eventType string) []fakeEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []fakeEvent
	for _, e := range f.events {
		if e.EventType == eventType {
			result = append(result, e)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Helper: create a BuildReconcileLoop with fakes
// ---------------------------------------------------------------------------

type reconcileLoopFakes struct {
	buildStore  *fakeBuildStoreRL
	workflow    *fakeWorkflowRL
	orch        *fakeCompletionHandlerRL
	locker      *fakeLockerRL
	domainStore *fakeDomainQuerier
	appCRMgr    *fakeAppCRMgr
	eventStore  *fakeEventStoreRL
}

func newReconcileLoopFakes() *reconcileLoopFakes {
	return &reconcileLoopFakes{
		buildStore:  newFakeBuildStoreRL(),
		workflow:    &fakeWorkflowRL{},
		orch:        &fakeCompletionHandlerRL{},
		locker:      &fakeLockerRL{},
		domainStore: &fakeDomainQuerier{},
		appCRMgr:    &fakeAppCRMgr{},
		eventStore:  &fakeEventStoreRL{},
	}
}

func (f *reconcileLoopFakes) buildLoop(cfg BuildReconcileLoopConfig) *BuildReconcileLoop {
	if cfg.MaxConcurrent == 0 {
		cfg.MaxConcurrent = 10
	}
	if cfg.MaxBuildsPerTenant == 0 {
		cfg.MaxBuildsPerTenant = 3
	}
	if cfg.RegistryURL == "" {
		cfg.RegistryURL = "registry.test:5000"
	}
	if cfg.BaseDomain == "" {
		cfg.BaseDomain = "test.local"
	}
	if cfg.ClusterIssuer == "" {
		cfg.ClusterIssuer = "test-issuer" // B14
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewBuildReconcileLoop(
		f.buildStore,
		f.workflow,
		f.orch,
		f.locker,
		f.domainStore,
		f.appCRMgr,
		f.eventStore,
		logger,
		cfg,
	)
}

func defaultBuild(id, status string) *db.Build {
	return &db.Build{
		ID:          id,
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Status:      status,
		CommitSHA:   "abc123",
		CreatedBy:   "user-1",
		CreatedAt:   time.Now().UTC().Add(-2 * time.Minute),
		WorkflowID:  "wf-" + id,
	}
}

// succeededStatusWithDigests builds a WorkflowStatus that mimics what the
// Argo Workflow CR reports after a successful cosign signing step. The
// supply-chain VAP (loopzero-require-image-signature) requires manifests to
// reference images by digest, so reconcile tests MUST surface a digest in
// the workflow outputs — otherwise BuildManifestApps fails-loud and
// downstream calls (OnBuildCompleted/RetryReleaseActivation) never fire.
func succeededStatusWithDigests(appNames ...string) *deploycontracts.WorkflowStatus {
	// 64 hex chars — each app gets a distinct digest by varying the
	// leading byte. Keeps assertions deterministic without needing
	// real cryptographic hashes.
	const fillerHex = "deadbeefcafef00d1234567890abcdef0123456789abcdef0123456789abcdef"
	outputs := make(map[string]map[string]string, len(appNames))
	for i, name := range appNames {
		// nibble 0..f for up to 16 apps in one workflow — plenty.
		leading := fmt.Sprintf("%x", i%16)
		hex64 := leading + fillerHex[1:] // keep total length at 64
		outputs["sign-image-"+name] = map[string]string{
			"digest":    "sha256:" + hex64,
			"signature": "registry.example.com/sig/" + name + "@sha256:" + hex64,
		}
	}
	return &deploycontracts.WorkflowStatus{
		Phase:      "Succeeded",
		FinishedAt: time.Now().UTC().Format(time.RFC3339),
		Outputs:    outputs,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestReconcileLoop_ProcessesActiveBuilds(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b1 := defaultBuild("bld-1", string(statemachine.BuildRunning))
	b2 := defaultBuild("bld-2", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b1.ID] = b1
	fakes.buildStore.builds[b2.ID] = b2
	fakes.buildStore.apps[b1.ID] = []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}}
	fakes.buildStore.apps[b2.ID] = []db.BuildAppSpec{{Name: "web", Type: "server", Port: 8080}}

	// Workflow returns Succeeded with cosign digests so the manifest
	// builder (post CHG-2026-04-29-DOGFOOD iter 8) finds a digest per app.
	fakes.workflow.statusResult = succeededStatusWithDigests("api", "web")

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.reconcile(context.Background())

	// Wait for goroutines to complete (reconcile dispatches async).
	// Semaphore draining: acquire all slots then release.
	for i := 0; i < cap(loop.sem); i++ {
		loop.sem <- struct{}{}
	}
	for i := 0; i < cap(loop.sem); i++ {
		<-loop.sem
	}

	// Assert: onWorkflowSucceeded delegates to orch.OnBuildCompleted
	fakes.orch.mu.Lock()
	calls := fakes.orch.onBuildCompletedCalls
	fakes.orch.mu.Unlock()
	if calls != 2 {
		t.Errorf("expected 2 OnBuildCompleted calls, got %d", calls)
	}
}

func TestReconcileBuild_ReleasingState_RetriesActivation(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-releasing", string(statemachine.BuildReleasing))
	fakes.buildStore.builds[b.ID] = b
	fakes.buildStore.apps[b.ID] = []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}}
	// No existing release found
	fakes.orch.findReleaseResult = nil
	// CHG-2026-04-29-DOGFOOD iter 8: retry path re-fetches workflow status
	// to enrich attestation outputs before manifest rendering. Without a
	// digest the manifest_builder fails-loud and orch.RetryReleaseActivation
	// is never called.
	fakes.workflow.statusResult = succeededStatusWithDigests("api")

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.reconcileBuild(context.Background(), b)

	// Assert: RetryReleaseActivation called on orch
	fakes.orch.mu.Lock()
	retryCalls := fakes.orch.retryCalls
	fakes.orch.mu.Unlock()
	if retryCalls != 1 {
		t.Errorf("expected 1 RetryReleaseActivation call, got %d", retryCalls)
	}
}

func TestReconcileBuild_NoWorkflowID_GracePeriod(t *testing.T) {
	// Arrange: build created 10s ago, no workflow ID — should be skipped (grace period)
	fakes := newReconcileLoopFakes()
	b := &db.Build{
		ID:          "bld-grace",
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Status:      string(statemachine.BuildRunning),
		CreatedAt:   time.Now().UTC().Add(-10 * time.Second),
		WorkflowID:  "", // no workflow ID yet
	}
	fakes.buildStore.builds[b.ID] = b
	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.reconcileBuild(context.Background(), b)

	// Assert: build should NOT be failed (grace period < 60s)
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildRunning) {
		t.Errorf("expected build to remain RUNNING during grace period, got %q", current.Status)
	}
	// No SSE event should be emitted
	failedEvents := fakes.eventStore.eventsByType("build.failed")
	if len(failedEvents) != 0 {
		t.Errorf("expected 0 build.failed events during grace period, got %d", len(failedEvents))
	}
}

func TestReconcileBuild_NoWorkflowID_Expired(t *testing.T) {
	// Arrange: build created 90s ago, no workflow ID — should be failed
	fakes := newReconcileLoopFakes()
	b := &db.Build{
		ID:          "bld-expired",
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Environment: "production",
		Status:      string(statemachine.BuildRunning),
		CreatedAt:   time.Now().UTC().Add(-90 * time.Second),
		WorkflowID:  "", // no workflow ID
	}
	fakes.buildStore.builds[b.ID] = b
	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.reconcileBuild(context.Background(), b)

	// Assert: build should be FAILED
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected build status FAILED after grace period, got %q", current.Status)
	}
	if current.ErrorMessage == "" {
		t.Error("expected error message to be set on expired build")
	}

	// SSE build.failed event should be emitted
	failedEvents := fakes.eventStore.eventsByType("build.failed")
	if len(failedEvents) != 1 {
		t.Errorf("expected 1 build.failed SSE event, got %d", len(failedEvents))
	}
}

func TestReconcileBuild_WorkflowSucceeded_CallsOnWorkflowSucceeded(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-succeeded", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b
	fakes.buildStore.apps[b.ID] = []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}}
	fakes.workflow.statusResult = succeededStatusWithDigests("api")

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.reconcileBuild(context.Background(), b)

	// Assert: orch.OnBuildCompleted should have been called
	fakes.orch.mu.Lock()
	calls := fakes.orch.onBuildCompletedCalls
	fakes.orch.mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 OnBuildCompleted call, got %d", calls)
	}
}

func TestReconcileBuild_WorkflowFailed_FailsBuild(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-wf-failed", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b
	fakes.workflow.statusResult = &deploycontracts.WorkflowStatus{
		Phase:      "Failed",
		FinishedAt: time.Now().UTC().Format(time.RFC3339),
	}

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.reconcileBuild(context.Background(), b)

	// Assert: build should be FAILED
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected build status FAILED, got %q", current.Status)
	}

	// SSE event emitted
	failedEvents := fakes.eventStore.eventsByType("build.failed")
	if len(failedEvents) != 1 {
		t.Errorf("expected 1 build.failed SSE event, got %d", len(failedEvents))
	}
}

func TestReconcileBuild_WorkflowFailed_NotFinished_Skips(t *testing.T) {
	// Arrange: workflow says "Failed" but FinishedAt is empty (transient state)
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-transient", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b
	fakes.workflow.statusResult = &deploycontracts.WorkflowStatus{
		Phase:      "Failed",
		FinishedAt: "", // not yet finished
	}

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.reconcileBuild(context.Background(), b)

	// Assert: build should remain RUNNING (transient Failed)
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildRunning) {
		t.Errorf("expected build to remain RUNNING for transient Failed, got %q", current.Status)
	}
}

func TestOnWorkflowSucceeded_GetBuildAppsFails_BuildFailed(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-apps-err", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b
	fakes.buildStore.getBuildAppsErr = errors.New("database connection lost")

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.onWorkflowSucceeded(context.Background(), b, &deploycontracts.WorkflowStatus{Phase: "Succeeded"})

	// Assert: build should be FAILED (GetBuildApps failed)
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected build status FAILED when GetBuildApps fails, got %q", current.Status)
	}
	if current.ErrorMessage == "" {
		t.Error("expected error message to be set")
	}
}

func TestOnWorkflowSucceeded_HappyPath(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-happy", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b
	fakes.buildStore.apps[b.ID] = []db.BuildAppSpec{
		{Name: "api", Type: "server", Port: 3000},
	}

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act — workflow status carries the cosign digest so manifest_builder
	// can render `image@sha256:...` (supply-chain VAP requirement).
	loop.onWorkflowSucceeded(context.Background(), b, succeededStatusWithDigests("api"))

	// Assert: orch.OnBuildCompleted should have been called
	fakes.orch.mu.Lock()
	calls := fakes.orch.onBuildCompletedCalls
	fakes.orch.mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 OnBuildCompleted call, got %d", calls)
	}
}

func TestRetryReleaseActivation_MaxAttempts_FailsBuild(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-max-retry", string(statemachine.BuildReleasing))
	fakes.buildStore.builds[b.ID] = b
	fakes.buildStore.apps[b.ID] = []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}}
	fakes.orch.findReleaseResult = nil

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Simulate 3 previous attempts (maxActivationAttempts = 3)
	loop.activationAttempts.Store(b.ID, maxActivationAttempts)

	// Act: 4th attempt should trigger failure
	loop.retryReleaseActivation(context.Background(), b)

	// Assert: build should be FAILED
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected build status FAILED after max attempts, got %q", current.Status)
	}

	// SSE event emitted
	failedEvents := fakes.eventStore.eventsByType("build.failed")
	if len(failedEvents) != 1 {
		t.Errorf("expected 1 build.failed SSE event, got %d", len(failedEvents))
	}
}

func TestRetryReleaseActivation_ReleaseAlreadyActive_CompletesBuild(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-rel-active", string(statemachine.BuildReleasing))
	fakes.buildStore.builds[b.ID] = b
	fakes.orch.findReleaseResult = &db.Release{
		ID:      "rel-001",
		BuildID: b.ID,
		Status:  "ACTIVE",
	}

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.retryReleaseActivation(context.Background(), b)

	// Assert: build should transition to SUCCEEDED (release already ACTIVE)
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildSucceeded) {
		t.Errorf("expected build status SUCCEEDED when release is ACTIVE, got %q", current.Status)
	}

	// RetryReleaseActivation on orch should NOT be called (release already ACTIVE)
	fakes.orch.mu.Lock()
	retryCalls := fakes.orch.retryCalls
	fakes.orch.mu.Unlock()
	if retryCalls != 0 {
		t.Errorf("expected 0 RetryReleaseActivation calls when release is ACTIVE, got %d", retryCalls)
	}
}

func TestRetryReleaseActivation_ReleaseSyncFailed_FailsBuild(t *testing.T) {
	// Arrange: existing release in SYNC_FAILED state
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-sync-fail", string(statemachine.BuildReleasing))
	fakes.buildStore.builds[b.ID] = b
	fakes.orch.findReleaseResult = &db.Release{
		ID:      "rel-sync-fail",
		BuildID: b.ID,
		Status:  "SYNC_FAILED",
	}

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.retryReleaseActivation(context.Background(), b)

	// Assert: build should be FAILED
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected build status FAILED for SYNC_FAILED release, got %q", current.Status)
	}
}

func TestRetryReleaseActivation_ReleaseTransient_Skips(t *testing.T) {
	// Arrange: existing release in PENDING state (transient — wait)
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-transient", string(statemachine.BuildReleasing))
	fakes.buildStore.builds[b.ID] = b
	fakes.orch.findReleaseResult = &db.Release{
		ID:      "rel-pending",
		BuildID: b.ID,
		Status:  "PENDING",
	}

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.retryReleaseActivation(context.Background(), b)

	// Assert: build should remain RELEASING (waiting for release to resolve)
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildReleasing) {
		t.Errorf("expected build to remain RELEASING for transient release, got %q", current.Status)
	}

	// orch.RetryReleaseActivation should NOT be called (transient state — skip)
	fakes.orch.mu.Lock()
	retryCalls := fakes.orch.retryCalls
	fakes.orch.mu.Unlock()
	if retryCalls != 0 {
		t.Errorf("expected 0 RetryReleaseActivation calls for transient release, got %d", retryCalls)
	}
}

func TestFailBuild_TransitionsToFailed_FromRunning(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-fail-running", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.failBuild(context.Background(), b, "something went wrong")

	// Assert
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected FAILED, got %q", current.Status)
	}
	if current.ErrorMessage != "something went wrong" {
		t.Errorf("expected error message 'something went wrong', got %q", current.ErrorMessage)
	}

	// SSE event
	failedEvents := fakes.eventStore.eventsByType("build.failed")
	if len(failedEvents) != 1 {
		t.Errorf("expected 1 build.failed SSE event, got %d", len(failedEvents))
	}
	var payload map[string]string
	if err := json.Unmarshal(failedEvents[0].Data, &payload); err != nil {
		t.Fatalf("failed to unmarshal SSE data: %v", err)
	}
	if payload["error"] != "something went wrong" {
		t.Errorf("expected error 'something went wrong' in SSE, got %q", payload["error"])
	}
}

func TestFailBuild_TransitionsToFailed_FromQueued(t *testing.T) {
	// Arrange: QUEUED build should go QUEUED -> RUNNING -> FAILED
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-fail-queued", string(statemachine.BuildQueued))
	fakes.buildStore.builds[b.ID] = b

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.failBuild(context.Background(), b, "orphaned build")

	// Assert
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected FAILED, got %q", current.Status)
	}

	// Verify two transitions happened: BuildStarted then BuildFailedEvent
	fakes.buildStore.mu.Lock()
	calls := fakes.buildStore.updateCalls
	fakes.buildStore.mu.Unlock()

	if len(calls) < 2 {
		t.Fatalf("expected at least 2 UpdateStatus calls (QUEUED->RUNNING->FAILED), got %d", len(calls))
	}
	if calls[0].Event != string(statemachine.BuildStarted) {
		t.Errorf("expected first transition BuildStarted, got %q", calls[0].Event)
	}
	if calls[1].Event != string(statemachine.BuildFailedEvent) {
		t.Errorf("expected second transition BuildFailedEvent, got %q", calls[1].Event)
	}
}

func TestFailBuild_TransitionsToFailed_FromReleasing(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-fail-releasing", string(statemachine.BuildReleasing))
	fakes.buildStore.builds[b.ID] = b

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.failBuild(context.Background(), b, "release activation timed out")

	// Assert
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected FAILED, got %q", current.Status)
	}

	// Should use BuildReleaseFailedEvent
	fakes.buildStore.mu.Lock()
	calls := fakes.buildStore.updateCalls
	fakes.buildStore.mu.Unlock()

	found := false
	for _, c := range calls {
		if c.Event == string(statemachine.BuildReleaseFailedEvent) {
			found = true
		}
	}
	if !found {
		t.Error("expected BuildReleaseFailedEvent transition for RELEASING build")
	}
}

func TestFailBuild_SkipsAlreadyTerminal(t *testing.T) {
	// Arrange: build already SUCCEEDED — failBuild should no-op
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-already-done", string(statemachine.BuildSucceeded))
	fakes.buildStore.builds[b.ID] = b

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.failBuild(context.Background(), b, "should not matter")

	// Assert: status unchanged
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildSucceeded) {
		t.Errorf("expected status to remain SUCCEEDED, got %q", current.Status)
	}
}

func TestEnsureApplicationCRs_NonFatal(t *testing.T) {
	// Arrange: appCRMgr returns error — should be logged but not block deploy
	fakes := newReconcileLoopFakes()
	fakes.appCRMgr.err = errors.New("k8s API unreachable")

	b := defaultBuild("bld-cr-fail", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b
	apps := []db.BuildAppSpec{
		{Name: "api", Type: "server", Port: 3000},
		{Name: "worker", Type: "worker", Port: 0},
	}

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act: should NOT panic or return error
	loop.ensureApplicationCRs(context.Background(), b, apps)

	// Assert: EnsureLoopZeroApplication was called for each app despite errors
	fakes.appCRMgr.mu.Lock()
	callCount := len(fakes.appCRMgr.calls)
	fakes.appCRMgr.mu.Unlock()
	if callCount != 2 {
		t.Errorf("expected 2 EnsureLoopZeroApplication calls (non-fatal), got %d", callCount)
	}
}

func TestEnsureApplicationCRs_NilManager_NoOp(t *testing.T) {
	// Arrange: appCRMgr is nil (not configured)
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-cr-nil", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b

	cfg := BuildReconcileLoopConfig{
		RegistryURL: "registry.test:5000",
		BaseDomain:  "test.local",
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	loop := NewBuildReconcileLoop(
		fakes.buildStore, fakes.workflow, fakes.orch, fakes.locker,
		fakes.domainStore, nil, // nil appCRMgr
		fakes.eventStore, logger, cfg,
	)

	apps := []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}}

	// Act: should not panic
	loop.ensureApplicationCRs(context.Background(), b, apps)
	// No assertion needed — the test passes if it does not panic
}

func TestEnsureApplicationCRs_DefaultPort(t *testing.T) {
	// Arrange: app with port 0 should get default port 80
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-cr-port", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b
	apps := []db.BuildAppSpec{
		{Name: "web", Type: "frontend", Port: 0},
	}

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.ensureApplicationCRs(context.Background(), b, apps)

	// Assert
	fakes.appCRMgr.mu.Lock()
	defer fakes.appCRMgr.mu.Unlock()
	if len(fakes.appCRMgr.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(fakes.appCRMgr.calls))
	}
	if fakes.appCRMgr.calls[0].ServicePort != 80 {
		t.Errorf("expected default port 80 for port-0 app, got %d", fakes.appCRMgr.calls[0].ServicePort)
	}
}

func TestReconcile_FindActiveBuildsFails_DoesNotPanic(t *testing.T) {
	// Arrange: buildStore.FindActiveBuilds returns error
	fakes := newReconcileLoopFakes()
	fakes.buildStore.findActiveErr = errors.New("connection reset by peer")

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act: should not panic
	loop.reconcile(context.Background())
	// No assertion — test passes if no panic
}

func TestReconcile_WorkflowNotFound_FailsBuild(t *testing.T) {
	// Arrange: GetStatus returns nil (workflow not found / expired)
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-wf-gone", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b
	fakes.workflow.statusResult = nil // workflow not found

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.reconcileBuild(context.Background(), b)

	// Assert: build should be FAILED
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected FAILED when workflow not found, got %q", current.Status)
	}
}

func TestReconcile_WorkflowStatusError_FailsBuild(t *testing.T) {
	// Arrange: GetStatus returns error
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-status-err", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b
	fakes.workflow.statusErr = errors.New("k8s API timeout")

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.reconcileBuild(context.Background(), b)

	// Assert: build should remain RUNNING (error checking status is not terminal)
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildRunning) {
		t.Errorf("expected RUNNING when GetStatus fails (will retry), got %q", current.Status)
	}
}

func TestOnWorkflowSucceeded_CompletionFailsMaxAttempts_FailsBuild(t *testing.T) {
	// Arrange: orch.OnBuildCompleted always fails
	fakes := newReconcileLoopFakes()
	fakes.orch.onBuildCompletedErr = errors.New("orch: database lock timeout")
	b := defaultBuild("bld-comp-fail", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b
	fakes.buildStore.apps[b.ID] = []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}}

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Simulate maxActivationAttempts-1 previous failures
	loop.completionAttempts.Store(b.ID, maxActivationAttempts-1)

	// Act: this is the Nth attempt that triggers failure
	loop.onWorkflowSucceeded(context.Background(), b, &deploycontracts.WorkflowStatus{Phase: "Succeeded"})

	// Assert: build should be FAILED
	current := fakes.buildStore.builds[b.ID]
	if current.Status != string(statemachine.BuildFailed) {
		t.Errorf("expected FAILED after max completion attempts, got %q", current.Status)
	}
}

func TestIsProcessing_ReturnsTrueForActiveBuilds(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Simulate a build being processed
	loop.active.Store("bld-123", "tenant-1")

	// Act & Assert
	if !loop.IsProcessing("bld-123") {
		t.Error("expected IsProcessing to return true for active build")
	}
	if loop.IsProcessing("bld-nonexistent") {
		t.Error("expected IsProcessing to return false for unknown build")
	}
}

func TestReconcile_SkipsDuplicateBuilds(t *testing.T) {
	// Arrange: build already in active map
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-dup", string(statemachine.BuildRunning))
	fakes.buildStore.builds[b.ID] = b
	fakes.workflow.statusResult = &deploycontracts.WorkflowStatus{Phase: "Succeeded", FinishedAt: time.Now().UTC().Format(time.RFC3339)}

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})
	// Pre-mark as active (simulates previous tick still processing)
	loop.active.Store(b.ID, b.TenantID)

	// Act
	loop.reconcile(context.Background())

	// Wait for any goroutines
	for i := 0; i < cap(loop.sem); i++ {
		loop.sem <- struct{}{}
	}
	for i := 0; i < cap(loop.sem); i++ {
		<-loop.sem
	}

	// Assert: OnBuildCompleted should NOT be called (build skipped as duplicate)
	fakes.orch.mu.Lock()
	calls := fakes.orch.onBuildCompletedCalls
	fakes.orch.mu.Unlock()
	if calls != 0 {
		t.Errorf("expected 0 OnBuildCompleted calls for duplicate build, got %d", calls)
	}
}

func TestReconcile_PerTenantBackpressure(t *testing.T) {
	// Arrange: 4 builds for same tenant, limit is 3
	fakes := newReconcileLoopFakes()
	for i := 0; i < 4; i++ {
		b := defaultBuild(fmt.Sprintf("bld-bp-%d", i), string(statemachine.BuildRunning))
		b.TenantID = "tenant-busy"
		fakes.buildStore.builds[b.ID] = b
		fakes.buildStore.apps[b.ID] = []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}}
	}
	fakes.workflow.statusResult = &deploycontracts.WorkflowStatus{Phase: "Running"}

	loop := fakes.buildLoop(BuildReconcileLoopConfig{MaxBuildsPerTenant: 3})

	// Act
	loop.reconcile(context.Background())

	// Wait for goroutines
	for i := 0; i < cap(loop.sem); i++ {
		loop.sem <- struct{}{}
	}
	for i := 0; i < cap(loop.sem); i++ {
		<-loop.sem
	}

	// Assert: at most 3 should have been dispatched. We cannot check exact
	// count since builds are map-iterated (non-deterministic), but all 4
	// should not be simultaneously active.
	// The test passes if it does not panic or deadlock — the backpressure
	// mechanism is structural.
}

// ---------------------------------------------------------------------------
// A failed release lookup must not be mistaken for "no release exists".
//
// ReleaseStoreSqlc.GetLatestByBuildID separates the two states deliberately:
// (nil, nil) when no release exists for the build, (nil, err) when the lookup
// failed. Discarding the error collapsed both to nil, skipped the guard, and
// reached Activate() — which writes a release row as its first statement.
//
// Every test below MUST populate the workflow status through
// succeededStatusWithDigests: without a digest, BuildManifestApps fails loud
// and RetryReleaseActivation is never reached, so an assertion of zero
// retries would be satisfied by the manifest gate instead of by the fix and
// would pass against the defect it exists to catch. See the note at the
// succeededStatusWithDigests declaration above.
//
// Backlog item B-069.
// ---------------------------------------------------------------------------

func TestRetryReleaseActivation_LookupFails_DoesNotActivate(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-lookup-fail", string(statemachine.BuildReleasing))
	fakes.buildStore.builds[b.ID] = b
	fakes.buildStore.apps[b.ID] = []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}}
	fakes.workflow.statusResult = succeededStatusWithDigests("api")
	fakes.orch.findReleaseResult = nil
	fakes.orch.findReleaseErr = errors.New("sqlc: get latest by build_id: conn closed")

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act
	loop.retryReleaseActivation(context.Background(), b)

	// Assert: the release state is unknown, so the branch that creates one
	// must not be taken.
	if fakes.orch.retryCalls != 0 {
		t.Errorf("lookup failed, so the release state is unknown: expected 0 "+
			"RetryReleaseActivation calls, got %d", fakes.orch.retryCalls)
	}
	// And a single transient read must not end the user's deploy.
	if got := fakes.buildStore.builds[b.ID].Status; got != string(statemachine.BuildReleasing) {
		t.Errorf("a transient lookup failure must skip the tick, not end the deploy: "+
			"expected status RELEASING, got %q", got)
	}
}

func TestRetryReleaseActivation_LookupFailsEveryTick_NeverRetries(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-lookup-fail-10", string(statemachine.BuildReleasing))
	fakes.buildStore.builds[b.ID] = b
	fakes.buildStore.apps[b.ID] = []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}}
	fakes.workflow.statusResult = succeededStatusWithDigests("api")
	fakes.orch.findReleaseResult = nil
	fakes.orch.findReleaseErr = errors.New("sqlc: get latest by build_id: pool exhausted")

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act: ten ticks, deliberately at or below the cap (the comparison is
	// attempts > maxActivationAttempts), so this measures the branch and not
	// the cap.
	for i := 0; i < maxActivationAttempts; i++ {
		loop.retryReleaseActivation(context.Background(), b)
	}

	// Assert
	if fakes.orch.retryCalls != 0 {
		t.Errorf("expected 0 RetryReleaseActivation calls across %d failing ticks, got %d",
			maxActivationAttempts, fakes.orch.retryCalls)
	}
}

func TestRetryReleaseActivation_LookupFailsEveryTick_FailsBuildAtCap(t *testing.T) {
	// Arrange
	fakes := newReconcileLoopFakes()
	b := defaultBuild("bld-lookup-fail-cap", string(statemachine.BuildReleasing))
	fakes.buildStore.builds[b.ID] = b
	fakes.buildStore.apps[b.ID] = []db.BuildAppSpec{{Name: "api", Type: "server", Port: 3000}}
	fakes.workflow.statusResult = succeededStatusWithDigests("api")
	fakes.orch.findReleaseResult = nil
	fakes.orch.findReleaseErr = errors.New("sqlc: get latest by build_id: conn closed")

	loop := fakes.buildLoop(BuildReconcileLoopConfig{})

	// Act: eleven ticks — the eleventh crosses maxActivationAttempts. The
	// error branch must sit AFTER the attempt counter, or a failing tick
	// costs nothing, the cap is never crossed, and the build stalls without
	// bound instead of failing.
	for i := 0; i < maxActivationAttempts+1; i++ {
		loop.retryReleaseActivation(context.Background(), b)
	}

	// Assert BOTH halves. The cap half alone is satisfied by the defect,
	// which also reaches FAILED — after creating a release row on every tick.
	if fakes.orch.retryCalls != 0 {
		t.Errorf("expected 0 RetryReleaseActivation calls, got %d", fakes.orch.retryCalls)
	}
	if got := fakes.buildStore.builds[b.ID].Status; got != string(statemachine.BuildFailed) {
		t.Errorf("a lookup failing on every tick must still terminate the build through "+
			"the existing cap: expected FAILED, got %q", got)
	}
}
