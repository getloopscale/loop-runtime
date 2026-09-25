// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/db"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/statemachine"
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
func (f *fakeConvergenceEnvTargetStore) Get(_ context.Context, _, _, _ string) (*store.EnvironmentTarget, error) {
	return nil, nil
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

// Sprint B T1.1 (ADR D1): stub methods to satisfy the widened SyncClient
// interface. fakeConvergenceSyncClient is used by tests that exercise the
// happy path / status-poll behavior; these recovery methods are noop here
// because dedicated tests (l21SyncClient, TestL21*, TestL22*) cover them.
func (f *fakeConvergenceSyncClient) ForceReconcile(_ context.Context, _ string) error {
	return nil
}
func (f *fakeConvergenceSyncClient) GetSyncRevision(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (f *fakeConvergenceSyncClient) TerminateOperation(_ context.Context, _ string) error {
	return nil
}

type fakeConvergenceEventStore struct {
	mu     sync.Mutex
	events []struct {
		BuildID, EventType string
		Data               json.RawMessage
	}
	nextID int64
}

func (f *fakeConvergenceEventStore) Append(_ context.Context, buildID, eventType string, data json.RawMessage) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.events = append(f.events, struct {
		BuildID, EventType string
		Data               json.RawMessage
	}{buildID, eventType, data})
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

// eventData returns the JSON payload of the first event matching eventType,
// or nil if no such event was appended. Used to assert the failure reason
// carried in the SSE payload (OLM-style assert-phase-AND-reason rigor).
func (f *fakeConvergenceEventStore) eventData(eventType string) json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.EventType == eventType {
			return e.Data
		}
	}
	return nil
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
		"",
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
		"dev.loopzero.example", "", slog.Default(),
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
		"dev.loopzero.example", "", slog.Default(),
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
		"dev.loopzero.example", "", slog.Default(),
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
		"",
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
		"dev.loopzero.example", "", slog.Default(),
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
	triggerCount   int
	terminateCount int
	triggerErr     error
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

// Sprint B T1.1 (ADR D1): stub methods to satisfy the widened SyncClient
// interface. The L21 recovery path tested here does NOT exercise
// ForceReconcile (that's the L22 path) or GetSyncRevision (revision
// mismatch path); noop is correct.
func (c *l21SyncClient) ForceReconcile(_ context.Context, _ string) error {
	return nil
}
func (c *l21SyncClient) GetSyncRevision(_ context.Context, _ string) (string, error) {
	return "", nil
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
		"dev.loopzero.example", "", slog.Default(),
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
		"dev.loopzero.example", "", slog.Default(),
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

// Sprint B T1.1 (ADR D1): stub method to satisfy the widened SyncClient
// interface. L22 controller queue-stuck tests don't assert on
// GetSyncRevision (the revision-mismatch path is exercised by a separate
// test); returning empty signals "no revision known yet", which keeps the
// revision-check branch inert in the L22 scenario.
func (c *l22SyncClient) GetSyncRevision(_ context.Context, _ string) (string, error) {
	return "", nil
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
		"dev.loopzero.example", "", slog.Default(),
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
		"dev.loopzero.example", "", slog.Default(),
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

// --- c13: valid-but-destructive config fail-loud (plan c13-valid-destructive-config-chaos T1.1) ---
//
// A schema-valid config whose workload SYNCS but never becomes HEALTHY (e.g. a
// readiness path that always 404s) is the "valid-but-destructive config" class.
// LoopZero's ACTIVE transition is HEALTH-GATED (release_convergence_loop.go:192,118),
// so such a release stays PENDING and is failed loud at the SyncTimeout
// (release_convergence_loop.go:81,240). This test locks that fail-loud boundary
// with TWO assertions — mirroring OLM's assert-phase-AND-reason rigor
// (knowledge-base/references/operator-lifecycle-manager/.../operator_test.go:1299):
//
//	Assertion 1 — backdated past timeout → SYNC_FAILED + the specific health reason.
//	Assertion 2 — recent (not timed-out) synced+unhealthy → stays PENDING (neither
//	              failed nor activated), proving the health-gate, which is what makes
//	              this test non-redundant vs the generic timeout test
//	              (TestReleaseConvergenceLoop_PendingRelease_TimedOut_FailsRelease).
func TestReleaseConvergenceLoop_SyncedButUnhealthy_TimedOut_FailsLoud(t *testing.T) {
	cfg := ReleaseConvergenceLoopConfig{Interval: time.Second, SyncTimeout: 3 * time.Minute, MaxConcurrent: 1}

	// --- Assertion 1: backdated past timeout → fail loud ---
	rs := newFakeConvergenceReleaseStore()
	vdRelease := db.Release{
		ID: "rel-vd", BuildID: "build-vd", TenantID: "tn-1",
		ProjectID: "proj-1", Environment: "production",
		Status:    string(statemachine.ReleasePending),
		CreatedAt: time.Now().Add(-10 * time.Minute), // way past SyncTimeout
		Apps:      json.RawMessage(`[{"name":"api","rendering":"container"}]`),
	}
	rs.pending = []db.Release{vdRelease}
	rs.releases["rel-vd"] = &vdRelease

	es := &fakeConvergenceEventStore{}
	// valid-but-destructive: ArgoCD reports synced=true but the workload is unhealthy.
	syncClient := &fakeConvergenceSyncClient{synced: true, healthy: false}

	loop := NewReleaseConvergenceLoop(
		rs, newFakeConvergenceEnvTargetStore(), syncClient, &fakeConvergenceLocker{}, es,
		"dev.loopzero.example", "", slog.Default(), cfg,
	)

	// verifyRelease runs the per-release path synchronously (no goroutine), so no poll needed.
	loop.verifyRelease(context.Background(), &rs.pending[0])

	if got := rs.getStatus("rel-vd"); got != string(statemachine.ReleaseSyncFailed_) {
		t.Fatalf("Assertion 1: expected SYNC_FAILED, got %q", got)
	}
	if !es.hasEvent("deploy.sync_failed") {
		t.Error("Assertion 1: expected deploy.sync_failed SSE event")
	}
	// OLM-style assert-phase-AND-reason: the SSE payload must carry the health reason.
	var payload struct {
		Reason string `json:"reason"`
	}
	if data := es.eventData("deploy.sync_failed"); data != nil {
		_ = json.Unmarshal(data, &payload)
	}
	if !strings.Contains(payload.Reason, "did not become healthy") {
		t.Errorf("Assertion 1: expected reason to contain %q, got %q", "did not become healthy", payload.Reason)
	}

	// --- Assertion 2 (EC-1): recent synced+unhealthy is HEALTH-GATED, not instant-failed nor activated ---
	rs2 := newFakeConvergenceReleaseStore()
	recent := db.Release{
		ID: "rel-recent", BuildID: "build-recent", TenantID: "tn-1",
		ProjectID: "proj-1", Environment: "production",
		Status:    string(statemachine.ReleasePending),
		CreatedAt: time.Now(), // NOT past timeout
		Apps:      json.RawMessage(`[{"name":"api","rendering":"container"}]`),
	}
	rs2.pending = []db.Release{recent}
	rs2.releases["rel-recent"] = &recent

	loop2 := NewReleaseConvergenceLoop(
		rs2, newFakeConvergenceEnvTargetStore(),
		&fakeConvergenceSyncClient{synced: true, healthy: false}, &fakeConvergenceLocker{},
		&fakeConvergenceEventStore{}, "dev.loopzero.example", "", slog.Default(), cfg,
	)
	loop2.verifyRelease(context.Background(), &rs2.pending[0])

	// Health-gated: an unhealthy release neither activates nor instantly fails — it waits.
	if got := rs2.getStatus("rel-recent"); got != "" {
		t.Errorf("Assertion 2: recent synced+unhealthy release must stay PENDING (health-gated); got status transition %q", got)
	}
}

// BenchmarkReleaseConvergenceLoop_FailLoudTick measures the single verifyRelease
// tick latency to fail a timed-out release. Deterministic — CreatedAt is backdated
// past the SyncTimeout so the timeout branch fires in one tick with NO real sleep
// (plan ADR D2). The per-iteration store rebuild is excluded from the timer (EC-2)
// via Stop/StartTimer. A DISCARD logger isolates the fail-loud PROCESSING cost from
// synchronous slog I/O (review F1) — without it the ns/op would be dominated by log
// writes, not the state transition. The enforced budget is the configured
// SyncTimeout (180s); the reported ns/op is the processing cost the loop incurs to
// detect + fail a timed-out release.
func BenchmarkReleaseConvergenceLoop_FailLoudTick(b *testing.B) {
	cfg := ReleaseConvergenceLoopConfig{Interval: time.Second, SyncTimeout: 180 * time.Second, MaxConcurrent: 1}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil)) // F1: isolate processing cost from log I/O
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		rs := newFakeConvergenceReleaseStore()
		rel := db.Release{
			ID: "rel-bench", BuildID: "build-bench", TenantID: "tn-1",
			ProjectID: "proj-1", Environment: "production",
			Status:    string(statemachine.ReleasePending),
			CreatedAt: time.Now().Add(-181 * time.Second), // past the 180s budget
			Apps:      json.RawMessage(`[{"name":"api","rendering":"container"}]`),
		}
		rs.pending = []db.Release{rel}
		rs.releases["rel-bench"] = &rel
		loop := NewReleaseConvergenceLoop(
			rs, newFakeConvergenceEnvTargetStore(),
			&fakeConvergenceSyncClient{synced: true, healthy: false}, &fakeConvergenceLocker{},
			&fakeConvergenceEventStore{}, "dev.loopzero.example", "", quiet, cfg,
		)
		target := &rs.pending[0]
		b.StartTimer()
		loop.verifyRelease(context.Background(), target)
	}
}
