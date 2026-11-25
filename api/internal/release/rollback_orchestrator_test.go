// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/pkg/statemachine"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// --- Test doubles for RollbackOrchestrator ---

type fakeRollbackReleaseStore struct {
	mu            sync.Mutex
	releases      map[string]*db.Release
	activeByEnv   map[string]*db.Release // key: tenant-project-env
	previousByEnv map[string]*db.Release
	lastStatus    map[string]string
	sideEffects   map[string]map[string]any
}

// status reads a release's status under the lock the background completion
// writes it with.
func (f *fakeRollbackReleaseStore) status(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.releases[id]; ok {
		return r.Status
	}
	return ""
}

func (f *fakeRollbackReleaseStore) AppendSideEffect(_ context.Context, _ db.DBTX, id string, patch map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sideEffects[id] == nil {
		f.sideEffects[id] = map[string]any{}
	}
	for k, v := range patch {
		f.sideEffects[id][k] = v
	}
	return nil
}

func newFakeRollbackReleaseStore() *fakeRollbackReleaseStore {
	return &fakeRollbackReleaseStore{
		releases:      make(map[string]*db.Release),
		activeByEnv:   make(map[string]*db.Release),
		previousByEnv: make(map[string]*db.Release),
		lastStatus:    make(map[string]string),
		sideEffects:   make(map[string]map[string]any),
	}
}

func (f *fakeRollbackReleaseStore) GetActiveByEnvTx(_ context.Context, _ db.DBTX, tenant, project, env string) (*db.Release, error) {
	return f.activeByEnv[tenant+"-"+project+"-"+env], nil
}
func (f *fakeRollbackReleaseStore) GetPreviousRelease(_ context.Context, tenant, project, env string) (*db.Release, error) {
	return f.previousByEnv[tenant+"-"+project+"-"+env], nil
}
func (f *fakeRollbackReleaseStore) GetByIDForTenant(_ context.Context, tenant, id string) (*db.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.releases[id]
	if !ok {
		return nil, nil
	}
	snapshot := *r
	return &snapshot, nil
}
func (f *fakeRollbackReleaseStore) UpdateStatus(_ context.Context, _ db.DBTX, id, event string) (*db.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastStatus[id] = event
	if r, ok := f.releases[id]; ok {
		// Simulate state transitions for re-fetch checks
		switch statemachine.Event(event) {
		case statemachine.ReleaseRollbackStarted:
			r.Status = string(statemachine.ReleaseRollingBack)
		case statemachine.ReleaseRollbackCompleted:
			r.Status = string(statemachine.ReleaseRolledBack)
		case statemachine.ReleaseReactivated:
			r.Status = string(statemachine.ReleaseActive)
		case statemachine.ReleaseRollbackFailed_:
			r.Status = "ROLLBACK_FAILED"
		case statemachine.ReleaseManualResolve:
			r.Status = string(statemachine.ReleaseActive)
		}
		return r, nil
	}
	return nil, nil
}

type fakeRollbackEnvTargetStore struct {
	desired map[string]string
	active  map[string]string
}

func newFakeRollbackEnvTargetStore() *fakeRollbackEnvTargetStore {
	return &fakeRollbackEnvTargetStore{
		desired: make(map[string]string),
		active:  make(map[string]string),
	}
}

func (f *fakeRollbackEnvTargetStore) SetDesired(_ context.Context, _ db.DBTX, tenant, project, env, releaseID, _, _ string) error {
	f.desired[tenant+"-"+project+"-"+env] = releaseID
	return nil
}
func (f *fakeRollbackEnvTargetStore) SetActive(_ context.Context, _ db.DBTX, tenant, project, env, releaseID string) error {
	f.active[tenant+"-"+project+"-"+env] = releaseID
	return nil
}

type fakeRollbackLocker struct{}

func (f *fakeRollbackLocker) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

// --- Tests ---

func TestRollback_HappyPath(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{
		ID: "rel-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive),
		Apps: json.RawMessage(`[]`),
	}
	targetRelease := &db.Release{
		ID: "rel-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded),
		GitCommit: "abc123", Apps: json.RawMessage(`[{"name":"web","type":"server"}]`),
	}

	rs.releases["rel-2"] = activeRelease
	rs.releases["rel-1"] = targetRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	rs.previousByEnv["tn-1-proj-1-production"] = targetRelease

	ets := newFakeRollbackEnvTargetStore()
	delivery := &fakeDeliveryActivator{}

	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: ets,
		Locker:     &fakeRollbackLocker{},
		Delivery:   delivery,
		Logger:     slog.Default(),
	})

	result, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		ProjectName: "my-proj",
		Environment: "production",
		UserID:      "user-1",
		BaseDomain:  "dev.getloopscale.com",
	})
	orch.Wait()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReactivatedRelease.ID != "rel-1" {
		t.Errorf("expected reactivated rel-1, got %s", result.ReactivatedRelease.ID)
	}
	if result.RolledBackRelease.ID != "rel-2" {
		t.Errorf("expected rolled back rel-2, got %s", result.RolledBackRelease.ID)
	}
	if delivery.lastReq.TargetRelease == nil || delivery.lastReq.TargetRelease.GitCommit != "abc123" {
		t.Errorf("expected delivery to re-deliver the target's commit abc123, got %+v", delivery.lastReq.TargetRelease)
	}
	if ets.desired["tn-1-proj-1-production"] != "rel-1" {
		t.Error("expected desired target set to target release")
	}
	if ets.active["tn-1-proj-1-production"] != "rel-1" {
		t.Error("expected active target set to target release")
	}
	if result.URL == "" {
		t.Error("expected URL to be set")
	}

	// Verify state transitions
	if rs.lastStatus["rel-2"] != string(statemachine.ReleaseRollbackCompleted) {
		t.Errorf("current release should transition to ROLLED_BACK via %s, got %s",
			statemachine.ReleaseRollbackCompleted, rs.lastStatus["rel-2"])
	}
	if rs.lastStatus["rel-1"] != string(statemachine.ReleaseReactivated) {
		t.Errorf("target release should transition via %s, got %s",
			statemachine.ReleaseReactivated, rs.lastStatus["rel-1"])
	}
}

func TestRollback_NoActiveRelease(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	// No active release set

	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker:     &fakeRollbackLocker{},
		Delivery:   &fakeDeliveryActivator{},
		Logger:     slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
	})
	orch.Wait()

	if err == nil {
		t.Fatal("expected error for no active release")
	}
	if err != loopzeroerrors.ErrNoActiveRelease {
		t.Errorf("expected ErrNoActiveRelease, got %v", err)
	}
}

func TestRollback_NoPreviousRelease(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{
		ID: "rel-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive),
	}
	rs.releases["rel-1"] = activeRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	// No previous release set

	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker:     &fakeRollbackLocker{},
		Delivery:   &fakeDeliveryActivator{},
		Logger:     slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
	})
	orch.Wait()

	if err == nil {
		t.Fatal("expected error for no previous release")
	}
	if err != loopzeroerrors.ErrNoPreviousRelease {
		t.Errorf("expected ErrNoPreviousRelease, got %v", err)
	}
}

func TestRollback_WithDeliveryActivator(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{
		ID: "rel-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive),
	}
	targetRelease := &db.Release{
		ID: "rel-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded),
		GitCommit: "abc123",
	}
	rs.releases["rel-2"] = activeRelease
	rs.releases["rel-1"] = targetRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	rs.previousByEnv["tn-1-proj-1-production"] = targetRelease

	delivery := &fakeDeliveryActivator{}

	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker:     &fakeRollbackLocker{},
		Delivery:   delivery,
		Logger:     slog.Default(),
	})

	result, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
	})
	orch.Wait()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !delivery.rollbackCalled {
		t.Error("expected delivery rollback to be called")
	}
	if result.ReactivatedRelease.ID != "rel-1" {
		t.Errorf("expected reactivated rel-1, got %s", result.ReactivatedRelease.ID)
	}
}

// fakeDeliveryActivator is a minimal test double for DeliveryActivator.
type fakeDeliveryActivator struct {
	rollbackCalled bool
	rollbackErr    error
	onRollback     func() // runs during delivery, to simulate a concurrent change
	lastReq        deploycontracts.RollbackDeliveryRequest
}

func (f *fakeDeliveryActivator) Activate(_ context.Context, _ deploycontracts.ActivationBuild, _ deploycontracts.BuildCallbackRequest) error {
	return nil
}
func (f *fakeDeliveryActivator) Rollback(_ context.Context, req deploycontracts.RollbackDeliveryRequest) error {
	f.rollbackCalled = true
	f.lastReq = req
	if f.onRollback != nil {
		f.onRollback()
	}
	return f.rollbackErr
}
func (f *fakeDeliveryActivator) AwaitRollback(context.Context, deploycontracts.RollbackDeliveryRequest) error {
	return nil
}
func (f *fakeDeliveryActivator) RollbackConverged(context.Context, deploycontracts.RollbackDeliveryRequest) (bool, error) {
	return true, nil
}

func TestRollback_SpecificReleaseID(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{
		ID: "rel-3", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive),
	}
	targetRelease := &db.Release{
		ID: "rel-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded),
		GitCommit: "target-sha",
	}
	rs.releases["rel-3"] = activeRelease
	rs.releases["rel-1"] = targetRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease

	delivery := &fakeDeliveryActivator{}

	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker:     &fakeRollbackLocker{},
		Delivery:   delivery,
		Logger:     slog.Default(),
	})

	result, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		ToReleaseID: "rel-1", // explicit target
	})
	orch.Wait()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReactivatedRelease.ID != "rel-1" {
		t.Errorf("expected target rel-1, got %s", result.ReactivatedRelease.ID)
	}
}

func TestRollback_SpecificReleaseNotFound(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{
		ID: "rel-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive),
	}
	rs.releases["rel-2"] = activeRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	// rel-99 does NOT exist

	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker:     &fakeRollbackLocker{},
		Delivery:   &fakeDeliveryActivator{},
		Logger:     slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		ToReleaseID: "rel-99",
	})
	orch.Wait()

	if err == nil {
		t.Fatal("expected error for non-existent target release")
	}
	if err != loopzeroerrors.ErrReleaseNotFound {
		t.Errorf("expected ErrReleaseNotFound, got %v", err)
	}
}

func TestRollback_URLGeneration(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{
		ID: "rel-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "staging", Status: string(statemachine.ReleaseActive),
	}
	targetRelease := &db.Release{
		ID: "rel-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "staging", Status: string(statemachine.ReleaseSuperseded),
		GitCommit: "sha123",
		Apps:      json.RawMessage(`[{"name":"worker","type":"worker"},{"name":"web","type":"server"}]`),
	}
	rs.releases["rel-2"] = activeRelease
	rs.releases["rel-1"] = targetRelease
	rs.activeByEnv["tn-1-proj-1-staging"] = activeRelease
	rs.previousByEnv["tn-1-proj-1-staging"] = targetRelease

	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker:     &fakeRollbackLocker{},
		Delivery:   &fakeDeliveryActivator{},
		Logger:     slog.Default(),
	})

	result, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		ProjectName: "my-app",
		Environment: "staging",
		BaseDomain:  "dev.getloopscale.com",
	})
	orch.Wait()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The canonical host the IngressRoute serves (naming.AppURL), not the legacy
	// "{project}-{env}.{domain}" that Traefik answered with 404 (loopzero-cloud#159).
	expected := "https://tn-1-proj-1-web-staging.dev.getloopscale.com"
	if result.URL != expected {
		t.Errorf("URL = %q, want %q", result.URL, expected)
	}
}

func TestRollback_NoBaseDomain_EmptyURL(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{
		ID: "rel-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive),
	}
	targetRelease := &db.Release{
		ID: "rel-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded),
		GitCommit: "sha",
	}
	rs.releases["rel-2"] = activeRelease
	rs.releases["rel-1"] = targetRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	rs.previousByEnv["tn-1-proj-1-production"] = targetRelease

	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker:     &fakeRollbackLocker{},
		Delivery:   &fakeDeliveryActivator{},
		Logger:     slog.Default(),
	})

	result, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		BaseDomain:  "", // no base domain
	})
	orch.Wait()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.URL != "" {
		t.Errorf("URL should be empty when no base domain, got %q", result.URL)
	}
}

// TestRollback_FlipsReleaseStateOnlyAfterDeliveryReturns pins the DB safety
// invariant (LIVE sponsor mandate 2026-06-18): the current release is marked
// ROLLED_BACK only after delivery — which waits for the sync, see
// TestContainerDelivery_Rollback_WaitsForSyncConfirmation — has returned.
func TestRollback_FlipsReleaseStateOnlyAfterDeliveryReturns(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{ID: "rel-active", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive), GitCommit: "sha-active"}
	targetRelease := &db.Release{ID: "rel-target", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded), GitCommit: "sha-target"}
	rs.releases["rel-active"] = activeRelease
	rs.releases["rel-target"] = targetRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	rs.previousByEnv["tn-1-proj-1-production"] = targetRelease
	var eventDuringDelivery string
	delivery := &fakeDeliveryActivator{onRollback: func() { eventDuringDelivery = rs.lastStatus["rel-active"] }}
	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases: rs, EnvTargets: newFakeRollbackEnvTargetStore(), Locker: &fakeRollbackLocker{},
		Delivery: delivery, Logger: slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
	})
	orch.Wait()

	if err != nil {
		t.Fatalf("rollback err: %v", err)
	}
	if eventDuringDelivery != string(statemachine.ReleaseRollbackStarted) {
		t.Errorf("during delivery the release must be ROLLING_BACK, last event was %q", eventDuringDelivery)
	}
	if rs.lastStatus["rel-active"] != string(statemachine.ReleaseRollbackCompleted) {
		t.Errorf("after delivery the release must be ROLLED_BACK, last event was %q", rs.lastStatus["rel-active"])
	}
}

func TestRollback_WithoutDeliveryRefusesBeforeChangingState(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{ID: "rel-active", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive)}
	rs.releases["rel-active"] = activeRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases: rs, EnvTargets: newFakeRollbackEnvTargetStore(), Locker: &fakeRollbackLocker{},
		Logger: slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
	})
	orch.Wait()

	if !errors.Is(err, errRollbackDeliveryNotWired) {
		t.Fatalf("want errRollbackDeliveryNotWired, got %v", err)
	}
	if _, touched := rs.lastStatus["rel-active"]; touched {
		t.Error("an unwired orchestrator must refuse before transitioning any release")
	}
}

// The completion runs after the request was answered, so a release that is
// gone by then cannot be reported to the caller. It must not activate the
// target on top of a state it no longer understands.
func TestRollback_CurrentReleaseVanishesBeforeFinalize_LeavesTheTargetAlone(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{ID: "rel-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive)}
	targetRelease := &db.Release{ID: "rel-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded), GitCommit: "abc123"}
	rs.releases["rel-2"] = activeRelease
	rs.releases["rel-1"] = targetRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	rs.previousByEnv["tn-1-proj-1-production"] = targetRelease
	delivery := &fakeDeliveryActivator{onRollback: func() {
		rs.mu.Lock()
		defer rs.mu.Unlock()
		delete(rs.releases, "rel-2")
	}}
	ets := newFakeRollbackEnvTargetStore()
	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases: rs, EnvTargets: ets,
		Locker: &fakeRollbackLocker{}, Delivery: delivery, Logger: slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
	})
	orch.Wait()

	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := rs.status("rel-1"); got != string(statemachine.ReleaseSuperseded) {
		t.Errorf("the target is %s, want it left SUPERSEDED", got)
	}
	if len(ets.active) != 0 {
		t.Errorf("the environment target moved to %v", ets.active)
	}
}

func TestRollback_URLPrefersThePublicDomain(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{ID: "rel-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive)}
	targetRelease := &db.Release{ID: "rel-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded), GitCommit: "sha",
		Apps: json.RawMessage(`[{"name":"web","type":"server"}]`)}
	rs.releases["rel-2"] = activeRelease
	rs.releases["rel-1"] = targetRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	rs.previousByEnv["tn-1-proj-1-production"] = targetRelease
	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases: rs, EnvTargets: newFakeRollbackEnvTargetStore(), Locker: &fakeRollbackLocker{},
		Delivery: &fakeDeliveryActivator{}, PublicDomain: "apps.example.com", Logger: slog.Default(),
	})

	result, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID: "tn-1", ProjectID: "proj-1", ProjectName: "my-app",
		Environment: "production", BaseDomain: "loopzero.local",
	})
	orch.Wait()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "https://tn-1-proj-1-web-production.apps.example.com"; result.URL != want {
		t.Errorf("URL = %q, want %q", result.URL, want)
	}
}
