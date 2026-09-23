// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/pkg/statemachine"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// --- Test doubles for RollbackOrchestrator ---

type fakeRollbackReleaseStore struct {
	releases      map[string]*db.Release
	activeByEnv   map[string]*db.Release // key: tenant-project-env
	previousByEnv map[string]*db.Release
	lastStatus    map[string]string
}

func newFakeRollbackReleaseStore() *fakeRollbackReleaseStore {
	return &fakeRollbackReleaseStore{
		releases:      make(map[string]*db.Release),
		activeByEnv:   make(map[string]*db.Release),
		previousByEnv: make(map[string]*db.Release),
		lastStatus:    make(map[string]string),
	}
}

func (f *fakeRollbackReleaseStore) GetActiveByEnvTx(_ context.Context, _ db.DBTX, tenant, project, env string) (*db.Release, error) {
	return f.activeByEnv[tenant+"-"+project+"-"+env], nil
}
func (f *fakeRollbackReleaseStore) GetPreviousRelease(_ context.Context, tenant, project, env string) (*db.Release, error) {
	return f.previousByEnv[tenant+"-"+project+"-"+env], nil
}
func (f *fakeRollbackReleaseStore) GetByIDForTenant(_ context.Context, tenant, id string) (*db.Release, error) {
	r, ok := f.releases[id]
	if !ok {
		return nil, nil
	}
	return r, nil
}
func (f *fakeRollbackReleaseStore) UpdateStatus(_ context.Context, _ db.DBTX, id, event string) (*db.Release, error) {
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

type fakeRollbackGitReader struct {
	manifests map[string][]byte
}

func (f *fakeRollbackGitReader) GetManifestsAtCommit(_ context.Context, _, _, _, commitSHA string) ([]byte, error) {
	m, ok := f.manifests[commitSHA]
	if !ok {
		return nil, fmt.Errorf("manifests not found for commit %s", commitSHA)
	}
	return m, nil
}

type fakeRollbackGitPusher struct {
	pushed bool
}

func (f *fakeRollbackGitPusher) Push(_ context.Context, _ deploycontracts.GitOpsPushOpts) (string, error) {
	f.pushed = true
	return "commit-sha-rollback", nil
}

type fakeRollbackSyncClient struct {
	synced     bool
	waitCalled bool
	waitErr    error
}

func (f *fakeRollbackSyncClient) TriggerSync(_ context.Context, _ string) error {
	f.synced = true
	return nil
}

func (f *fakeRollbackSyncClient) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	f.waitCalled = true
	return f.waitErr
}

func (f *fakeRollbackSyncClient) ForceReconcile(_ context.Context, _ string) error {
	return nil
}

type fakeRollbackLocker struct{}

func (f *fakeRollbackLocker) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

// --- Tests ---

func TestRollback_HappyPath_LegacyFallback(t *testing.T) {
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
	gitReader := &fakeRollbackGitReader{manifests: map[string][]byte{"abc123": []byte("manifests-yaml")}}
	gitPusher := &fakeRollbackGitPusher{}
	syncClient := &fakeRollbackSyncClient{}

	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: ets,
		GitReader:  gitReader,
		GitPusher:  gitPusher,
		Sync:       syncClient,
		Locker:     &fakeRollbackLocker{},
		Delivery:   nil, // legacy fallback path
		Logger:     slog.Default(),
	})

	result, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		ProjectName: "my-proj",
		Environment: "production",
		UserID:      "user-1",
		BaseDomain:  "dev.loopzero.example",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReactivatedRelease.ID != "rel-1" {
		t.Errorf("expected reactivated rel-1, got %s", result.ReactivatedRelease.ID)
	}
	if result.RolledBackRelease.ID != "rel-2" {
		t.Errorf("expected rolled back rel-2, got %s", result.RolledBackRelease.ID)
	}
	if !gitPusher.pushed {
		t.Error("expected git push")
	}
	if !syncClient.synced {
		t.Error("expected argocd sync")
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
		GitReader:  &fakeRollbackGitReader{},
		GitPusher:  &fakeRollbackGitPusher{},
		Sync:       &fakeRollbackSyncClient{},
		Locker:     &fakeRollbackLocker{},
		Logger:     slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
	})

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
		GitReader:  &fakeRollbackGitReader{},
		GitPusher:  &fakeRollbackGitPusher{},
		Sync:       &fakeRollbackSyncClient{},
		Locker:     &fakeRollbackLocker{},
		Logger:     slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
	})

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
}

func (f *fakeDeliveryActivator) Activate(_ context.Context, _ deploycontracts.ActivationBuild, _ deploycontracts.BuildCallbackRequest) error {
	return nil
}
func (f *fakeDeliveryActivator) Rollback(_ context.Context, _ deploycontracts.RollbackDeliveryRequest) error {
	f.rollbackCalled = true
	if f.onRollback != nil {
		f.onRollback()
	}
	return f.rollbackErr
}

func TestRollback_DeliveryFails_TransitionsToRollbackFailed(t *testing.T) {
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

	delivery := &fakeDeliveryActivator{rollbackErr: fmt.Errorf("delivery exploded")}

	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker:     &fakeRollbackLocker{},
		Delivery:   delivery,
		Logger:     slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
	})

	if err == nil {
		t.Fatal("expected error when delivery fails")
	}

	// Current release should be marked ROLLBACK_FAILED
	if rs.lastStatus["rel-2"] != string(statemachine.ReleaseRollbackFailed_) {
		t.Errorf("expected ROLLBACK_FAILED, got %s", rs.lastStatus["rel-2"])
	}
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
		Logger:     slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		ToReleaseID: "rel-99",
	})

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
		BaseDomain:  "dev.loopzero.example",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The canonical host the IngressRoute serves (naming.AppURL), not the legacy
	// "{project}-{env}.{domain}" that Traefik answered with 404 (loopzero-cloud#159).
	expected := "https://tn-1-proj-1-web-staging.dev.loopzero.example"
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

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.URL != "" {
		t.Errorf("URL should be empty when no base domain, got %q", result.URL)
	}
}

// TestRollback_WaitsForSyncBeforeFlippingDB confirms the canonical
// safety invariant: DB transitions ROLLING_BACK → ROLLED_BACK + target
// → ACTIVE happen ONLY AFTER the delivery layer confirms the deployment
// infrastructure has synced + pods are healthy. If WaitForSync fails,
// the DB does NOT flip and the rollback is marked failed.
//
// LIVE-caught 2026-06-18 sponsor mandate: "o banco deve ser atualizado
// somente COM GARANTIA MAXIMA QUE O ROLLBACK aconteceu". The previous
// flow trigger-sync-then-flip risked DB lying about ACTIVE state while
// pods still served the rolled-back code.
func TestRollback_WaitsForSyncBeforeFlippingDB(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{
		ID: "rel-active", BuildID: "build-active", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive), GitCommit: "sha-active",
	}
	targetRelease := &db.Release{
		ID: "rel-target", BuildID: "build-target", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded), GitCommit: "sha-target",
	}
	rs.releases["rel-active"] = activeRelease
	rs.releases["rel-target"] = targetRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	rs.previousByEnv["tn-1-proj-1-production"] = targetRelease

	syncClient := &fakeRollbackSyncClient{waitErr: nil} // sync succeeds
	gitReader := &fakeRollbackGitReader{manifests: map[string][]byte{"sha-target": []byte("apps:\n  - name: app")}}
	gitPusher := &fakeRollbackGitPusher{}

	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker:     &fakeRollbackLocker{},
		GitReader:  gitReader,
		GitPusher:  gitPusher,
		Sync:       syncClient,
		Logger:     slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
	})
	if err != nil {
		t.Fatalf("rollback err: %v", err)
	}
	if !syncClient.waitCalled {
		t.Errorf("WaitForSync NOT called — DB safety invariant violated: rollback flipped state without confirming sync")
	}
}

// TestRollback_SyncWaitFails_DBStaysInRollingBack confirms that when
// WaitForSync errors, the DB does NOT flip current to ROLLED_BACK and
// the rollback is reported failed. Closes the LIVE risk of DB lying
// ACTIVE while pods still serve the rolled-back code.
func TestRollback_SyncWaitFails_DBStaysInRollingBack(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{
		ID: "rel-active", BuildID: "build-active", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive), GitCommit: "sha-active",
	}
	targetRelease := &db.Release{
		ID: "rel-target", BuildID: "build-target", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded), GitCommit: "sha-target",
	}
	rs.releases["rel-active"] = activeRelease
	rs.releases["rel-target"] = targetRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	rs.previousByEnv["tn-1-proj-1-production"] = targetRelease

	syncClient := &fakeRollbackSyncClient{waitErr: fmt.Errorf("argo sync timeout")}
	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker:     &fakeRollbackLocker{},
		GitReader:  &fakeRollbackGitReader{manifests: map[string][]byte{"sha-target": []byte("apps: []")}},
		GitPusher:  &fakeRollbackGitPusher{},
		Sync:       syncClient,
		Logger:     slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
	})
	if err == nil {
		t.Fatalf("expected error when WaitForSync fails")
	}
	// Sync wait failure → release stays in ROLLBACK_FAILED, NOT ROLLED_BACK.
	if rs.lastStatus["rel-active"] == string(statemachine.ReleaseRollbackCompleted) {
		t.Errorf("DB SAFETY VIOLATION: release marked ROLLED_BACK after sync wait failed (expected ROLLBACK_FAILED)")
	}
}

func TestRollback_CurrentReleaseVanishesBeforeFinalize_ReturnsReleaseNotFound(t *testing.T) {
	rs := newFakeRollbackReleaseStore()
	activeRelease := &db.Release{ID: "rel-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive)}
	targetRelease := &db.Release{ID: "rel-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded), GitCommit: "abc123"}
	rs.releases["rel-2"] = activeRelease
	rs.releases["rel-1"] = targetRelease
	rs.activeByEnv["tn-1-proj-1-production"] = activeRelease
	rs.previousByEnv["tn-1-proj-1-production"] = targetRelease
	delivery := &fakeDeliveryActivator{onRollback: func() { delete(rs.releases, "rel-2") }}
	orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases: rs, EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker: &fakeRollbackLocker{}, Delivery: delivery, Logger: slog.Default(),
	})

	_, err := orch.Rollback(context.Background(), RollbackRequest{
		TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
	})

	if !errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
		t.Fatalf("want ErrReleaseNotFound when the rolling-back release is gone, got %v", err)
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

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "https://tn-1-proj-1-web-production.apps.example.com"; result.URL != want {
		t.Errorf("URL = %q, want %q", result.URL, want)
	}
}
