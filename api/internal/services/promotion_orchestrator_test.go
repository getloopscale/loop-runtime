// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/pkg/statemachine"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// --- Test doubles for PromotionOrchestrator ---

type fakePromotionReleaseStore struct {
	releases    map[string]*db.Release
	activeByEnv map[string]*db.Release // key: tenant-project-env
	created     []*db.Release
	lastStatus  map[string]string
}

func newFakePromotionReleaseStore() *fakePromotionReleaseStore {
	return &fakePromotionReleaseStore{
		releases:    make(map[string]*db.Release),
		activeByEnv: make(map[string]*db.Release),
		created:     nil,
		lastStatus:  make(map[string]string),
	}
}

func (f *fakePromotionReleaseStore) GetActiveByEnv(_ context.Context, tenant, project, env string) (*db.Release, error) {
	return f.activeByEnv[tenant+"-"+project+"-"+env], nil
}

func (f *fakePromotionReleaseStore) Create(_ context.Context, _ db.DBTX, release *db.Release) error {
	f.created = append(f.created, release)
	f.releases[release.ID] = release
	return nil
}

func (f *fakePromotionReleaseStore) UpdateStatus(_ context.Context, _ db.DBTX, id, event string) (*db.Release, error) {
	f.lastStatus[id] = event
	if r, ok := f.releases[id]; ok {
		// V9 L17: mirror production behavior — apply the event to the
		// stored release via the canonical transitions so callers
		// observe the post-transition state. Production
		// ReleaseStoreSqlc.UpdateStatus does this via the state machine
		// and a real UPDATE; the mock must do the same for tests to
		// validate the post-condition end-to-end.
		switch event {
		case "GitMirrorCompleted":
			if r.Status == "READY" {
				r.Status = "PENDING"
			}
		case "SyncCompleted":
			if r.Status == "PENDING" {
				r.Status = "ACTIVE"
			}
		}
		return r, nil
	}
	return nil, nil
}

func (f *fakePromotionReleaseStore) SetGitCommit(_ context.Context, _ db.DBTX, id, sha string) error {
	if r, ok := f.releases[id]; ok {
		r.GitCommit = sha
	}
	return nil
}

type fakePromotionEnvTargetStore struct {
	targets map[string]*db.EnvironmentTarget
	desired map[string]string
}

func newFakePromotionEnvTargetStore() *fakePromotionEnvTargetStore {
	return &fakePromotionEnvTargetStore{
		targets: make(map[string]*db.EnvironmentTarget),
		desired: make(map[string]string),
	}
}

func (f *fakePromotionEnvTargetStore) Get(_ context.Context, tenant, project, env string) (*db.EnvironmentTarget, error) {
	return f.targets[tenant+"-"+project+"-"+env], nil
}

func (f *fakePromotionEnvTargetStore) SetDesired(_ context.Context, _ db.DBTX, tenant, project, env, releaseID, _, _ string) error {
	f.desired[tenant+"-"+project+"-"+env] = releaseID
	return nil
}

type fakePromotionGitReader struct {
	manifests map[string][]byte
}

func (f *fakePromotionGitReader) GetManifestsAtCommit(_ context.Context, _, _, _, commitSHA string) ([]byte, error) {
	m, ok := f.manifests[commitSHA]
	if !ok {
		return nil, fmt.Errorf("manifests not found for commit %s", commitSHA)
	}
	return m, nil
}

type fakePromotionGitPusher struct {
	pushed    bool
	lastOpts  GitOpsPushOpts
	commitSHA string
}

func (f *fakePromotionGitPusher) Push(_ context.Context, opts GitOpsPushOpts) (string, error) {
	f.pushed = true
	f.lastOpts = opts
	if f.commitSHA == "" {
		return "commit-sha-promote", nil
	}
	return f.commitSHA, nil
}

type fakePromotionSyncClient struct {
	synced   bool
	syncErr  error
}

func (f *fakePromotionSyncClient) TriggerSync(_ context.Context, _ string) error {
	f.synced = true
	return f.syncErr
}

type fakePromotionLocker struct{}

func (f *fakePromotionLocker) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

// --- Tests ---

func TestPromote_HappyPath(t *testing.T) {
	// Arrange
	rs := newFakePromotionReleaseStore()
	sourceRelease := &db.Release{
		ID:          "rel-src-1",
		BuildID:     "build-1",
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "staging",
		Status:      string(statemachine.ReleaseActive),
		GitCommit:   "abc123",
		Apps:        json.RawMessage(`[{"name":"api","url":"https://api.staging.dev.loopzero.example"}]`),
	}
	rs.releases["rel-src-1"] = sourceRelease
	rs.activeByEnv["tn-1-proj-1-staging"] = sourceRelease

	ets := newFakePromotionEnvTargetStore()
	gitReader := &fakePromotionGitReader{manifests: map[string][]byte{"abc123": []byte("deployment-yaml")}}
	gitPusher := &fakePromotionGitPusher{}
	syncClient := &fakePromotionSyncClient{}

	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   rs,
		EnvTargets: ets,
		GitReader:  gitReader,
		GitPusher:  gitPusher,
		Sync:       syncClient,
		Locker:     &fakePromotionLocker{},
		BaseDomain: "dev.loopzero.example",
		Logger:     slog.Default(),
	})

	// Act
	result, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:      "tn-1",
		ProjectID:     "proj-1",
		ProjectName:   "my-api",
		FromEnv:       "staging",
		ToEnv:         "production",
		CorrelationID: "corr-1",
		UserID:        "user-1",
	})

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PromotionID == "" {
		t.Error("expected promotion ID to be set")
	}
	if result.SourceRelease.ID != "rel-src-1" {
		t.Errorf("expected source release rel-src-1, got %s", result.SourceRelease.ID)
	}
	if result.NewRelease == nil {
		t.Fatal("expected new release to be created")
	}
	if result.NewRelease.Environment != "production" {
		t.Errorf("expected new release environment production, got %s", result.NewRelease.Environment)
	}
	if result.NewRelease.BuildID != "build-1" {
		t.Errorf("expected new release to reference same build, got %s", result.NewRelease.BuildID)
	}
	if result.NewRelease.Status != "PENDING" {
		t.Errorf("expected PENDING status, got %s", result.NewRelease.Status)
	}
	if result.FromEnv != "staging" {
		t.Errorf("expected from staging, got %s", result.FromEnv)
	}
	if result.ToEnv != "production" {
		t.Errorf("expected to production, got %s", result.ToEnv)
	}
	if result.URL != "https://my-api-production.dev.loopzero.example" {
		t.Errorf("expected URL, got %s", result.URL)
	}

	// Verify git push was to target environment
	if !gitPusher.pushed {
		t.Error("expected git push")
	}
	if gitPusher.lastOpts.Environment != "production" {
		t.Errorf("expected push to production, got %s", gitPusher.lastOpts.Environment)
	}
	if gitPusher.lastOpts.BuildID != "build-1" {
		t.Errorf("expected push with build-1, got %s", gitPusher.lastOpts.BuildID)
	}

	// Verify sync was triggered
	if !syncClient.synced {
		t.Error("expected sync trigger")
	}

	// Verify desired was set in target env
	if ets.desired["tn-1-proj-1-production"] == "" {
		t.Error("expected desired target set for production")
	}

	// Verify new release was created
	if len(rs.created) != 1 {
		t.Fatalf("expected 1 release created, got %d", len(rs.created))
	}
}

func TestPromote_SameEnvironment(t *testing.T) {
	// Arrange
	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   newFakePromotionReleaseStore(),
		EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader:  &fakePromotionGitReader{},
		GitPusher:  &fakePromotionGitPusher{},
		Sync:       &fakePromotionSyncClient{},
		Locker:     &fakePromotionLocker{},
		Logger:     slog.Default(),
	})

	// Act
	_, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		FromEnv:     "staging",
		ToEnv:       "staging",
	})

	// Assert
	if err == nil {
		t.Fatal("expected error for same environment")
	}
	if err != loopzeroerrors.ErrSameEnvironment {
		t.Errorf("expected ErrSameEnvironment, got %v", err)
	}
}

func TestPromote_NoSourceRelease(t *testing.T) {
	// Arrange
	rs := newFakePromotionReleaseStore()
	// No active release in staging

	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader:  &fakePromotionGitReader{},
		GitPusher:  &fakePromotionGitPusher{},
		Sync:       &fakePromotionSyncClient{},
		Locker:     &fakePromotionLocker{},
		Logger:     slog.Default(),
	})

	// Act
	_, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		FromEnv:     "staging",
		ToEnv:       "production",
	})

	// Assert
	if err == nil {
		t.Fatal("expected error for no source release")
	}
	if err != loopzeroerrors.ErrNoActiveRelease {
		t.Errorf("expected ErrNoActiveRelease, got %v", err)
	}
}

func TestPromote_NoGitCommit(t *testing.T) {
	// Arrange
	rs := newFakePromotionReleaseStore()
	sourceRelease := &db.Release{
		ID:          "rel-src-1",
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "staging",
		Status:      string(statemachine.ReleaseActive),
		GitCommit:   "", // no git commit
	}
	rs.activeByEnv["tn-1-proj-1-staging"] = sourceRelease

	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader:  &fakePromotionGitReader{},
		GitPusher:  &fakePromotionGitPusher{},
		Sync:       &fakePromotionSyncClient{},
		Locker:     &fakePromotionLocker{},
		Logger:     slog.Default(),
	})

	// Act
	_, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		FromEnv:     "staging",
		ToEnv:       "production",
	})

	// Assert
	if err == nil {
		t.Fatal("expected error for no git commit")
	}
}

func TestPromote_SyncFailure_DoesNotFail(t *testing.T) {
	// Arrange: sync fails but promotion should still succeed
	rs := newFakePromotionReleaseStore()
	sourceRelease := &db.Release{
		ID:          "rel-src-1",
		BuildID:     "build-1",
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "staging",
		Status:      string(statemachine.ReleaseActive),
		GitCommit:   "abc123",
		Apps:        json.RawMessage(`[]`),
	}
	rs.releases["rel-src-1"] = sourceRelease
	rs.activeByEnv["tn-1-proj-1-staging"] = sourceRelease

	gitReader := &fakePromotionGitReader{manifests: map[string][]byte{"abc123": []byte("yaml")}}
	syncClient := &fakePromotionSyncClient{syncErr: fmt.Errorf("sync unavailable")}

	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader:  gitReader,
		GitPusher:  &fakePromotionGitPusher{},
		Sync:       syncClient,
		Locker:     &fakePromotionLocker{},
		BaseDomain: "dev.loopzero.example",
		Logger:     slog.Default(),
	})

	// Act
	result, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		ProjectName: "my-api",
		FromEnv:     "staging",
		ToEnv:       "production",
	})

	// Assert: promotion succeeds despite sync failure (convergence loop picks up)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.NewRelease == nil {
		t.Fatal("expected new release to be created")
	}
}

func TestPromote_NoBaseDomain_EmptyURL(t *testing.T) {
	// Arrange
	rs := newFakePromotionReleaseStore()
	sourceRelease := &db.Release{
		ID:          "rel-src-1",
		BuildID:     "build-1",
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "staging",
		Status:      string(statemachine.ReleaseActive),
		GitCommit:   "abc123",
		Apps:        json.RawMessage(`[]`),
	}
	rs.releases["rel-src-1"] = sourceRelease
	rs.activeByEnv["tn-1-proj-1-staging"] = sourceRelease

	gitReader := &fakePromotionGitReader{manifests: map[string][]byte{"abc123": []byte("yaml")}}

	orch := NewPromotionOrchestrator(PromotionOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakePromotionEnvTargetStore(),
		GitReader:  gitReader,
		GitPusher:  &fakePromotionGitPusher{},
		Sync:       &fakePromotionSyncClient{},
		Locker:     &fakePromotionLocker{},
		BaseDomain: "", // no base domain
		Logger:     slog.Default(),
	})

	// Act
	result, err := orch.Promote(context.Background(), PromotionRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		ProjectName: "my-api",
		FromEnv:     "staging",
		ToEnv:       "production",
	})

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.URL != "" {
		t.Errorf("expected empty URL, got %q", result.URL)
	}
}
