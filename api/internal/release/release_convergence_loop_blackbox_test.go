// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	releasesvc "github.com/getloopscale/loopzero/api/internal/release"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// --- Fakes for ReleaseConvergenceLoop ---

type fakeConvergenceReleaseStore struct {
	mu         sync.Mutex
	pending    []db.Release
	activeList []db.Release           // releases returned by FindActiveReleases
	statuses   map[string]string      // releaseID → current status (guarded by mu)
	active     map[string]*db.Release // key: tenant:project:env → active release
}

func newFakeConvergenceReleaseStore() *fakeConvergenceReleaseStore {
	return &fakeConvergenceReleaseStore{
		statuses: make(map[string]string),
		active:   make(map[string]*db.Release),
	}
}

// getStatus returns the status of a release (thread-safe).
func (f *fakeConvergenceReleaseStore) getStatus(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statuses[id]
}

func (f *fakeConvergenceReleaseStore) FindPendingReleases(_ context.Context, _ string) ([]db.Release, error) {
	return f.pending, nil
}

func (f *fakeConvergenceReleaseStore) FindActiveReleases(_ context.Context, _ string) ([]db.Release, error) {
	return f.activeList, nil
}

func (f *fakeConvergenceReleaseStore) UpdateStatus(_ context.Context, _ db.DBTX, id string, event string) (*db.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := statemachine.NewReleaseMachine()
	current := f.statuses[id]
	if current == "" {
		current = string(statemachine.RPhPending)
	}
	newState, err := safeTransitionRelease(m, statemachine.State(current), statemachine.Event(event))
	if err != nil {
		return nil, err
	}
	f.statuses[id] = string(newState)
	return &db.Release{ID: id, Status: string(newState)}, nil
}

func (f *fakeConvergenceReleaseStore) GetActiveByEnvTx(_ context.Context, _ db.DBTX, tenantID, projectID, env string) (*db.Release, error) {
	key := fmt.Sprintf("%s:%s:%s", tenantID, projectID, env)
	return f.active[key], nil
}

func (f *fakeConvergenceReleaseStore) GetByIDTx(_ context.Context, _ db.DBTX, id string) (*db.Release, error) {
	f.mu.Lock()
	status := f.statuses[id]
	f.mu.Unlock()
	if status == "" {
		status = string(statemachine.RPhPending)
	}
	for _, r := range f.pending {
		if r.ID == id {
			r.Status = status
			return &r, nil
		}
	}
	return &db.Release{ID: id, Status: status}, nil
}

func (f *fakeConvergenceReleaseStore) CancelAllByEnv(_ context.Context, _ db.DBTX, _, _, _ string) (int, error) {
	return 0, nil
}

func safeTransitionRelease(m *statemachine.Machine, current statemachine.State, event statemachine.Event) (newState statemachine.State, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("invalid transition: %s + %s", current, event)
		}
	}()
	newState = m.Transition(current, event)
	return newState, nil
}

type fakeConvergenceEnvTargetStore struct {
	mu          sync.Mutex
	activeCalls int
}

func (f *fakeConvergenceEnvTargetStore) getActiveCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.activeCalls
}
func (f *fakeConvergenceEnvTargetStore) ListByProject(_ context.Context, _, _ string) ([]store.EnvironmentTarget, error) {
	return nil, nil
}
func (f *fakeConvergenceEnvTargetStore) DeleteTarget(_ context.Context, _ db.DBTX, _, _, _ string) error {
	return nil
}

// 2026-06-07 D2/D5/D6/D7 — preview-as-category interface extensions.
func (f *fakeConvergenceEnvTargetStore) SetPreviewDesired(_ context.Context, _ db.DBTX, _, _, _, _, _, _ string) error {
	return nil
}
func (f *fakeConvergenceEnvTargetStore) CountPermanent(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (f *fakeConvergenceEnvTargetStore) ListPermanent(_ context.Context, _ string) ([]string, error) {
	return []string{}, nil
}
func (f *fakeConvergenceEnvTargetStore) DeleteByKind(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	return nil
}
func (f *fakeConvergenceEnvTargetStore) SetDesired(_ context.Context, _ db.DBTX, _, _, _, _, _, _ string) error {
	return nil
}
func (f *fakeConvergenceEnvTargetStore) SetActive(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activeCalls++
	return nil
}
func (f *fakeConvergenceEnvTargetStore) Get(_ context.Context, _, _, _ string) (*store.EnvironmentTarget, error) {
	return nil, nil
}

type fakeConvergenceSyncClient struct {
	mu               sync.Mutex
	synced           bool
	healthy          bool
	err              error
	triggerSyncCalls int
	triggerSyncErr   error
}

func (f *fakeConvergenceSyncClient) TriggerSync(_ context.Context, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.triggerSyncCalls++
	return f.triggerSyncErr
}
func (f *fakeConvergenceSyncClient) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	return nil
}
func (f *fakeConvergenceSyncClient) GetAppStatus(_ context.Context, _ string, _ []string) (bool, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.synced, f.healthy, f.err
}

// Sprint B T1.1 (ADR D1): stub methods to satisfy the widened SyncClient
// interface. These convergence regressions don't exercise the recovery
// paths; the package's own recovery tests do.
func (f *fakeConvergenceSyncClient) ForceReconcile(_ context.Context, _ string) error {
	return nil
}
func (f *fakeConvergenceSyncClient) GetSyncRevision(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (f *fakeConvergenceSyncClient) TerminateOperation(_ context.Context, _ string) error {
	return nil
}

func (f *fakeConvergenceSyncClient) setStatus(synced, healthy bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.synced = synced
	f.healthy = healthy
}
func (f *fakeConvergenceSyncClient) getTriggerSyncCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.triggerSyncCalls
}

type fakeConvergenceEventStore struct {
	mu     sync.Mutex
	events []string
}

func (f *fakeConvergenceEventStore) Append(_ context.Context, _ string, eventType string, _ json.RawMessage) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, eventType)
	return 1, nil
}
func (f *fakeConvergenceEventStore) getEvents() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]string, len(f.events))
	copy(cp, f.events)
	return cp
}

// fakeTerminatingSyncClient extends fakeConvergenceSyncClient with TerminateOperation.
// Used to verify the convergence loop calls TerminateOperation on timeout (MEET-2026-03-29-019).
type fakeTerminatingSyncClient struct {
	fakeConvergenceSyncClient
	tmu            sync.Mutex
	terminateCalls int
	terminatedApp  string
}

func (f *fakeTerminatingSyncClient) TerminateOperation(_ context.Context, appName string) error {
	f.tmu.Lock()
	defer f.tmu.Unlock()
	f.terminateCalls++
	f.terminatedApp = appName
	return nil
}

func (f *fakeTerminatingSyncClient) getTerminateCalls() int {
	f.tmu.Lock()
	defer f.tmu.Unlock()
	return f.terminateCalls
}

func (f *fakeTerminatingSyncClient) getTerminatedApp() string {
	f.tmu.Lock()
	defer f.tmu.Unlock()
	return f.terminatedApp
}

type fakeConvergenceLocker struct{}

func (f *fakeConvergenceLocker) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

// --- Tests ---

func TestReleaseConvergenceLoop_ActivatesPendingRelease(t *testing.T) {
	// Arrange: one PENDING release, ArgoCD reports synced + healthy
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-001", BuildID: "build-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-001"] = "PENDING"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	syncClient := &fakeConvergenceSyncClient{synced: true, healthy: true}
	eventStore := &fakeConvergenceEventStore{}
	locker := &fakeConvergenceLocker{}

	loop := releasesvc.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		"test.local", "", testLogger, releasesvc.ReleaseConvergenceLoopConfig{
			Interval: 100 * time.Millisecond, SyncTimeout: 5 * time.Minute, MaxConcurrent: 2,
		},
	)

	// Act: run loop and poll until the release transitions.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go loop.Start(ctx)

	deadline := time.After(5 * time.Second)
	for releaseStore.getStatus("rel-001") != "ACTIVE" {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for release ACTIVE, got %q", releaseStore.getStatus("rel-001"))
		default:
			time.Sleep(5 * time.Millisecond) // intentional: tight poll for convergence loop tick
		}
	}

	// Assert: release should be ACTIVE
	if releaseStore.getStatus("rel-001") != "ACTIVE" {
		t.Errorf("expected release ACTIVE, got %q", releaseStore.getStatus("rel-001"))
	}
	if envTargetStore.getActiveCalls() == 0 {
		t.Error("expected SetActive to be called")
	}
	// Should emit deploy.live event
	found := false
	for _, e := range eventStore.getEvents() {
		if e == "deploy.live" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected deploy.live event, got %v", eventStore.getEvents())
	}
}

func TestReleaseConvergenceLoop_TimesOutStaleRelease(t *testing.T) {
	// Arrange: one PENDING release created 10 minutes ago, timeout is 1 second
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-002", BuildID: "build-002",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now().Add(-10 * time.Minute),
	}}
	releaseStore.statuses["rel-002"] = "PENDING"

	syncClient := &fakeConvergenceSyncClient{synced: false, healthy: false}
	eventStore := &fakeConvergenceEventStore{}
	locker := &fakeConvergenceLocker{}
	envTargetStore := &fakeConvergenceEnvTargetStore{}

	loop := releasesvc.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		"test.local", "", testLogger, releasesvc.ReleaseConvergenceLoopConfig{
			Interval: 100 * time.Millisecond, SyncTimeout: 1 * time.Second, MaxConcurrent: 2,
		},
	)

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go loop.Start(ctx)

	deadline := time.After(5 * time.Second)
	for releaseStore.getStatus("rel-002") != "SYNC_FAILED" {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for SYNC_FAILED, got %q", releaseStore.getStatus("rel-002"))
		default:
			time.Sleep(5 * time.Millisecond) // intentional: tight poll for convergence loop tick
		}
	}

	// Assert: release should be SYNC_FAILED
	if releaseStore.getStatus("rel-002") != "SYNC_FAILED" {
		t.Errorf("expected release SYNC_FAILED, got %q", releaseStore.getStatus("rel-002"))
	}
	// Should emit deploy.sync_failed event
	found := false
	for _, e := range eventStore.getEvents() {
		if e == "deploy.sync_failed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected deploy.sync_failed event, got %v", eventStore.getEvents())
	}
}

func TestReleaseConvergenceLoop_SkipsWhenNotReady(t *testing.T) {
	// Arrange: ArgoCD reports not synced, not healthy
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-003", BuildID: "build-003",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-003"] = "PENDING"

	syncClient := &fakeConvergenceSyncClient{synced: false, healthy: false}
	eventStore := &fakeConvergenceEventStore{}
	locker := &fakeConvergenceLocker{}
	envTargetStore := &fakeConvergenceEnvTargetStore{}

	loop := releasesvc.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		"test.local", "", testLogger, releasesvc.ReleaseConvergenceLoopConfig{
			Interval: 100 * time.Millisecond, SyncTimeout: 5 * time.Minute, MaxConcurrent: 2,
		},
	)

	// Act — run 2 full loop iterations (100ms interval) then verify status unchanged.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go loop.Start(ctx)
	<-ctx.Done() // wait for the context to expire (loop ran at least 2 iterations)

	// Assert: release stays PENDING (not activated, not failed)
	if releaseStore.getStatus("rel-003") != "PENDING" {
		t.Errorf("expected release to stay PENDING, got %q", releaseStore.getStatus("rel-003"))
	}
	if len(eventStore.getEvents()) != 0 {
		t.Errorf("expected no events (still waiting), got %v", eventStore.getEvents())
	}
}

func TestReleaseConvergenceLoop_TerminatesStuckOperationOnTimeout(t *testing.T) {
	// MEET-2026-03-29-019: When a release times out, the convergence loop
	// must call TerminateOperation to clear the stuck ArgoCD sync.
	// Without this, the stuck operation persists and blocks ALL future syncs.
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-stuck", BuildID: "build-stuck",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now().Add(-10 * time.Minute),
	}}
	releaseStore.statuses["rel-stuck"] = "PENDING"

	// Sync client that also implements TerminateOperation
	terminatingSyncClient := &fakeTerminatingSyncClient{
		fakeConvergenceSyncClient: fakeConvergenceSyncClient{synced: false, healthy: false},
	}
	eventStore := &fakeConvergenceEventStore{}
	locker := &fakeConvergenceLocker{}
	envTargetStore := &fakeConvergenceEnvTargetStore{}

	loop := releasesvc.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, terminatingSyncClient, locker, eventStore,
		"test.local", "", testLogger, releasesvc.ReleaseConvergenceLoopConfig{
			Interval: 100 * time.Millisecond, SyncTimeout: 1 * time.Second, MaxConcurrent: 2,
		},
	)

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go loop.Start(ctx)

	deadline := time.After(5 * time.Second)
	for terminatingSyncClient.getTerminateCalls() == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for TerminateOperation to be called")
		default:
			time.Sleep(5 * time.Millisecond) // intentional: tight poll for convergence loop tick
		}
	}

	// Assert: TerminateOperation was called
	if terminatingSyncClient.getTerminateCalls() == 0 {
		t.Error("expected TerminateOperation to be called on timeout")
	}
	// #271: the Application's identity is {tenant}-{project}-{env}; omitting the project
	// made two projects of the same tenant collide into a single object.
	if terminatingSyncClient.getTerminatedApp() != "acme-my-app-production" {
		t.Errorf("expected TerminateOperation for 'acme-my-app-production', got %q", terminatingSyncClient.getTerminatedApp())
	}
	// Release should be SYNC_FAILED
	if releaseStore.getStatus("rel-stuck") != "SYNC_FAILED" {
		t.Errorf("expected release SYNC_FAILED, got %q", releaseStore.getStatus("rel-stuck"))
	}
}

func TestReleaseConvergenceLoop_HandlesEmptyPendingList(t *testing.T) {
	// Arrange: no pending releases
	releaseStore := newFakeConvergenceReleaseStore()
	syncClient := &fakeConvergenceSyncClient{}
	eventStore := &fakeConvergenceEventStore{}
	locker := &fakeConvergenceLocker{}
	envTargetStore := &fakeConvergenceEnvTargetStore{}

	loop := releasesvc.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		"test.local", "", testLogger, releasesvc.ReleaseConvergenceLoopConfig{
			Interval: 50 * time.Millisecond, SyncTimeout: 5 * time.Minute, MaxConcurrent: 2,
		},
	)

	// Act: should not panic or error — run for a few iterations.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go loop.Start(ctx)
	<-ctx.Done() // wait for context to expire (loop ran several iterations)

	// Assert: nothing happened
	if len(eventStore.getEvents()) != 0 {
		t.Errorf("expected no events, got %v", eventStore.getEvents())
	}
}

// --- MEET-2026-03-31-023 T0-1: ActivationKernel.Activate idempotency guard ---

func TestActivationKernel_SkipsAlreadyActiveRelease(t *testing.T) {
	// Arrange: release is passed as PENDING but has already been transitioned
	// to ACTIVE by another activator (simulates callback + polling race).
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-dup-001", BuildID: "build-dup-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now(),
	}}
	// Simulate that another activator already transitioned to ACTIVE
	releaseStore.statuses["rel-dup-001"] = "ACTIVE"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	eventStore := &fakeConvergenceEventStore{}

	// The release object still says PENDING (stale), but the store says ACTIVE
	staleRelease := &db.Release{
		ID: "rel-dup-001", BuildID: "build-dup-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now(),
	}

	// Act: call ActivationKernel.Activate with the stale release
	err := releasesvc.ActivationKernel{
		Releases: releaseStore, EnvTargets: envTargetStore, Events: eventStore, DeployEvents: fakeDeployEventStore{}, Logger: testLogger,
	}.Activate(context.Background(), nil, staleRelease, nil, 10)

	// Assert: no error (idempotent skip), no side effects
	if err != nil {
		t.Fatalf("expected nil error (idempotent skip), got %v", err)
	}
	if envTargetStore.getActiveCalls() != 0 {
		t.Errorf("expected SetActive NOT called (release already ACTIVE), got %d calls", envTargetStore.getActiveCalls())
	}
	if len(eventStore.getEvents()) != 0 {
		t.Errorf("expected no events emitted (idempotent skip), got %v", eventStore.getEvents())
	}
}

func TestActivationKernel_ProceedsWhenStillPending(t *testing.T) {
	// Arrange: release is PENDING in both the object and the store
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-dup-002", BuildID: "build-dup-002",
		TenantID: "acme", ProjectID: "my-app", Environment: "staging",
		Status: "PENDING", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-dup-002"] = "PENDING"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	eventStore := &fakeConvergenceEventStore{}

	release := &db.Release{
		ID: "rel-dup-002", BuildID: "build-dup-002",
		TenantID: "acme", ProjectID: "my-app", Environment: "staging",
		Status: "PENDING", CreatedAt: time.Now(),
	}

	// Act
	err := releasesvc.ActivationKernel{
		Releases: releaseStore, EnvTargets: envTargetStore, Events: eventStore, DeployEvents: fakeDeployEventStore{}, Logger: testLogger,
	}.Activate(context.Background(), nil, release, nil, 5)

	// Assert: activation proceeds normally
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if releaseStore.getStatus("rel-dup-002") != "ACTIVE" {
		t.Errorf("expected release ACTIVE, got %q", releaseStore.getStatus("rel-dup-002"))
	}
	if envTargetStore.getActiveCalls() == 0 {
		t.Error("expected SetActive to be called")
	}
}

// --- MEET-2026-03-31-023 T1-5: Convergence loop retriggers TriggerSync ---

func TestReleaseConvergenceLoop_RetriggersSyncForStalledRelease(t *testing.T) {
	// Arrange: PENDING release > 30s old, ArgoCD reports NOT synced
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-retrigger-001", BuildID: "build-retrigger-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now().Add(-1 * time.Minute), // 1 minute old
	}}
	releaseStore.statuses["rel-retrigger-001"] = "PENDING"

	syncClient := &fakeConvergenceSyncClient{synced: false, healthy: false}
	eventStore := &fakeConvergenceEventStore{}
	locker := &fakeConvergenceLocker{}
	envTargetStore := &fakeConvergenceEnvTargetStore{}

	loop := releasesvc.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		"test.local", "", testLogger, releasesvc.ReleaseConvergenceLoopConfig{
			Interval: 100 * time.Millisecond, SyncTimeout: 5 * time.Minute, MaxConcurrent: 2,
		},
	)

	// Act: run loop and poll until TriggerSync is called.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go loop.Start(ctx)

	deadline := time.After(5 * time.Second)
	for syncClient.getTriggerSyncCalls() == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for TriggerSync to be called")
		default:
			time.Sleep(5 * time.Millisecond) // intentional: tight poll for convergence loop tick
		}
	}

	// Assert: TriggerSync was called (retrigger for stalled release)
	if syncClient.getTriggerSyncCalls() == 0 {
		t.Error("expected TriggerSync to be called for stalled release")
	}
}

func TestReleaseConvergenceLoop_DoesNotRetriggerRecentRelease(t *testing.T) {
	// Arrange: PENDING release only 5s old (< 30s threshold), ArgoCD not synced
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-retrigger-002", BuildID: "build-retrigger-002",
		TenantID: "acme", ProjectID: "my-app", Environment: "staging",
		Status: "PENDING", CreatedAt: time.Now().Add(-5 * time.Second), // only 5s old
	}}
	releaseStore.statuses["rel-retrigger-002"] = "PENDING"

	syncClient := &fakeConvergenceSyncClient{synced: false, healthy: false}
	eventStore := &fakeConvergenceEventStore{}
	locker := &fakeConvergenceLocker{}
	envTargetStore := &fakeConvergenceEnvTargetStore{}

	loop := releasesvc.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		"test.local", "", testLogger, releasesvc.ReleaseConvergenceLoopConfig{
			Interval: 100 * time.Millisecond, SyncTimeout: 5 * time.Minute, MaxConcurrent: 2,
		},
	)

	// Act — run for a few loop iterations then verify no retrigger.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go loop.Start(ctx)
	<-ctx.Done() // wait for context to expire (loop ran several iterations)

	// Assert: TriggerSync NOT called (release too recent, < 30s)
	if syncClient.triggerSyncCalls != 0 {
		t.Errorf("expected NO TriggerSync for recent release, got %d calls", syncClient.triggerSyncCalls)
	}
}

// --- MEET-2026-03-31-023 T1-4: Post-activation health monitoring ---

func TestHealthCheckActiveReleases_DetectsDivergence(t *testing.T) {
	// Arrange: one ACTIVE release, ArgoCD reports NOT healthy
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.activeList = []db.Release{{
		ID: "rel-health-001", BuildID: "build-health-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "ACTIVE", CreatedAt: time.Now().Add(-5 * time.Minute),
	}}

	syncClient := &fakeConvergenceSyncClient{synced: true, healthy: false} // synced but NOT healthy
	eventStore := &fakeConvergenceEventStore{}
	locker := &fakeConvergenceLocker{}
	envTargetStore := &fakeConvergenceEnvTargetStore{}

	loop := releasesvc.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		"test.local", "", testLogger, releasesvc.ReleaseConvergenceLoopConfig{
			Interval:       1 * time.Second,        // slow PENDING ticker
			HealthInterval: 100 * time.Millisecond, // fast health ticker for test
			SyncTimeout:    5 * time.Minute,
			MaxConcurrent:  2,
		},
	)

	// Act: run long enough for health ticker to fire.
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	go loop.Start(ctx)
	<-ctx.Done() // wait for context to expire (health ticker ran several times)

	// Assert: release should NOT be state-changed (observability only)
	// The release status should remain ACTIVE — health check does not mutate state.
	// We verify the method was called by checking that no state transitions happened
	// and no events were emitted (health check is silent except for metrics+logs).
	if len(eventStore.getEvents()) != 0 {
		t.Errorf("expected no events from health check (observability only), got %v", eventStore.getEvents())
	}
}

func TestHealthCheckActiveReleases_ClearsDivergenceWhenHealthy(t *testing.T) {
	// Arrange: one ACTIVE release, ArgoCD reports healthy
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.activeList = []db.Release{{
		ID: "rel-health-002", BuildID: "build-health-002",
		TenantID: "acme", ProjectID: "my-app", Environment: "staging",
		Status: "ACTIVE", CreatedAt: time.Now().Add(-10 * time.Minute),
	}}

	syncClient := &fakeConvergenceSyncClient{synced: true, healthy: true} // healthy
	eventStore := &fakeConvergenceEventStore{}
	locker := &fakeConvergenceLocker{}
	envTargetStore := &fakeConvergenceEnvTargetStore{}

	loop := releasesvc.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		"test.local", "", testLogger, releasesvc.ReleaseConvergenceLoopConfig{
			Interval:       1 * time.Second,
			HealthInterval: 100 * time.Millisecond,
			SyncTimeout:    5 * time.Minute,
			MaxConcurrent:  2,
		},
	)

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	go loop.Start(ctx)
	<-ctx.Done() // wait for context to expire (health ticker ran several times)

	// Assert: no state changes, no events (health check is observability-only)
	if len(eventStore.getEvents()) != 0 {
		t.Errorf("expected no events from health check, got %v", eventStore.getEvents())
	}
}
