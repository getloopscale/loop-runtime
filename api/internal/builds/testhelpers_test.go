// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"encoding/json"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// testLogger returns a discarding slog.Logger for tests. M9 Step 6: the shared
// services-level test helper (the placement context kept its own copy in

// fakeLockerContainer is a no-op EnvironmentLocker for build-context tests. M9
// Step 6: the build context kept its own copy when container_delivery (and its
// test fake) moved to the release context.
type fakeLockerContainer struct{}

func (f *fakeLockerContainer) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

// M9 Step 6: build-context copies of the container-delivery test fakes.
// container_delivery moved to the release context; these no-op doubles are
// reused by build_orchestrator tests, so the build package keeps its own copy
// (test scaffolding is per-package; duplicating trivial fakes is not a DRY issue).
type fakeEventStoreContainer struct {
	events []struct {
		BuildID   string
		EventType string
	}
	nextID int64
}

func (f *fakeEventStoreContainer) Append(_ context.Context, buildID, eventType string, _ json.RawMessage) (int64, error) {
	f.nextID++
	f.events = append(f.events, struct {
		BuildID   string
		EventType string
	}{buildID, eventType})
	return f.nextID, nil
}

type fakeGitOpsContainer struct {
	pushErr   error
	commitSHA string
}

func (f *fakeGitOpsContainer) Push(_ context.Context, _ deploycontracts.GitOpsPushOpts) (string, error) {
	return f.commitSHA, f.pushErr
}

func (f *fakeGitOpsContainer) Remove(_ context.Context, _ deploycontracts.GitOpsRemoveOpts) (string, error) {
	return "", nil
}

type fakeSyncContainer struct {
	triggerErr error
	triggered  bool
}

func (f *fakeSyncContainer) TriggerSync(_ context.Context, _ string) error {
	f.triggered = true
	return f.triggerErr
}

func (f *fakeSyncContainer) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	return nil
}

func (f *fakeSyncContainer) ForceReconcile(_ context.Context, _ string) error {
	return nil
}

func (f *fakeSyncContainer) GetSyncRevision(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (f *fakeSyncContainer) TerminateOperation(_ context.Context, _ string) error {
	return nil
}

func (f *fakeSyncContainer) GetAppStatus(_ context.Context, _ string, _ []string) (bool, bool, error) {
	return true, true, nil
}

func newFakeReleaseStoreContainer() *fakeReleaseStoreContainer {
	return &fakeReleaseStoreContainer{
		releases:    make(map[string]*db.Release),
		lastStatus:  make(map[string]string),
		activeByEnv: make(map[string]*db.Release),
		gitCommits:  make(map[string]string),
	}
}

type fakeReleaseStoreContainer struct {
	releases    map[string]*db.Release
	lastStatus  map[string]string
	activeByEnv map[string]*db.Release
	gitCommits  map[string]string
}

func (f *fakeReleaseStoreContainer) Create(_ context.Context, _ db.DBTX, r *db.Release) error {
	r.ID = "rel-" + r.BuildID
	r.Status = string(statemachine.ReleaseReady)
	f.releases[r.ID] = r
	return nil
}

func (f *fakeReleaseStoreContainer) GetByID(_ context.Context, id string) (*db.Release, error) {
	return f.releases[id], nil
}

func (f *fakeReleaseStoreContainer) UpdateStatus(_ context.Context, _ db.DBTX, id, event string) (*db.Release, error) {
	f.lastStatus[id] = event
	return f.releases[id], nil
}

func (f *fakeReleaseStoreContainer) SetGitCommit(_ context.Context, _ db.DBTX, id, sha string) error {
	f.gitCommits[id] = sha
	return nil
}

func (f *fakeReleaseStoreContainer) GetActiveByEnv(_ context.Context, tenant, project, env string) (*db.Release, error) {
	return f.activeByEnv[tenant+"-"+project+"-"+env], nil
}

func (f *fakeReleaseStoreContainer) GetActiveByEnvTx(_ context.Context, _ db.DBTX, tenant, project, env string) (*db.Release, error) {
	return f.activeByEnv[tenant+"-"+project+"-"+env], nil
}

func (f *fakeReleaseStoreContainer) GetPreviousRelease(_ context.Context, _, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreContainer) GetByIDForTenant(_ context.Context, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreContainer) ListByEnv(_ context.Context, _, _, _ string, _ int) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreContainer) GetLatestByBuildID(_ context.Context, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreContainer) FindPendingReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreContainer) FindActiveReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreContainer) GetByIDTx(_ context.Context, _ db.DBTX, id string) (*db.Release, error) {
	return f.releases[id], nil
}

func (f *fakeReleaseStoreContainer) CancelAllByEnv(_ context.Context, _ db.DBTX, _, _, _ string) (int, error) {
	return 0, nil
}

func (f *fakeReleaseStoreContainer) AppendSideEffect(_ context.Context, _ db.DBTX, _ string, _ map[string]any) error {
	return nil
}

func newFakeEnvTargetStoreContainer() *fakeEnvTargetStoreContainer {
	return &fakeEnvTargetStoreContainer{desired: make(map[string]string)}
}

type fakeEnvTargetStoreContainer struct {
	desired map[string]string
}

func (f *fakeEnvTargetStoreContainer) SetDesired(_ context.Context, _ db.DBTX, t, p, e, rel, _, _ string) error {
	f.desired[t+"-"+p+"-"+e] = rel
	return nil
}

func (f *fakeEnvTargetStoreContainer) SetActive(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	return nil
}

func (f *fakeEnvTargetStoreContainer) Get(_ context.Context, _, _, _ string) (*store.EnvironmentTarget, error) {
	return nil, nil
}

func (f *fakeEnvTargetStoreContainer) ListByProject(_ context.Context, _, _ string) ([]store.EnvironmentTarget, error) {
	return nil, nil
}

func (f *fakeEnvTargetStoreContainer) DeleteTarget(_ context.Context, _ db.DBTX, _, _, _ string) error {
	return nil
}

func (f *fakeEnvTargetStoreContainer) SetPreviewDesired(_ context.Context, _ db.DBTX, t, p, e, rel, _, _ string) error {
	// Same map as SetDesired; tests assert on key shape, not kind.
	f.desired[t+"-"+p+"-"+e] = rel
	return nil
}

func (f *fakeEnvTargetStoreContainer) CountPermanent(_ context.Context, _ string) (int, error) {
	return 0, nil
}

func (f *fakeEnvTargetStoreContainer) ListPermanent(_ context.Context, _ string) ([]string, error) {
	return []string{}, nil
}

func (f *fakeEnvTargetStoreContainer) DeleteByKind(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	return nil
}
