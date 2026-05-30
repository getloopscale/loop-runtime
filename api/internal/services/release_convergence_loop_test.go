// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// --- Test doubles for ReleaseConvergenceLoop ---

type fakeConvergenceReleaseStore struct {
	mu          sync.Mutex
	pending     []db.Release
	active      []db.Release
	releases    map[string]*db.Release
	lastStatus  map[string]string
	activeByEnv map[string]*db.Release
}

func newFakeConvergenceReleaseStore() *fakeConvergenceReleaseStore {
	return &fakeConvergenceReleaseStore{
		releases:    make(map[string]*db.Release),
		lastStatus:  make(map[string]string),
		activeByEnv: make(map[string]*db.Release),
	}
}

func (f *fakeConvergenceReleaseStore) FindPendingReleases(_ context.Context, _ string) ([]db.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pending, nil
}
func (f *fakeConvergenceReleaseStore) FindActiveReleases(_ context.Context, _ string) ([]db.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active, nil
}
func (f *fakeConvergenceReleaseStore) UpdateStatus(_ context.Context, _ db.DBTX, id, event string) (*db.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastStatus[id] = event
	if r, ok := f.releases[id]; ok {
		return r, nil
	}
	return nil, nil
}
func (f *fakeConvergenceReleaseStore) GetActiveByEnvTx(_ context.Context, _ db.DBTX, tenant, project, env string) (*db.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.activeByEnv[tenant+"-"+project+"-"+env], nil
}
func (f *fakeConvergenceReleaseStore) GetByIDTx(_ context.Context, _ db.DBTX, id string) (*db.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.releases[id], nil
}

func (f *fakeConvergenceReleaseStore) CancelAllByEnv(_ context.Context, _ db.DBTX, _, _, _ string) (int, error) {
	return 0, nil
}

// getStatus safely reads lastStatus under lock.
func (f *fakeConvergenceReleaseStore) getStatus(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastStatus[id]
}

type fakeConvergenceEnvTargetStore struct {
	desired map[string]string
	active  map[string]string
}

func newFakeConvergenceEnvTargetStore() *fakeConvergenceEnvTargetStore {
	return &fakeConvergenceEnvTargetStore{
		desired: make(map[string]string),
		active:  make(map[string]string),
	}
}

func (f *fakeConvergenceEnvTargetStore) SetDesired(_ context.Context, _ db.DBTX, t, p, e, rel, _, _ string) error {
	f.desired[t+"-"+p+"-"+e] = rel
	return nil
}
func (f *fakeConvergenceEnvTargetStore) SetActive(_ context.Context, _ db.DBTX, t, p, e, rel string) error {
	f.active[t+"-"+p+"-"+e] = rel
	return nil
}
func (f *fakeConvergenceEnvTargetStore) Get(_ context.Context, _, _, _ string) (*db.EnvironmentTarget, error) {
	return nil, nil
}
func (f *fakeConvergenceEnvTargetStore) ListByProject(_ context.Context, _, _ string) ([]db.EnvironmentTarget, error) {
	return nil, nil
}
func (f *fakeConvergenceEnvTargetStore) DeleteTarget(_ context.Context, _ db.DBTX, _, _, _ string) error {
	return nil
}

type fakeConvergenceSyncClient struct {
	synced  bool
	healthy bool
	syncErr error
}

func (f *fakeConvergenceSyncClient) TriggerSync(_ context.Context, _ string) error {
	f.synced = true
	return f.syncErr
}
func (f *fakeConvergenceSyncClient) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	if f.healthy {
		return nil
	}
	return context.DeadlineExceeded // not ready
}
func (f *fakeConvergenceSyncClient) GetAppStatus(_ context.Context, _ string, _ []string) (bool, bool, error) {
	return f.synced, f.healthy, nil
}

type fakeConvergenceEventStore struct {
	mu     sync.Mutex
	events []struct{ BuildID, EventType string }
	nextID int64
}

func (f *fakeConvergenceEventStore) Append(_ context.Context, buildID, eventType string, _ json.RawMessage) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.events = append(f.events, struct{ BuildID, EventType string }{buildID, eventType})
	return f.nextID, nil
}

func (f *fakeConvergenceEventStore) hasEvent(eventType string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.EventType == eventType {
			return true
		}
	}
	return false
}

type fakeConvergenceLocker struct{}

func (f *fakeConvergenceLocker) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

// --- Tests ---

func TestReleaseConvergenceLoop_NoPending_NoAction(t *testing.T) {
	rs := newFakeConvergenceReleaseStore()
	loop := NewReleaseConvergenceLoop(
		rs,
		newFakeConvergenceEnvTargetStore(),
		&fakeConvergenceSyncClient{},
		&fakeConvergenceLocker{},
		&fakeConvergenceEventStore{},
		"dev.loopzero.example",
		slog.Default(),
		ReleaseConvergenceLoopConfig{Interval: time.Second, SyncTimeout: time.Minute},
	)

	// Act
	loop.tick(context.Background())

	// Assert: no status changes
	if len(rs.lastStatus) > 0 {
		t.Errorf("expected no status changes, got %v", rs.lastStatus)
	}
}

func TestReleaseConvergenceLoop_PendingRelease_SyncedAndHealthy_Activates(t *testing.T) {
	rs := newFakeConvergenceReleaseStore()
	pendingRelease := db.Release{
		ID: "rel-1", BuildID: "build-1", TenantID: "tn-1",
		ProjectID: "proj-1", Environment: "production",
		Status:    string(statemachine.ReleasePending),
		CreatedAt: time.Now(),
		Apps:      json.RawMessage(`[{"name":"api","rendering":"container"}]`),
	}
	rs.pending = []db.Release{pendingRelease}
	rs.releases["rel-1"] = &pendingRelease

	ets := newFakeConvergenceEnvTargetStore()
	es := &fakeConvergenceEventStore{}
	syncClient := &fakeConvergenceSyncClient{synced: true, healthy: true}

	loop := NewReleaseConvergenceLoop(
		rs, ets, syncClient, &fakeConvergenceLocker{}, es,
		"dev.loopzero.example", slog.Default(),
		ReleaseConvergenceLoopConfig{Interval: time.Second, SyncTimeout: 5 * time.Minute, MaxConcurrent: 1},
	)

	// Act
	loop.tick(context.Background())

	// Poll until goroutine completes (race-safe)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := rs.getStatus("rel-1"); s != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Assert: release should be activated (SyncCompleted event)
	if got := rs.getStatus("rel-1"); got != string(statemachine.ReleaseSyncCompleted) {
		t.Errorf("expected SyncCompleted, got %q", got)
	}
}

func TestReleaseConvergenceLoop_PendingRelease_TimedOut_FailsRelease(t *testing.T) {
	rs := newFakeConvergenceReleaseStore()
	oldRelease := db.Release{
		ID: "rel-old", BuildID: "build-old", TenantID: "tn-1",
		ProjectID: "proj-1", Environment: "production",
		Status:    string(statemachine.ReleasePending),
		CreatedAt: time.Now().Add(-10 * time.Minute), // way past timeout
		Apps:      json.RawMessage(`[]`),
	}
	rs.pending = []db.Release{oldRelease}
	rs.releases["rel-old"] = &oldRelease

	es := &fakeConvergenceEventStore{}
	syncClient := &fakeConvergenceSyncClient{synced: false, healthy: false}

	loop := NewReleaseConvergenceLoop(
		rs, newFakeConvergenceEnvTargetStore(), syncClient, &fakeConvergenceLocker{}, es,
		"dev.loopzero.example", slog.Default(),
		ReleaseConvergenceLoopConfig{Interval: time.Second, SyncTimeout: 3 * time.Minute, MaxConcurrent: 1},
	)

	// Act
	loop.tick(context.Background())

	// Poll until goroutine completes (race-safe)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := rs.getStatus("rel-old"); s != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Assert: release should be failed (SyncFailed event)
	if got := rs.getStatus("rel-old"); got != string(statemachine.ReleaseSyncFailed_) {
		t.Errorf("expected SyncFailed, got %q", got)
	}

	// Should emit deploy.sync_failed event
	if !es.hasEvent("deploy.sync_failed") {
		t.Error("expected deploy.sync_failed event")
	}
}

func TestReleaseConvergenceLoop_PendingRelease_NotReady_Retries(t *testing.T) {
	rs := newFakeConvergenceReleaseStore()
	recentRelease := db.Release{
		ID: "rel-new", BuildID: "build-new", TenantID: "tn-1",
		ProjectID: "proj-1", Environment: "production",
		Status:    string(statemachine.ReleasePending),
		CreatedAt: time.Now(), // recent
		Apps:      json.RawMessage(`[{"name":"api"}]`),
	}
	rs.pending = []db.Release{recentRelease}
	rs.releases["rel-new"] = &recentRelease

	syncClient := &fakeConvergenceSyncClient{synced: false, healthy: false}

	loop := NewReleaseConvergenceLoop(
		rs, newFakeConvergenceEnvTargetStore(), syncClient, &fakeConvergenceLocker{},
		&fakeConvergenceEventStore{},
		"dev.loopzero.example", slog.Default(),
		ReleaseConvergenceLoopConfig{Interval: time.Second, SyncTimeout: 5 * time.Minute, MaxConcurrent: 1},
	)

	// Act
	loop.tick(context.Background())
	time.Sleep(200 * time.Millisecond) // Allow goroutine to run

	// Assert: no status change (not activated, not failed — will retry next tick)
	if got := rs.getStatus("rel-new"); got != "" {
		t.Errorf("expected no status change for not-ready release, got %q", got)
	}
}

func TestReleaseConvergenceLoop_DefaultConfig(t *testing.T) {
	loop := NewReleaseConvergenceLoop(
		newFakeConvergenceReleaseStore(),
		newFakeConvergenceEnvTargetStore(),
		&fakeConvergenceSyncClient{},
		&fakeConvergenceLocker{},
		&fakeConvergenceEventStore{},
		"dev.loopzero.example",
		slog.Default(),
		ReleaseConvergenceLoopConfig{}, // all zeros
	)

	if loop.cfg.Interval != 10*time.Second {
		t.Errorf("expected default interval 10s, got %v", loop.cfg.Interval)
	}
	if loop.cfg.SyncTimeout != 180*time.Second {
		t.Errorf("expected default sync timeout 180s, got %v", loop.cfg.SyncTimeout)
	}
	if loop.cfg.HealthInterval != 60*time.Second {
		t.Errorf("expected default health interval 60s, got %v", loop.cfg.HealthInterval)
	}
	if loop.cfg.MaxConcurrent != 10 {
		t.Errorf("expected default max concurrent 10, got %d", loop.cfg.MaxConcurrent)
	}
}

func TestReleaseConvergenceLoop_HealthCheck_ActiveUnhealthy_EmitsMetric(t *testing.T) {
	rs := newFakeConvergenceReleaseStore()
	rs.active = []db.Release{
		{
			ID: "rel-active", BuildID: "build-1", TenantID: "tn-1",
			ProjectID: "proj-1", Environment: "production",
			Status: string(statemachine.ReleaseActive),
			Apps:   json.RawMessage(`[{"name":"api","rendering":"container"}]`),
		},
	}

	syncClient := &fakeConvergenceSyncClient{synced: true, healthy: false} // unhealthy

	loop := NewReleaseConvergenceLoop(
		rs, newFakeConvergenceEnvTargetStore(), syncClient, &fakeConvergenceLocker{},
		&fakeConvergenceEventStore{},
		"dev.loopzero.example", slog.Default(),
		ReleaseConvergenceLoopConfig{Interval: time.Second, HealthInterval: time.Second},
	)

	// Act: should not panic, should log warning + emit metrics
	loop.healthCheckActiveReleases(context.Background())

	// Assert: no status changes (health check is observability-only)
	if len(rs.lastStatus) > 0 {
		t.Error("health check should NOT change release state")
	}
}

// --- V9 L21 tests ---
//
// L21 (commit a7daa7fc): when TriggerSync returns ErrSyncOperationInProgress
// twice in a row, the convergence loop calls TerminateOperation to clear
// the stale ArgoCD sync lock pre-timeout, rather than waiting 5min for the
// sync_timeout path to fire TerminateOperation (which fails the release).

// l21SyncClient counts TriggerSync + TerminateOperation invocations and
// returns a configurable error from TriggerSync. Mirrors fake pattern
// used in the rest of this file (no testify, no mockery).
type l21SyncClient struct {
	triggerCount    int
	terminateCount  int
	triggerErr      error
}

func (c *l21SyncClient) TriggerSync(_ context.Context, _ string) error {
	c.triggerCount++
	return c.triggerErr
}
func (c *l21SyncClient) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	return nil
}
func (c *l21SyncClient) GetAppStatus(_ context.Context, _ string, _ []string) (bool, bool, error) {
	return false, false, nil
}
func (c *l21SyncClient) TerminateOperation(_ context.Context, _ string) error {
	c.terminateCount++
	return nil
}

func TestRetriggerSync_L21_TerminatesAfterTwoConsecutiveLockStuck(t *testing.T) {
	// Arrange: SyncClient that always returns ErrSyncOperationInProgress.
	client := &l21SyncClient{triggerErr: loopzeroerrors.ErrSyncOperationInProgress}

	loop := NewReleaseConvergenceLoop(
		newFakeConvergenceReleaseStore(),
		newFakeConvergenceEnvTargetStore(),
		client,
		&fakeConvergenceLocker{},
		&fakeConvergenceEventStore{},
		"dev.loopzero.example", slog.Default(),
		ReleaseConvergenceLoopConfig{},
	)

	release := &db.Release{ID: "rel-l21-1", TenantID: "tn-1", Environment: "production"}

	// Act 1: first retrigger — should fail with lock-stuck, increment counter to 1.
	loop.retriggerSyncIfNeeded(context.Background(), release, "tn-1-production")

	if client.terminateCount != 0 {
		t.Errorf("after 1 lock-stuck failure, expected 0 TerminateOperation calls; got %d", client.terminateCount)
	}

	// Act 2: second retrigger — counter hits 2, TerminateOperation MUST fire.
	loop.retriggerSyncIfNeeded(context.Background(), release, "tn-1-production")

	if client.terminateCount != 1 {
		t.Errorf("after 2 consecutive lock-stuck failures, expected 1 TerminateOperation call (L21 pre-timeout recovery); got %d", client.terminateCount)
	}
}

func TestRetriggerSync_L21_DoesNotTerminateOnNonLockErrors(t *testing.T) {
	// Arrange: SyncClient that returns a generic non-lock error.
	client := &l21SyncClient{triggerErr: fmt.Errorf("generic transient network error")}

	loop := NewReleaseConvergenceLoop(
		newFakeConvergenceReleaseStore(),
		newFakeConvergenceEnvTargetStore(),
		client,
		&fakeConvergenceLocker{},
		&fakeConvergenceEventStore{},
		"dev.loopzero.example", slog.Default(),
		ReleaseConvergenceLoopConfig{},
	)

	release := &db.Release{ID: "rel-l21-2", TenantID: "tn-1", Environment: "production"}

	// Act: 5 consecutive non-lock failures.
	for i := 0; i < 5; i++ {
		loop.retriggerSyncIfNeeded(context.Background(), release, "tn-1-production")
	}

	// Assert: TerminateOperation should NEVER fire for non-lock errors.
	// L21 is targeted at ErrSyncOperationInProgress specifically.
	if client.terminateCount != 0 {
		t.Errorf("non-lock errors must NOT trigger TerminateOperation; got %d calls", client.terminateCount)
	}
}

// --- V9 L22 tests ---
//
// L22 (commit pending): when ArgoCD application-controller workqueue
// stalls (controller alive but not processing Apps), the convergence
// loop calls ForceReconcile to PATCH the App with refresh=hard
// annotation, kicking the controller back into action.
//
// Different from L21:
// - L21 fires when TriggerSync RETURNS error SYNC_OPERATION_IN_PROGRESS
// - L22 fires when TriggerSync SUCCEEDS but ArgoCD doesn't act on it

// l22SyncClient tracks TriggerSync (succeeds), GetAppStatus (returns
// not-synced/not-healthy to simulate stalled controller),
// ForceReconcile + TerminateOperation invocations.
type l22SyncClient struct {
	triggerCount   int
	statusCount    int
	forceCount     int
	terminateCount int
}

func (c *l22SyncClient) TriggerSync(_ context.Context, _ string) error {
	c.triggerCount++
	return nil
}
func (c *l22SyncClient) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	return context.DeadlineExceeded
}
func (c *l22SyncClient) GetAppStatus(_ context.Context, _ string, _ []string) (bool, bool, error) {
	c.statusCount++
	return false, false, nil // stalled — never synced, never healthy
}
func (c *l22SyncClient) ForceReconcile(_ context.Context, _ string) error {
	c.forceCount++
	return nil
}
func (c *l22SyncClient) TerminateOperation(_ context.Context, _ string) error {
	c.terminateCount++
	return nil
}

func TestL22_ForceReconcileFiresAfterMaxRetriggersExhausted(t *testing.T) {
	// Arrange: SyncClient where TriggerSync always succeeds but ArgoCD
	// never advances the app state (simulates L22 controller queue-stuck).
	client := &l22SyncClient{}
	rs := newFakeConvergenceReleaseStore()

	// Release created >2min ago so the L22 elapsed > 2min gate fires.
	oldCreated := time.Now().Add(-2*time.Minute - 30*time.Second) // > 2min L22 gate, < 3min sync_timeout default
	rs.pending = []db.Release{
		{
			ID: "rel-l22-1", BuildID: "build-1", TenantID: "tn-1",
			ProjectID: "proj-1", Environment: "production",
			Status:    string(statemachine.ReleasePending),
			CreatedAt: oldCreated,
			Apps:      json.RawMessage(`[{"name":"api","rendering":"container"}]`),
		},
	}

	loop := NewReleaseConvergenceLoop(
		rs, newFakeConvergenceEnvTargetStore(), client, &fakeConvergenceLocker{},
		&fakeConvergenceEventStore{},
		"dev.loopzero.example", slog.Default(),
		ReleaseConvergenceLoopConfig{},
	)

	// Act: exhaust maxSyncRetriggers (3 default) by calling retriggerSyncIfNeeded
	// directly. Each call increments the per-release counter.
	release := &rs.pending[0]
	for i := 0; i < maxSyncRetriggers; i++ {
		loop.retriggerSyncIfNeeded(context.Background(), release, "tn-1-production")
	}

	// Sanity: at this point ForceReconcile has NOT yet fired (it's gated
	// behind a separate check in the main loop body, not in retriggerSyncIfNeeded).
	if client.forceCount != 0 {
		t.Errorf("ForceReconcile must not fire from retriggerSyncIfNeeded alone; got %d", client.forceCount)
	}

	// Act: run a full processPendingRelease tick. The main loop body
	// detects "not synced + elapsed > 2min + retriggers exhausted" → L22.
	loop.verifyRelease(context.Background(), release)

	// Assert: ForceReconcile fires exactly once.
	if client.forceCount != 1 {
		t.Errorf("after exhausting retriggers + 2min elapsed, expected 1 ForceReconcile (L22 recovery); got %d", client.forceCount)
	}
}

func TestL22_ForceReconcileDoesNotFireBeforeRetriggersExhausted(t *testing.T) {
	// Arrange: Release just started — retriggers not yet exhausted.
	client := &l22SyncClient{}
	rs := newFakeConvergenceReleaseStore()
	oldCreated := time.Now().Add(-2*time.Minute - 30*time.Second) // > 2min L22 gate, < 3min sync_timeout default
	rs.pending = []db.Release{
		{
			ID: "rel-l22-2", BuildID: "build-2", TenantID: "tn-1",
			ProjectID: "proj-1", Environment: "production",
			Status:    string(statemachine.ReleasePending),
			CreatedAt: oldCreated,
			Apps:      json.RawMessage(`[{"name":"api","rendering":"container"}]`),
		},
	}

	loop := NewReleaseConvergenceLoop(
		rs, newFakeConvergenceEnvTargetStore(), client, &fakeConvergenceLocker{},
		&fakeConvergenceEventStore{},
		"dev.loopzero.example", slog.Default(),
		ReleaseConvergenceLoopConfig{},
	)

	release := &rs.pending[0]
	// Only 1 retrigger — counter == 1, < maxSyncRetriggers (3).
	loop.retriggerSyncIfNeeded(context.Background(), release, "tn-1-production")

	// Run processPendingRelease. L22 escalation should NOT fire because
	// retriggers are not yet exhausted (counter < maxSyncRetriggers).
	loop.verifyRelease(context.Background(), release)

	if client.forceCount != 0 {
		t.Errorf("L22 escalation MUST wait for maxSyncRetriggers exhaustion; got %d ForceReconcile calls", client.forceCount)
	}
}
