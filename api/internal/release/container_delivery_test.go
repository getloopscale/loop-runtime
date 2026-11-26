// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// contains checks if a string slice contains a value.
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// --- Test doubles for ContainerDeliveryActivator ---

type fakeReleaseStoreContainer struct {
	releases    map[string]*db.Release
	lastStatus  map[string]string
	activeByEnv map[string]*db.Release
	gitCommits  map[string]string
}

func newFakeReleaseStoreContainer() *fakeReleaseStoreContainer {
	return &fakeReleaseStoreContainer{
		releases:    make(map[string]*db.Release),
		lastStatus:  make(map[string]string),
		activeByEnv: make(map[string]*db.Release),
		gitCommits:  make(map[string]string),
	}
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

type fakeEnvTargetStoreContainer struct {
	desired map[string]string
}

func newFakeEnvTargetStoreContainer() *fakeEnvTargetStoreContainer {
	return &fakeEnvTargetStoreContainer{desired: make(map[string]string)}
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

// 2026-06-07 D2/D5/D6/D7 — preview-as-category interface extensions.
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

type fakeLockerContainer struct{}

func (f *fakeLockerContainer) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

type fakeSyncContainer struct {
	triggerErr error
	triggered  bool
	waitErr    error
	waited     bool
	// With statusSet, GetAppStatus answers synced and healthy as given; with
	// statusErr it fails. Otherwise it answers synced and healthy.
	synced, healthy bool
	statusErr       error
	statusSet       bool
}

func (f *fakeSyncContainer) TriggerSync(_ context.Context, _ string) error {
	f.triggered = true
	return f.triggerErr
}
func (f *fakeSyncContainer) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	f.waited = true
	return f.waitErr
}

// Sprint B T1.1 (ADR D1): stub methods to satisfy the widened SyncClient
// interface. Container-delivery tests exercise only the TriggerSync /
// WaitForSync paths; ArgoCD recovery methods are noop.
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
	if f.statusErr != nil {
		return false, false, f.statusErr
	}
	if f.statusSet {
		return f.synced, f.healthy, nil
	}
	return true, true, nil
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

type fakeGitReaderContainer struct {
	manifests []byte
}

func (f *fakeGitReaderContainer) GetManifestsAtCommit(_ context.Context, _, _, _, _ string) ([]byte, error) {
	return f.manifests, nil
}

// --- Tests ---

func TestContainerDelivery_Activate_HappyPath(t *testing.T) {
	rs := newFakeReleaseStoreContainer()
	ets := newFakeEnvTargetStoreContainer()
	es := &fakeEventStoreContainer{}
	sync := &fakeSyncContainer{}
	gitops := &fakeGitOpsContainer{commitSHA: "gitcommit123"}

	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: ets,
		EventStore:     es,
		Sync:           sync,
		Locker:         &fakeLockerContainer{},
		GitOps:         gitops,
	})

	build := deploycontracts.ActivationBuild{
		ID: "build-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", CreatedBy: "user-1",
	}
	result := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      []deploycontracts.AppRelease{{Name: "api", Type: "server"}},
		Manifests: "apiVersion: v1\nkind: Deployment",
	}

	err := activator.Activate(context.Background(), build, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify release was created
	if len(rs.releases) != 1 {
		t.Errorf("expected 1 release, got %d", len(rs.releases))
	}

	// Verify git commit was recorded
	relID := "rel-build-1"
	if rs.gitCommits[relID] != "gitcommit123" {
		t.Errorf("expected git commit gitcommit123, got %s", rs.gitCommits[relID])
	}

	// Verify transition to PENDING
	if rs.lastStatus[relID] != string(statemachine.ReleaseGitMirrorCompleted) {
		t.Errorf("expected %s, got %s", statemachine.ReleaseGitMirrorCompleted, rs.lastStatus[relID])
	}

	// Verify env target was set
	if ets.desired["tn-1-proj-1-production"] != relID {
		t.Errorf("expected desired target %s, got %s", relID, ets.desired["tn-1-proj-1-production"])
	}

	// Verify ArgoCD sync triggered
	if !sync.triggered {
		t.Error("expected ArgoCD sync to be triggered")
	}

	// Verify SSE events
	eventTypes := make([]string, 0, len(es.events))
	for _, e := range es.events {
		eventTypes = append(eventTypes, e.EventType)
	}
	if !contains(eventTypes, "deploy.delivering") {
		t.Error("expected deploy.delivering event")
	}
	if !contains(eventTypes, "deploy.verifying") {
		t.Error("expected deploy.verifying event")
	}
}

func TestContainerDelivery_Activate_GitOpsPushFails(t *testing.T) {
	rs := newFakeReleaseStoreContainer()
	es := &fakeEventStoreContainer{}
	gitops := &fakeGitOpsContainer{pushErr: fmt.Errorf("gitea unreachable")}

	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: newFakeEnvTargetStoreContainer(),
		EventStore:     es,
		Sync:           &fakeSyncContainer{},
		Locker:         &fakeLockerContainer{},
		GitOps:         gitops,
	})

	build := deploycontracts.ActivationBuild{
		ID: "build-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", CreatedBy: "user-1",
	}
	result := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      []deploycontracts.AppRelease{{Name: "api"}},
		Manifests: "manifests-yaml",
	}

	err := activator.Activate(context.Background(), build, result)
	if err == nil {
		t.Fatal("expected error when gitops push fails")
	}

	// Verify release was transitioned to GIT_MIRROR_FAILED
	relID := "rel-build-2"
	if rs.lastStatus[relID] != string(statemachine.ReleaseGitMirrorFailed_) {
		t.Errorf("expected %s, got %s", statemachine.ReleaseGitMirrorFailed_, rs.lastStatus[relID])
	}

	// Verify delivery_failed event
	eventTypes := make([]string, 0, len(es.events))
	for _, e := range es.events {
		eventTypes = append(eventTypes, e.EventType)
	}
	if !contains(eventTypes, "deploy.delivery_failed") {
		t.Error("expected deploy.delivery_failed event")
	}
}

func TestContainerDelivery_Rollback_HappyPath(t *testing.T) {
	gitops := &fakeGitOpsContainer{commitSHA: "rollback-sha"}
	sync := &fakeSyncContainer{}
	gitReader := &fakeGitReaderContainer{manifests: []byte("old-manifests")}

	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		Sync:      sync,
		GitOps:    gitops,
		GitReader: gitReader,
		Locker:    &fakeLockerContainer{},
	})

	err := activator.Rollback(context.Background(), deploycontracts.RollbackDeliveryRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		TargetRelease: &deploycontracts.RollbackTarget{
			ID:        "rel-old",
			GitCommit: "old-commit-sha",
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sync.triggered {
		t.Error("expected ArgoCD sync to be triggered")
	}
}

func TestContainerDelivery_Rollback_NoGitCommit(t *testing.T) {
	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		Locker: &fakeLockerContainer{},
	})

	err := activator.Rollback(context.Background(), deploycontracts.RollbackDeliveryRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		TargetRelease: &deploycontracts.RollbackTarget{
			ID:        "rel-old",
			GitCommit: "", // no git commit
		},
	})

	if err == nil {
		t.Fatal("expected error when target release has no git commit")
	}
}

func TestContainerDelivery_Activate_SyncTriggerFails_NonRecoverable(t *testing.T) {
	rs := newFakeReleaseStoreContainer()
	es := &fakeEventStoreContainer{}
	gitops := &fakeGitOpsContainer{commitSHA: "sha-1"}
	syncClient := &fakeSyncContainer{triggerErr: fmt.Errorf("connection refused")}

	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: newFakeEnvTargetStoreContainer(),
		EventStore:     es,
		Sync:           syncClient,
		Locker:         &fakeLockerContainer{},
		GitOps:         gitops,
	})

	build := deploycontracts.ActivationBuild{
		ID: "build-sync-fail", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", CreatedBy: "user-1",
	}
	result := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      []deploycontracts.AppRelease{{Name: "api"}},
		Manifests: "manifests",
	}

	err := activator.Activate(context.Background(), build, result)
	if err == nil {
		t.Fatal("expected error when sync trigger fails with non-recoverable error")
	}

	// Release should be marked SYNC_FAILED
	relID := "rel-build-sync-fail"
	if rs.lastStatus[relID] != string(statemachine.ReleaseSyncFailed_) {
		t.Errorf("expected SYNC_FAILED, got %s", rs.lastStatus[relID])
	}
}

func TestContainerDelivery_Activate_SyncTrigger404_StaysPending(t *testing.T) {
	rs := newFakeReleaseStoreContainer()
	es := &fakeEventStoreContainer{}
	gitops := &fakeGitOpsContainer{commitSHA: "sha-2"}
	syncClient := &fakeSyncContainer{triggerErr: fmt.Errorf("argocd sync: application %q not found: %w", "x", deploycontracts.ErrSyncApplicationNotFound)}

	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: newFakeEnvTargetStoreContainer(),
		EventStore:     es,
		Sync:           syncClient,
		Locker:         &fakeLockerContainer{},
		GitOps:         gitops,
	})

	build := deploycontracts.ActivationBuild{
		ID: "build-404", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", CreatedBy: "user-1",
	}
	result := deploycontracts.BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      []deploycontracts.AppRelease{{Name: "api"}},
		Manifests: "manifests",
	}

	err := activator.Activate(context.Background(), build, result)
	// 404 is recoverable — should NOT return error
	if err != nil {
		t.Fatalf("404 sync error should be recoverable, got: %v", err)
	}

	// Release should be in PENDING (GitMirrorCompleted), not SYNC_FAILED
	relID := "rel-build-404"
	if rs.lastStatus[relID] != string(statemachine.ReleaseGitMirrorCompleted) {
		t.Errorf("expected GIT_MIRROR_COMPLETED (PENDING), got %s", rs.lastStatus[relID])
	}
}

// A transport error whose text happens to contain "not found" (a DNS lookup
// failure) is not the ArgoCD Application being absent. Classifying by the
// message text used to leave such releases PENDING forever instead of failing
// them; only the typed sentinel may mark a sync failure as recoverable.
func TestContainerDelivery_Activate_SyncTriggerDNSNotFound_FailsRelease(t *testing.T) {
	rs := newFakeReleaseStoreContainer()
	syncClient := &fakeSyncContainer{triggerErr: fmt.Errorf("dial tcp: lookup argocd-server: host not found")}
	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: newFakeEnvTargetStoreContainer(),
		EventStore:     &fakeEventStoreContainer{},
		Sync:           syncClient,
		Locker:         &fakeLockerContainer{},
		GitOps:         &fakeGitOpsContainer{commitSHA: "sha-dns"},
	})
	build := deploycontracts.ActivationBuild{
		ID: "build-dns", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", CreatedBy: "user-1",
	}
	result := deploycontracts.BuildCallbackRequest{
		Status: "succeeded", Apps: []deploycontracts.AppRelease{{Name: "api"}}, Manifests: "manifests",
	}

	err := activator.Activate(context.Background(), build, result)

	if err == nil {
		t.Fatal("a DNS failure must fail the release, not leave it PENDING")
	}
	if got := rs.lastStatus["rel-build-dns"]; got != string(statemachine.ReleaseSyncFailed_) {
		t.Errorf("expected SYNC_FAILED, got %s", got)
	}
}

func TestContainerDelivery_Activate_SyncTriggerAuthFailed_StaysPending(t *testing.T) {
	rs := newFakeReleaseStoreContainer()
	syncClient := &fakeSyncContainer{triggerErr: loopruntimeerrors.ErrDeployServiceAuthFailed}
	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: newFakeEnvTargetStoreContainer(),
		EventStore:     &fakeEventStoreContainer{},
		Sync:           syncClient,
		Locker:         &fakeLockerContainer{},
		GitOps:         &fakeGitOpsContainer{commitSHA: "sha-auth"},
	})
	build := deploycontracts.ActivationBuild{
		ID: "build-auth", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", CreatedBy: "user-1",
	}
	result := deploycontracts.BuildCallbackRequest{
		Status: "succeeded", Apps: []deploycontracts.AppRelease{{Name: "api"}}, Manifests: "manifests",
	}

	err := activator.Activate(context.Background(), build, result)

	if err != nil {
		t.Fatalf("an auth failure is recoverable (token re-mint), got: %v", err)
	}
	if got := rs.lastStatus["rel-build-auth"]; got != string(statemachine.ReleaseGitMirrorCompleted) {
		t.Errorf("expected GIT_MIRROR_COMPLETED (PENDING), got %s", got)
	}
}

func TestContainerDelivery_Rollback_GitOpsPushFails(t *testing.T) {
	gitops := &fakeGitOpsContainer{pushErr: fmt.Errorf("push failed")}
	gitReader := &fakeGitReaderContainer{manifests: []byte("old-manifests")}

	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		Sync:      &fakeSyncContainer{},
		GitOps:    gitops,
		GitReader: gitReader,
		Locker:    &fakeLockerContainer{},
	})

	err := activator.Rollback(context.Background(), deploycontracts.RollbackDeliveryRequest{
		TenantID:    "tn-1",
		ProjectID:   "proj-1",
		Environment: "production",
		TargetRelease: &deploycontracts.RollbackTarget{
			ID:        "rel-old",
			GitCommit: "old-sha",
		},
	})

	if err == nil {
		t.Fatal("expected error when gitops push fails during rollback")
	}
}

// CHG-2026-04-25-ATTESTATION-WIRE Phase 5 (AC-5.1):
// when AppRelease[0] carries signature data, phaseACreateRelease must
// persist all three columns on the release row.
func TestPhaseACreateRelease_PersistsSignatureColumns(t *testing.T) {
	rs := newFakeReleaseStoreContainer()
	signedAt := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)

	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: newFakeEnvTargetStoreContainer(),
		EventStore:     &fakeEventStoreContainer{},
		Sync:           &fakeSyncContainer{},
		Locker:         &fakeLockerContainer{},
		GitOps:         &fakeGitOpsContainer{commitSHA: "gitcommit123"},
	})

	build := deploycontracts.ActivationBuild{
		ID: "build-sig", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", CreatedBy: "user-1",
	}
	result := deploycontracts.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []deploycontracts.AppRelease{
			{
				Name:              "api",
				Type:              "server",
				ImageDigestSHA256: "sha256:deadbeef",
				ImageSignature:    "registry/api:abc@sha256:deadbeef",
				ImageSignedAt:     &signedAt,
			},
		},
		Manifests: "apiVersion: v1",
	}

	err := activator.Activate(context.Background(), build, result)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	rel, ok := rs.releases["rel-build-sig"]
	if !ok {
		t.Fatal("release not created")
	}
	if rel.ImageDigest == nil || *rel.ImageDigest != "sha256:deadbeef" {
		t.Errorf("ImageDigest: got %v, want sha256:deadbeef", rel.ImageDigest)
	}
	if rel.ImageSignature == nil || *rel.ImageSignature != "registry/api:abc@sha256:deadbeef" {
		t.Errorf("ImageSignature: got %v", rel.ImageSignature)
	}
	if rel.ImageSignedAt == nil || !rel.ImageSignedAt.Equal(signedAt) {
		t.Errorf("ImageSignedAt: got %v, want %v", rel.ImageSignedAt, signedAt)
	}
}

// AC-5.2: legacy/missing AppRelease signature fields must produce a
// release row with all three columns NULL. The deploy must succeed.
func TestPhaseACreateRelease_LegacyAppRelease_NullColumns(t *testing.T) {
	rs := newFakeReleaseStoreContainer()

	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: newFakeEnvTargetStoreContainer(),
		EventStore:     &fakeEventStoreContainer{},
		Sync:           &fakeSyncContainer{},
		Locker:         &fakeLockerContainer{},
		GitOps:         &fakeGitOpsContainer{commitSHA: "gitcommit123"},
	})

	build := deploycontracts.ActivationBuild{
		ID: "build-legacy", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", CreatedBy: "user-1",
	}
	// AppRelease has legacy ImageDigest (the tag) but no signature fields.
	result := deploycontracts.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []deploycontracts.AppRelease{
			{Name: "api", Type: "server", ImageDigest: "registry/api:abc"},
		},
		Manifests: "apiVersion: v1",
	}

	err := activator.Activate(context.Background(), build, result)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	rel := rs.releases["rel-build-legacy"]
	if rel.ImageDigest != nil {
		t.Errorf("ImageDigest must be NULL for legacy release: got %v", rel.ImageDigest)
	}
	if rel.ImageSignature != nil {
		t.Errorf("ImageSignature must be NULL: got %v", rel.ImageSignature)
	}
	if rel.ImageSignedAt != nil {
		t.Errorf("ImageSignedAt must be NULL: got %v", rel.ImageSignedAt)
	}
}

// AC-5.3: digest without signature must NOT set ImageSignedAt — the
// timestamp without the signature reference is meaningless.
func TestPhaseACreateRelease_DigestWithoutSignature_NoSignedAt(t *testing.T) {
	rs := newFakeReleaseStoreContainer()
	signedAt := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)

	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: newFakeEnvTargetStoreContainer(),
		EventStore:     &fakeEventStoreContainer{},
		Sync:           &fakeSyncContainer{},
		Locker:         &fakeLockerContainer{},
		GitOps:         &fakeGitOpsContainer{commitSHA: "gitcommit123"},
	})

	build := deploycontracts.ActivationBuild{
		ID: "build-partial", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", CreatedBy: "user-1",
	}
	// Digest is set, but Signature is NOT (cosign step missing/skipped).
	// SignedAt is provided — but must be ignored because Signature is empty.
	result := deploycontracts.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []deploycontracts.AppRelease{
			{
				Name:              "api",
				ImageDigestSHA256: "sha256:abc",
				ImageSignedAt:     &signedAt,
			},
		},
		Manifests: "apiVersion: v1",
	}

	err := activator.Activate(context.Background(), build, result)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	rel := rs.releases["rel-build-partial"]
	if rel.ImageDigest == nil || *rel.ImageDigest != "sha256:abc" {
		t.Errorf("ImageDigest should be populated: got %v", rel.ImageDigest)
	}
	if rel.ImageSignature != nil {
		t.Errorf("ImageSignature must be NULL: got %v", rel.ImageSignature)
	}
	if rel.ImageSignedAt != nil {
		t.Errorf("ImageSignedAt must be NULL when Signature is empty: got %v", rel.ImageSignedAt)
	}
}

// AC-5.4: in multi-app builds, only Apps[0]'s signature is persisted.
// The current schema has ONE set of signature columns per release —
// per-app attestation requires a sidecar table (out of scope per ADR-0014).
func TestPhaseACreateRelease_MultiApp_PrimaryOnly(t *testing.T) {
	rs := newFakeReleaseStoreContainer()
	signedAt := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)

	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: newFakeEnvTargetStoreContainer(),
		EventStore:     &fakeEventStoreContainer{},
		Sync:           &fakeSyncContainer{},
		Locker:         &fakeLockerContainer{},
		GitOps:         &fakeGitOpsContainer{commitSHA: "gitcommit123"},
	})

	build := deploycontracts.ActivationBuild{
		ID: "build-multi", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", CreatedBy: "user-1",
	}
	result := deploycontracts.BuildCallbackRequest{
		Status: "succeeded",
		Apps: []deploycontracts.AppRelease{
			{
				Name:              "api",
				ImageDigestSHA256: "sha256:apidigest",
				ImageSignature:    "registry/api:abc@sha256:apidigest",
				ImageSignedAt:     &signedAt,
			},
			{
				Name:              "web",
				ImageDigestSHA256: "sha256:webdigest",
				ImageSignature:    "registry/web:abc@sha256:webdigest",
			},
		},
		Manifests: "apiVersion: v1",
	}

	err := activator.Activate(context.Background(), build, result)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}

	rel := rs.releases["rel-build-multi"]
	if rel.ImageDigest == nil || *rel.ImageDigest != "sha256:apidigest" {
		t.Errorf("expected primary (Apps[0]) digest, got %v", rel.ImageDigest)
	}
}

// AppendSideEffect — fix-all-platform-issues-plan T4.1 stub.
func (f *fakeReleaseStoreContainer) AppendSideEffect(_ context.Context, _ db.DBTX, _ string, _ map[string]any) error {
	return nil
}

func TestContainerSyncPolicy_TerminatesOnlyBeforeTheFinalStuckAttempt(t *testing.T) {
	if containerSyncPolicy.ShouldTerminate(containerSyncPolicy.MaxStuckAttempts - 2) {
		t.Error("an earlier attempt must not cancel a peer's in-flight sync")
	}
	if !containerSyncPolicy.ShouldTerminate(containerSyncPolicy.MaxStuckAttempts - 1) {
		t.Error("the stuck operation must be terminated before the final attempt")
	}
}

var rollbackDeliveryRequest = deploycontracts.RollbackDeliveryRequest{
	TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
	TargetRelease: &deploycontracts.RollbackTarget{ID: "rel-old", GitCommit: "old"},
}

func rollbackActivator(sync *fakeSyncContainer) *ContainerDeliveryActivator {
	return NewContainerDeliveryActivator(ContainerDeliveryDeps{
		Sync: sync, GitOps: &fakeGitOpsContainer{commitSHA: "sha"},
		GitReader: &fakeGitReaderContainer{manifests: []byte("m")}, Locker: &fakeLockerContainer{},
	})
}

// A rollout through canary steps outlives the request that asked for the
// rollback (#118), so starting the delivery does not wait for the sync.
func TestContainerDelivery_Rollback_TriggersTheSyncAndDoesNotWaitForIt(t *testing.T) {
	sync := &fakeSyncContainer{waitErr: fmt.Errorf("argo sync timeout")}

	err := rollbackActivator(sync).Rollback(context.Background(), rollbackDeliveryRequest)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sync.triggered {
		t.Error("the rollback delivery did not trigger the sync")
	}
	if sync.waited {
		t.Error("the rollback delivery waited for the sync, which ties it to the request")
	}
}

// The DB safety invariant of rollback (LIVE sponsor mandate 2026-06-18): the
// orchestrator flips release state only after AwaitRollback returns nil, so it
// must not return nil before the sync is confirmed.
func TestContainerDelivery_AwaitRollback_WaitsForSyncConfirmation(t *testing.T) {
	sync := &fakeSyncContainer{}

	err := rollbackActivator(sync).AwaitRollback(context.Background(), rollbackDeliveryRequest)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sync.waited {
		t.Error("AwaitRollback returned without waiting for sync confirmation")
	}
}

func TestContainerDelivery_AwaitRollback_FailsWhenTheSyncIsNotConfirmed(t *testing.T) {
	sync := &fakeSyncContainer{waitErr: fmt.Errorf("argo sync timeout")}

	if err := rollbackActivator(sync).AwaitRollback(context.Background(), rollbackDeliveryRequest); err == nil {
		t.Fatal("a sync that never confirmed must fail AwaitRollback")
	}
}

func TestContainerDelivery_RollbackConverged_NeedsSyncedAndHealthy(t *testing.T) {
	for name, tc := range map[string]struct {
		synced, healthy, want bool
	}{
		"synced and healthy":   {true, true, true},
		"synced, not healthy":  {true, false, false},
		"healthy, out of sync": {false, true, false},
	} {
		t.Run(name, func(t *testing.T) {
			sync := &fakeSyncContainer{statusSet: true, synced: tc.synced, healthy: tc.healthy}

			got, err := rollbackActivator(sync).RollbackConverged(context.Background(), rollbackDeliveryRequest)

			if err != nil || got != tc.want {
				t.Fatalf("RollbackConverged = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestContainerDelivery_RollbackConverged_ReportsAStatusItCannotRead(t *testing.T) {
	sync := &fakeSyncContainer{statusErr: fmt.Errorf("argocd: connection refused")}

	if got, err := rollbackActivator(sync).RollbackConverged(context.Background(), rollbackDeliveryRequest); err == nil || got {
		t.Fatalf("RollbackConverged = %v, %v; want false and the error", got, err)
	}
}
