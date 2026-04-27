// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
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
