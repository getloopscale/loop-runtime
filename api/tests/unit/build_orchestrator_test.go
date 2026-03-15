package unit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/api/internal/sse"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
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
	build.Status = string(statemachine.BuildQueued)
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
	releases     map[string]*db.Release
	activeByEnv  map[string]*db.Release // key: tenant:project:env
	createErr    error
	updateErr    error
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
	release.Status = string(statemachine.ReleaseReady)
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

func (f *fakeReleaseStoreOrch) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*db.Release, error) {
	key := fmt.Sprintf("%s:%s:%s", tenantID, projectID, env)
	r, ok := f.activeByEnv[key]
	if !ok {
		return nil, nil
	}
	copy := *r
	return &copy, nil
}

// setActiveByEnv manually sets an active release (for test setup).
func (f *fakeReleaseStoreOrch) setActiveByEnv(tenantID, projectID, env string, r *db.Release) {
	key := fmt.Sprintf("%s:%s:%s", tenantID, projectID, env)
	f.activeByEnv[key] = r
	f.releases[r.ID] = r
}

// fakeEnvTargetStoreOrch implements db.EnvironmentTargetStore.
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

func (f *fakeEnvTargetStoreOrch) Get(ctx context.Context, tenantID, projectID, env string) (*db.EnvironmentTarget, error) {
	return nil, nil
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

// fakeWorkflowSubmitter implements services.WorkflowSubmitter.
type fakeWorkflowSubmitter struct {
	submitted  []services.WorkflowSpec
	workflowID string
	err        error
}

func (f *fakeWorkflowSubmitter) Submit(ctx context.Context, spec services.WorkflowSpec) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.submitted = append(f.submitted, spec)
	if f.workflowID == "" {
		return "wf-fake-001", nil
	}
	return f.workflowID, nil
}

func (f *fakeWorkflowSubmitter) Cancel(ctx context.Context, workflowID string) error {
	return nil
}

// NOTE: fakeGitOpsPusher REMOVED — the workflow handles gitops-push (spec §10.1, step 13i).

// fakeHealthProber implements services.HealthProber.
type fakeHealthProber struct {
	results map[string]error // url → error (nil = success)
	probed  []string         // urls that were probed
}

func newFakeHealthProber() *fakeHealthProber {
	return &fakeHealthProber{
		results: make(map[string]error),
	}
}

func (f *fakeHealthProber) Probe(ctx context.Context, url string, path string) error {
	fullURL := url + path
	f.probed = append(f.probed, fullURL)
	if err, ok := f.results[fullURL]; ok {
		return err
	}
	return nil
}

// fakeSyncClient implements services.SyncClient.
type fakeSyncClient struct {
	synced    []string
	triggerErr error
	waitErr   error
}

func (f *fakeSyncClient) TriggerSync(ctx context.Context, appName string) error {
	if f.triggerErr != nil {
		return f.triggerErr
	}
	f.synced = append(f.synced, appName)
	return nil
}

func (f *fakeSyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration) error {
	if f.waitErr != nil {
		return f.waitErr
	}
	return nil
}

// fakeEnvironmentLocker implements services.EnvironmentLocker.
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
	buildStore    *fakeBuildStoreOrch
	releaseStore  *fakeReleaseStoreOrch
	envTargetStore *fakeEnvTargetStoreOrch
	eventStore    *fakeEventStoreOrch
	workflow      *fakeWorkflowSubmitter
	health        *fakeHealthProber
	sync          *fakeSyncClient
	locker        *fakeEnvironmentLocker
}

func newOrchestratorFakes() *orchestratorFakes {
	return &orchestratorFakes{
		buildStore:     newFakeBuildStoreOrch(),
		releaseStore:   newFakeReleaseStoreOrch(),
		envTargetStore: newFakeEnvTargetStoreOrch(),
		eventStore:     newFakeEventStoreOrch(),
		workflow:       &fakeWorkflowSubmitter{},
		health:         newFakeHealthProber(),
		sync:           &fakeSyncClient{},
		locker:         &fakeEnvironmentLocker{},
	}
}

func (f *orchestratorFakes) buildOrchestrator() *services.BuildOrchestrator {
	return services.NewBuildOrchestrator(services.BuildOrchestratorDeps{
		BuildStore:     f.buildStore,
		ReleaseStore:   f.releaseStore,
		EnvTargetStore: f.envTargetStore,
		EventStore:     f.eventStore,
		Workflow:       f.workflow,
		Health:         f.health,
		Sync:           f.sync,
		Locker:         f.locker,
	})
}

func defaultSubmitReq() services.SubmitBuildRequest {
	return services.SubmitBuildRequest{
		TenantID:    "tenant-1",
		ProjectID:   "proj-abc123",
		Environment: "production",
		CommitSHA:   "abc123def456",
		Apps: []services.AppSpec{
			{Name: "api", Type: "server", Port: 3000},
		},
		CreatedBy: "user-1",
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
	if build.Status != string(statemachine.BuildRunning) {
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
	cbReq := services.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []services.AppRelease{
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
	if build.Status != string(statemachine.BuildSucceeded) {
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
	if activeRelease.Status != string(statemachine.ReleaseActive) {
		t.Errorf("expected release status ACTIVE, got %q", activeRelease.Status)
	}

	// Environment targets should be updated
	envKey := "tenant-1:proj-abc123:production"
	if fakes.envTargetStore.desired[envKey] != activeRelease.ID {
		t.Errorf("expected desired release %q, got %q", activeRelease.ID, fakes.envTargetStore.desired[envKey])
	}
	if fakes.envTargetStore.active[envKey] != activeRelease.ID {
		t.Errorf("expected active release %q, got %q", activeRelease.ID, fakes.envTargetStore.active[envKey])
	}

	// SSE events: build.succeeded, deploy.pending, deploy.health_check (checking), deploy.health_check (ok), deploy.active, deploy.completed
	succeededEvents := fakes.eventStore.eventsByType("build.succeeded")
	if len(succeededEvents) != 1 {
		t.Errorf("expected 1 build.succeeded event, got %d", len(succeededEvents))
	}

	pendingEvents := fakes.eventStore.eventsByType("deploy.pending")
	if len(pendingEvents) != 1 {
		t.Errorf("expected 1 deploy.pending event, got %d", len(pendingEvents))
	}

	activeEvents := fakes.eventStore.eventsByType("deploy.active")
	if len(activeEvents) != 1 {
		t.Errorf("expected 1 deploy.active event, got %d", len(activeEvents))
	}

	deployEvents := fakes.eventStore.eventsByType("deploy.completed")
	if len(deployEvents) != 1 {
		t.Errorf("expected 1 deploy.completed event, got %d", len(deployEvents))
	}

	// Verify deploy.completed payload has deploy_id in d-{4hex} format
	if len(deployEvents) == 1 {
		var payload map[string]interface{}
		if err := json.Unmarshal(deployEvents[0].Data, &payload); err != nil {
			t.Fatalf("failed to unmarshal deploy.completed data: %v", err)
		}
		deployID, ok := payload["deploy_id"].(string)
		if !ok {
			t.Fatal("expected deploy_id in deploy.completed payload")
		}
		if !strings.HasPrefix(deployID, "d-") || len(deployID) != 6 {
			t.Errorf("expected deploy_id format d-{4hex}, got %q", deployID)
		}
	}
}

func TestBuildOrchestrator_OnBuildCompleted_ProbeFails(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	fakes.health.results["https://production.proj-abc123.tenant-1.loopzero.example/health"] = fmt.Errorf("connection refused")
	orch := fakes.buildOrchestrator()

	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act
	cbReq := services.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []services.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc123", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 45000},
		},
	}
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert — should NOT return error (orchestrator handles internally)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Release should be SYNC_FAILED
	var release *db.Release
	for _, r := range fakes.releaseStore.releases {
		if r.BuildID == resp.BuildID {
			release = r
			break
		}
	}
	if release == nil {
		t.Fatal("expected a release to be created")
	}
	if release.Status != string(statemachine.ReleaseSyncFailed) {
		t.Errorf("expected release status SYNC_FAILED, got %q", release.Status)
	}

	// SSE event for sync failed
	failedEvents := fakes.eventStore.eventsByType("deploy.sync_failed")
	if len(failedEvents) != 1 {
		t.Errorf("expected 1 deploy.sync_failed event, got %d", len(failedEvents))
	}
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
	cbReq := services.BuildCallbackRequest{
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
	if build.Status != string(statemachine.BuildFailed) {
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
	cbReq := services.BuildCallbackRequest{
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
	req.Apps = []services.AppSpec{
		{Name: "bg-worker", Type: "worker"},
	}

	resp, err := orch.SubmitBuild(context.Background(), req)
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	// Act
	cbReq := services.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []services.AppRelease{
			{Name: "bg-worker", ImageDigest: "sha256:abc123", URL: "", DurationMs: 30000},
		},
	}
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Health prober should NOT have been called
	if len(fakes.health.probed) != 0 {
		t.Errorf("expected no health probes for worker app, got %d: %v", len(fakes.health.probed), fakes.health.probed)
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
	if release.Status != string(statemachine.ReleaseActive) {
		t.Errorf("expected release status ACTIVE, got %q", release.Status)
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
	cbReq := services.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []services.AppRelease{
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

	// SetActive must have been called with the same release ID
	activeID, ok := fakes.envTargetStore.active[envKey]
	if !ok {
		t.Fatal("expected SetActive to be called")
	}
	if activeID != desiredID {
		t.Errorf("expected active and desired release IDs to match: active=%q, desired=%q", activeID, desiredID)
	}
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
		Status:      string(statemachine.BuildQueued),
		CommitSHA:   "abc123",
		CreatedBy:   "user-1",
		CreatedAt:   time.Now().UTC(),
	}

	// Act — callback arrives while build is still QUEUED
	cbReq := services.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []services.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 5000},
		},
	}
	err := orch.OnBuildCompleted(context.Background(), buildID, cbReq)

	// Assert — should not panic or error
	if err != nil {
		t.Fatalf("expected no error for EC-N scenario, got %v", err)
	}

	build, _ := fakes.buildStore.GetByID(context.Background(), buildID)
	if build.Status != string(statemachine.BuildSucceeded) {
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
	cbReq := services.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []services.AppRelease{
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
	cbReq := services.BuildCallbackRequest{
		Status: "succeeded",
		Apps:   []services.AppRelease{}, // empty!
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
	if loopzeroErr.HTTPStatus != 400 {
		t.Errorf("expected HTTP 400, got %d", loopzeroErr.HTTPStatus)
	}
}

func TestBuildOrchestrator_OnBuildCompleted_PreviousReleaseSuperseded(t *testing.T) {
	// Arrange
	fakes := newOrchestratorFakes()
	orch := fakes.buildOrchestrator()

	// Set up a previous active release
	prevRelease := &db.Release{
		ID:          "rel-previous",
		BuildID:     "build-old",
		TenantID:    "tenant-1",
		ProjectID:   "proj-abc123",
		Environment: "production",
		Status:      string(statemachine.ReleaseActive),
		CreatedAt:   time.Now().UTC(),
		GitMirrorStatus: "pending",
	}
	fakes.releaseStore.setActiveByEnv("tenant-1", "proj-abc123", "production", prevRelease)

	// Submit and complete a new build
	resp, err := orch.SubmitBuild(context.Background(), defaultSubmitReq())
	if err != nil {
		t.Fatalf("SubmitBuild failed: %v", err)
	}

	cbReq := services.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []services.AppRelease{
			{Name: "api", ImageDigest: "sha256:new", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 20000},
		},
	}

	// Act
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Previous release should be SUPERSEDED
	prev := fakes.releaseStore.releases["rel-previous"]
	if prev == nil {
		t.Fatal("expected previous release to still exist")
	}
	if prev.Status != string(statemachine.ReleaseSuperseded) {
		t.Errorf("expected previous release status SUPERSEDED, got %q", prev.Status)
	}
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

	cbReq := services.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []services.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc", URL: "https://production.proj-abc123.tenant-1.loopzero.example", DurationMs: 10000},
		},
	}

	// Act
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)
	if err != nil {
		t.Fatalf("OnBuildCompleted failed: %v", err)
	}

	// Assert
	deployEvents := fakes.eventStore.eventsByType("deploy.completed")
	if len(deployEvents) != 1 {
		t.Fatalf("expected 1 deploy.completed event, got %d", len(deployEvents))
	}

	var payload struct {
		DeployID        string `json:"deploy_id"`
		Status          string `json:"status"`
		DurationSeconds int    `json:"duration_seconds"`
		Apps            []struct {
			Name                 string `json:"name"`
			Status               string `json:"status"`
			URL                  string `json:"url"`
			BuildDurationSeconds int    `json:"build_duration_seconds"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(deployEvents[0].Data, &payload); err != nil {
		t.Fatalf("failed to unmarshal deploy.completed: %v", err)
	}

	// deploy_id must be d-{4hex} derived from release ID
	if !strings.HasPrefix(payload.DeployID, "d-") {
		t.Errorf("deploy_id must start with 'd-', got %q", payload.DeployID)
	}
	if len(payload.DeployID) != 6 {
		t.Errorf("deploy_id must be exactly 6 chars (d-XXXX), got %q (len=%d)", payload.DeployID, len(payload.DeployID))
	}

	if payload.Status != "success" {
		t.Errorf("expected status 'success', got %q", payload.Status)
	}

	if len(payload.Apps) != 1 {
		t.Fatalf("expected 1 app in payload, got %d", len(payload.Apps))
	}
	if payload.Apps[0].Name != "api" {
		t.Errorf("expected app name 'api', got %q", payload.Apps[0].Name)
	}
	if payload.Apps[0].Status != "deployed" {
		t.Errorf("expected app status 'deployed', got %q", payload.Apps[0].Status)
	}
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

	cbReq := services.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []services.AppRelease{
			{Name: "api", ImageDigest: "sha256:abc123", URL: "https://production.proj-abc123.tenant-1.loopzero.example", Type: "server", DurationMs: 45000},
		},
	}

	// Act
	err = orch.OnBuildCompleted(context.Background(), resp.BuildID, cbReq)

	// Assert — MUST NOT return error (activation failure is handled internally)
	if err != nil {
		t.Fatalf("expected OnBuildCompleted to handle sync failure gracefully, got error: %v", err)
	}

	// Build MUST be in SUCCEEDED (not RUNNING — the critical invariant)
	build := fakes.buildStore.builds[resp.BuildID]
	if build.Status != string(statemachine.BuildSucceeded) {
		t.Errorf("expected build status SUCCEEDED, got %q — build is stuck!", build.Status)
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
	if release.Status != string(statemachine.ReleaseSyncFailed) {
		t.Errorf("expected release status SYNC_FAILED, got %q", release.Status)
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
	build.Status = string(statemachine.BuildRunning)

	// Act
	err := orch.CancelBuild(context.Background(), build.ID)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Build should be CANCELLED
	updated := fakes.buildStore.builds[build.ID]
	if updated.Status != string(statemachine.BuildCancelled) {
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
	build.Status = string(statemachine.BuildQueued)

	// Act
	err := orch.CancelBuild(context.Background(), build.ID)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// QUEUED → CANCELLED directly (no CANCELLING intermediate)
	updated := fakes.buildStore.builds[build.ID]
	if updated.Status != string(statemachine.BuildCancelled) {
		t.Errorf("expected status CANCELLED, got %s", updated.Status)
	}
}

func TestBuildOrchestrator_CancelBuild_AlreadyTerminal(t *testing.T) {
	// Arrange
	terminalStates := []statemachine.State{
		statemachine.BuildSucceeded,
		statemachine.BuildFailed,
		statemachine.BuildCancelled,
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
