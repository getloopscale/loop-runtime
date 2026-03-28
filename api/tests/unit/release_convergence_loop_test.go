// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// --- Fakes for ReleaseConvergenceLoop ---

type fakeConvergenceReleaseStore struct {
	pending  []db.Release
	statuses map[string]string // releaseID → current status
	active   map[string]*db.Release // key: tenant:project:env → active release
}

func newFakeConvergenceReleaseStore() *fakeConvergenceReleaseStore {
	return &fakeConvergenceReleaseStore{
		statuses: make(map[string]string),
		active:   make(map[string]*db.Release),
	}
}

func (f *fakeConvergenceReleaseStore) FindPendingReleases(_ context.Context) ([]db.Release, error) {
	return f.pending, nil
}

func (f *fakeConvergenceReleaseStore) UpdateStatus(_ context.Context, _ db.DBTX, id string, event string) (*db.Release, error) {
	m := statemachine.NewReleaseMachine()
	current := f.statuses[id]
	if current == "" {
		current = string(statemachine.ReleasePending)
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
	activeCalls int
}

func (f *fakeConvergenceEnvTargetStore) ListByProject(_ context.Context, _, _ string) ([]db.EnvironmentTarget, error) {
	return nil, nil
}
func (f *fakeConvergenceEnvTargetStore) DeleteTarget(_ context.Context, _ db.DBTX, _, _, _ string) error {
	return nil
}
func (f *fakeConvergenceEnvTargetStore) SetDesired(_ context.Context, _ db.DBTX, _, _, _, _, _, _ string) error {
	return nil
}
func (f *fakeConvergenceEnvTargetStore) SetActive(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	f.activeCalls++
	return nil
}
func (f *fakeConvergenceEnvTargetStore) Get(_ context.Context, _, _, _ string) (*db.EnvironmentTarget, error) {
	return nil, nil
}

type fakeConvergenceSyncClient struct {
	synced  bool
	healthy bool
	err     error
}

func (f *fakeConvergenceSyncClient) TriggerSync(_ context.Context, _ string) error { return nil }
func (f *fakeConvergenceSyncClient) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	return nil
}
func (f *fakeConvergenceSyncClient) GetAppStatus(_ context.Context, _ string, _ []string) (bool, bool, error) {
	return f.synced, f.healthy, f.err
}

type fakeConvergenceEventStore struct {
	events []string
}

func (f *fakeConvergenceEventStore) Append(_ context.Context, _ string, eventType string, _ json.RawMessage) (int64, error) {
	f.events = append(f.events, eventType)
	return 1, nil
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

	loop := services.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		testLogger, services.ReleaseConvergenceLoopConfig{
			Interval: 100 * time.Millisecond, SyncTimeout: 5 * time.Minute, MaxConcurrent: 2,
		},
	)

	// Act: run one tick
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go loop.Start(ctx)
	time.Sleep(300 * time.Millisecond)

	// Assert: release should be ACTIVE
	if releaseStore.statuses["rel-001"] != "ACTIVE" {
		t.Errorf("expected release ACTIVE, got %q", releaseStore.statuses["rel-001"])
	}
	if envTargetStore.activeCalls == 0 {
		t.Error("expected SetActive to be called")
	}
	// Should emit deploy.live event
	found := false
	for _, e := range eventStore.events {
		if e == "deploy.live" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected deploy.live event, got %v", eventStore.events)
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

	loop := services.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		testLogger, services.ReleaseConvergenceLoopConfig{
			Interval: 100 * time.Millisecond, SyncTimeout: 1 * time.Second, MaxConcurrent: 2,
		},
	)

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go loop.Start(ctx)
	time.Sleep(300 * time.Millisecond)

	// Assert: release should be SYNC_FAILED
	if releaseStore.statuses["rel-002"] != "SYNC_FAILED" {
		t.Errorf("expected release SYNC_FAILED, got %q", releaseStore.statuses["rel-002"])
	}
	// Should emit deploy.sync_failed event
	found := false
	for _, e := range eventStore.events {
		if e == "deploy.sync_failed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected deploy.sync_failed event, got %v", eventStore.events)
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

	loop := services.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		testLogger, services.ReleaseConvergenceLoopConfig{
			Interval: 100 * time.Millisecond, SyncTimeout: 5 * time.Minute, MaxConcurrent: 2,
		},
	)

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	go loop.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Assert: release stays PENDING (not activated, not failed)
	if releaseStore.statuses["rel-003"] != "PENDING" {
		t.Errorf("expected release to stay PENDING, got %q", releaseStore.statuses["rel-003"])
	}
	if len(eventStore.events) != 0 {
		t.Errorf("expected no events (still waiting), got %v", eventStore.events)
	}
}

func TestReleaseConvergenceLoop_HandlesEmptyPendingList(t *testing.T) {
	// Arrange: no pending releases
	releaseStore := newFakeConvergenceReleaseStore()
	syncClient := &fakeConvergenceSyncClient{}
	eventStore := &fakeConvergenceEventStore{}
	locker := &fakeConvergenceLocker{}
	envTargetStore := &fakeConvergenceEnvTargetStore{}

	loop := services.NewReleaseConvergenceLoop(
		releaseStore, envTargetStore, syncClient, locker, eventStore,
		testLogger, services.ReleaseConvergenceLoopConfig{
			Interval: 50 * time.Millisecond, SyncTimeout: 5 * time.Minute, MaxConcurrent: 2,
		},
	)

	// Act: should not panic or error
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	go loop.Start(ctx)
	time.Sleep(150 * time.Millisecond)

	// Assert: nothing happened
	if len(eventStore.events) != 0 {
		t.Errorf("expected no events, got %v", eventStore.events)
	}
}
