// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/services/release"

	"github.com/getloopscale/loopzero/api/internal/services/shared"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// --- Fakes for RollbackOrchestrator ---

type fakeRollbackReleaseStore struct {
	active   *db.Release
	previous *db.Release
	byID     map[string]*db.Release
	statuses map[string]string
}

func (f *fakeRollbackReleaseStore) GetActiveByEnvTx(_ context.Context, _ db.DBTX, _, _, _ string) (*db.Release, error) {
	if f.active == nil {
		return nil, fmt.Errorf("no active release")
	}
	return f.active, nil
}

func (f *fakeRollbackReleaseStore) GetPreviousRelease(_ context.Context, _, _, _ string) (*db.Release, error) {
	if f.previous == nil {
		return nil, fmt.Errorf("no previous release")
	}
	return f.previous, nil
}

func (f *fakeRollbackReleaseStore) GetByIDForTenant(_ context.Context, _, releaseID string) (*db.Release, error) {
	r, ok := f.byID[releaseID]
	if !ok {
		return nil, fmt.Errorf("release not found")
	}
	// Return a copy with the latest status from statuses map (updated by UpdateStatus)
	if status, ok := f.statuses[releaseID]; ok {
		copy := *r
		copy.Status = status
		return &copy, nil
	}
	return r, nil
}

func (f *fakeRollbackReleaseStore) GetByIDTx(_ context.Context, _ db.DBTX, id string) (*db.Release, error) {
	r, ok := f.byID[id]
	if !ok {
		return nil, fmt.Errorf("release not found")
	}
	if status, ok := f.statuses[id]; ok {
		copy := *r
		copy.Status = status
		return &copy, nil
	}
	return r, nil
}

func (f *fakeRollbackReleaseStore) UpdateStatus(_ context.Context, _ db.DBTX, id string, event string) (*db.Release, error) {
	m := statemachine.NewReleaseMachine()
	current := f.statuses[id]
	newState, err := safeTransitionRelease(m, statemachine.State(current), statemachine.Event(event))
	if err != nil {
		return nil, err
	}
	f.statuses[id] = string(newState)
	return &db.Release{ID: id, Status: string(newState)}, nil
}

type fakeRollbackEnvTargetStore struct{}

func (f *fakeRollbackEnvTargetStore) SetDesired(_ context.Context, _ db.DBTX, _, _, _, _, _, _ string) error {
	return nil
}
func (f *fakeRollbackEnvTargetStore) SetActive(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	return nil
}

type fakeRollbackGitReader struct{}

func (f *fakeRollbackGitReader) GetManifestsAtCommit(_ context.Context, _, _, _, _ string) ([]byte, error) {
	return []byte("apiVersion: v1\nkind: Deployment"), nil
}

type fakeRollbackGitPusher struct{}

func (f *fakeRollbackGitPusher) Push(_ context.Context, _ shared.GitOpsPushOpts) (string, error) {
	return "sha-abc123", nil
}

type fakeRollbackSyncClient struct {
	triggerErr error
}

func (f *fakeRollbackSyncClient) TriggerSync(_ context.Context, _ string) error {
	return f.triggerErr
}

// WaitForSync satisfies the RollbackSyncClient interface (added when the
// interface was extended; this fake never blocks on sync confirmation in
// these tests — the rollback orchestrator paths under test stop at
// TriggerSync). Kept as a no-op so the test file compiles.
func (f *fakeRollbackSyncClient) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	return nil
}

// --- Tests ---

func TestRollbackOrchestrator_SyncFailureReturnsError(t *testing.T) {
	// Arrange: rollback with ArgoCD sync failure (MEET-2026-03-27-012 Phase 1.4)
	activeRel := &db.Release{
		ID: "rel-active", TenantID: "acme", ProjectID: "my-app",
		Environment: "production", Status: "ACTIVE",
	}
	releaseStore := &fakeRollbackReleaseStore{
		active: activeRel,
		previous: &db.Release{
			ID: "rel-target", TenantID: "acme", ProjectID: "my-app",
			Environment: "production", Status: "SUPERSEDED", GitCommit: "abc123",
		},
		byID: map[string]*db.Release{
			"rel-active": activeRel,
		},
		statuses: map[string]string{
			"rel-active": "ACTIVE",
			"rel-target": "SUPERSEDED",
		},
	}

	syncClient := &fakeRollbackSyncClient{triggerErr: errors.New("argocd: connection refused")}
	locker := &fakeDeleteLocker{} // reuse from project_deleter_test.go

	orch := release.NewRollbackOrchestrator(release.RollbackOrchestratorDeps{
		Releases:   releaseStore,
		EnvTargets: &fakeRollbackEnvTargetStore{},
		GitReader:  &fakeRollbackGitReader{},
		GitPusher:  &fakeRollbackGitPusher{},
		Sync:       syncClient,
		Locker:     locker,
		Logger:     testLogger,
	})

	req := release.RollbackRequest{
		TenantID:      "acme",
		ProjectID:     "my-app",
		ProjectName:   "my-saas",
		Environment:   "production",
		CorrelationID: "corr-rb-1",
		UserID:        "user-1",
		BaseDomain:    "loopzero.local",
	}

	// Act
	_, err := orch.Rollback(context.Background(), req)

	// Assert: rollback MUST return error when sync fails (not swallow it)
	if err == nil {
		t.Fatal("expected rollback to return error when ArgoCD sync fails, got nil")
	}
	// The active release should be in ROLLBACK_FAILED state
	if releaseStore.statuses["rel-active"] != "ROLLBACK_FAILED" {
		t.Errorf("expected active release in ROLLBACK_FAILED, got %q", releaseStore.statuses["rel-active"])
	}
}

func TestRollbackOrchestrator_SyncSuccessCompletes(t *testing.T) {
	// Arrange: rollback with successful sync
	activeRel := &db.Release{
		ID: "rel-active", TenantID: "acme", ProjectID: "my-app",
		Environment: "production", Status: "ACTIVE",
	}
	releaseStore := &fakeRollbackReleaseStore{
		active: activeRel,
		previous: &db.Release{
			ID: "rel-target", TenantID: "acme", ProjectID: "my-app",
			Environment: "production", Status: "SUPERSEDED", GitCommit: "abc123",
		},
		byID: map[string]*db.Release{
			"rel-active": activeRel,
		},
		statuses: map[string]string{
			"rel-active": "ACTIVE",
			"rel-target": "SUPERSEDED",
		},
	}

	syncClient := &fakeRollbackSyncClient{triggerErr: nil} // sync succeeds
	locker := &fakeDeleteLocker{}

	orch := release.NewRollbackOrchestrator(release.RollbackOrchestratorDeps{
		Releases:   releaseStore,
		EnvTargets: &fakeRollbackEnvTargetStore{},
		GitReader:  &fakeRollbackGitReader{},
		GitPusher:  &fakeRollbackGitPusher{},
		Sync:       syncClient,
		Locker:     locker,
		Logger:     testLogger,
	})

	req := release.RollbackRequest{
		TenantID:      "acme",
		ProjectID:     "my-app",
		ProjectName:   "my-saas",
		Environment:   "production",
		CorrelationID: "corr-rb-2",
		UserID:        "user-1",
		BaseDomain:    "loopzero.local",
	}

	// Act
	result, err := orch.Rollback(context.Background(), req)

	// Assert: rollback succeeds
	if err != nil {
		t.Fatalf("expected rollback to succeed, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Active release should be ROLLED_BACK
	if releaseStore.statuses["rel-active"] != "ROLLED_BACK" {
		t.Errorf("expected active release ROLLED_BACK, got %q", releaseStore.statuses["rel-active"])
	}
}
