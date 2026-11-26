// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/processing"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// fakeEnvTargetStoreStatic is a test double for EnvironmentTargetStore.
type fakeEnvTargetStoreStatic struct {
	desired map[string]string
	active  map[string]string
}

func newFakeEnvTargetStoreStatic() *fakeEnvTargetStoreStatic {
	return &fakeEnvTargetStoreStatic{
		desired: make(map[string]string),
		active:  make(map[string]string),
	}
}

func (f *fakeEnvTargetStoreStatic) SetDesired(_ context.Context, _ db.DBTX, tenant, project, env, releaseID, _, _ string) error {
	f.desired[tenant+"-"+project+"-"+env] = releaseID
	return nil
}

func (f *fakeEnvTargetStoreStatic) SetActive(_ context.Context, _ db.DBTX, tenant, project, env, releaseID string) error {
	f.active[tenant+"-"+project+"-"+env] = releaseID
	return nil
}

func (f *fakeEnvTargetStoreStatic) Get(_ context.Context, _, _, _ string) (*store.EnvironmentTarget, error) {
	return nil, nil
}

func (f *fakeEnvTargetStoreStatic) ListByProject(_ context.Context, _, _ string) ([]store.EnvironmentTarget, error) {
	return nil, nil
}

func (f *fakeEnvTargetStoreStatic) DeleteTarget(_ context.Context, _ db.DBTX, _, _, _ string) error {
	return nil
}

// 2026-06-07 D2/D5/D6/D7 — preview-as-category interface extensions.
func (f *fakeEnvTargetStoreStatic) SetPreviewDesired(_ context.Context, _ db.DBTX, _, _, _, _, _, _ string) error {
	return nil
}
func (f *fakeEnvTargetStoreStatic) CountPermanent(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (f *fakeEnvTargetStoreStatic) ListPermanent(_ context.Context, _ string) ([]string, error) {
	return []string{}, nil
}
func (f *fakeEnvTargetStoreStatic) DeleteByKind(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	return nil
}

// fakeLockerStatic is a no-op locker for tests.
type fakeLockerStatic struct{}

func (f *fakeLockerStatic) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

// =============================================================================
// PGClaimDedup tests (previously 0% coverage)
// =============================================================================

func TestPGClaimDedup_Disabled_TryClaimAlwaysTrue(t *testing.T) {
	d := processing.NewPGClaimDedup(nil, false, "pod-1")
	if claimed, err := d.TryClaim(context.Background(), "builds", "bld-001"); !claimed || err != nil {
		t.Error("disabled TryClaim should always grant")
	}
}

func TestPGClaimDedup_Disabled_ReleaseIsNoop(t *testing.T) {
	d := processing.NewPGClaimDedup(nil, false, "pod-1")
	// Should not panic
	d.Release(context.Background(), "builds", "bld-001")
}

func TestPGClaimDedup_NilStore_TryClaimTrue(t *testing.T) {
	d := processing.NewPGClaimDedup(nil, true, "pod-1")
	if claimed, err := d.TryClaim(context.Background(), "builds", "bld-001"); !claimed || err != nil {
		t.Error("nil store TryClaim should grant")
	}
}

func TestPGClaimDedup_NilStore_ReleaseIsNoop(t *testing.T) {
	d := processing.NewPGClaimDedup(nil, true, "pod-1")
	d.Release(context.Background(), "builds", "bld-001")
}

// =============================================================================
// SyncCallbackActivator tests (previously 0% for New, OnSyncCompleted, activate)
// =============================================================================

type fakeReleaseConvergenceStoreForCallback struct {
	pendingReleases []db.Release
	statuses        map[string]string
}

func (f *fakeReleaseConvergenceStoreForCallback) FindPendingReleases(_ context.Context, _ string) ([]db.Release, error) {
	return f.pendingReleases, nil
}

func (f *fakeReleaseConvergenceStoreForCallback) FindActiveReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseConvergenceStoreForCallback) UpdateStatus(_ context.Context, _ db.DBTX, id, event string) (*db.Release, error) {
	if f.statuses == nil {
		f.statuses = make(map[string]string)
	}
	f.statuses[id] = event
	return &db.Release{ID: id, Status: event}, nil
}

func (f *fakeReleaseConvergenceStoreForCallback) GetActiveByEnvTx(_ context.Context, _ db.DBTX, _, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseConvergenceStoreForCallback) GetByIDTx(_ context.Context, _ db.DBTX, id string) (*db.Release, error) {
	for _, r := range f.pendingReleases {
		if r.ID == id {
			return &r, nil
		}
	}
	return nil, nil
}

func (f *fakeReleaseConvergenceStoreForCallback) CancelAllByEnv(_ context.Context, _ db.DBTX, _, _, _ string) (int, error) {
	return 0, nil
}

func TestNewSyncCallbackActivator(t *testing.T) {
	store := &fakeReleaseConvergenceStoreForCallback{}
	activator := NewSyncCallbackActivator(store, nil, nil, nil, "dev.getloopscale.com", "", nil)
	if activator == nil {
		t.Fatal("expected non-nil activator")
	}
}

func TestSyncCallbackActivator_OnSyncCompleted_UnlabelledAppIsRejected(t *testing.T) {
	store := &fakeReleaseConvergenceStoreForCallback{}
	activator := NewSyncCallbackActivator(store, nil, nil, nil, "dev.getloopscale.com", "", nil).
		WithAppLabelReader(&fakeLabelReader{})

	err := activator.OnSyncCompleted("nohyphen", "abc123")
	if !errors.Is(err, errAppIdentityUnresolved) {
		t.Fatalf("expected errAppIdentityUnresolved, got %v", err)
	}
}

func TestSyncCallbackActivator_OnSyncCompleted_NoMatchingReleases(t *testing.T) {
	store := &fakeReleaseConvergenceStoreForCallback{
		pendingReleases: nil, // no pending releases
	}
	es := &fakeCallbackEventStore{}
	activator := NewSyncCallbackActivator(store, nil, nil, es, "dev.getloopscale.com", "", testLogger()).
		WithAppLabelReader(labelsFor("tn-acme-app-production", "tn-acme", "production"))

	// Should succeed with 0 activations
	err := activator.OnSyncCompleted("tn-acme-app-production", "rev123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncCallbackActivator_OnSyncCompleted_SkipsEmptyGitCommit(t *testing.T) {
	store := &fakeReleaseConvergenceStoreForCallback{
		pendingReleases: []db.Release{
			{
				ID: "rel-1", TenantID: "tn-acme", ProjectID: "app",
				Environment: "production", BuildID: "bld-1",
				GitCommit: "", // empty — should be skipped
				Status:    "PENDING",
			},
		},
	}
	es := &fakeCallbackEventStore{}
	activator := NewSyncCallbackActivator(store, nil, nil, es, "dev.getloopscale.com", "", testLogger()).
		WithAppLabelReader(labelsFor("tn-acme-app-production", "tn-acme", "production"))

	err := activator.OnSyncCompleted("tn-acme-app-production", "rev123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No activation should happen
	if len(store.statuses) > 0 {
		t.Errorf("expected no status changes, got %v", store.statuses)
	}
}

func TestSyncCallbackActivator_OnSyncCompleted_SkipsMismatchedRevision(t *testing.T) {
	store := &fakeReleaseConvergenceStoreForCallback{
		pendingReleases: []db.Release{
			{
				ID: "rel-1", TenantID: "tn-acme", ProjectID: "app",
				Environment: "production", BuildID: "bld-1",
				GitCommit: "commit-abc",
				Status:    "PENDING",
			},
		},
	}
	es := &fakeCallbackEventStore{}
	activator := NewSyncCallbackActivator(store, nil, nil, es, "dev.getloopscale.com", "", testLogger()).
		WithAppLabelReader(labelsFor("tn-acme-app-production", "tn-acme", "production"))

	err := activator.OnSyncCompleted("tn-acme-app-production", "different-revision")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No activation due to revision mismatch
	if len(store.statuses) > 0 {
		t.Errorf("expected no status changes for mismatched revision, got %v", store.statuses)
	}
}

func TestSyncCallbackActivator_OnSyncCompleted_ActivatesMatchingRelease(t *testing.T) {
	appsJSON, _ := json.Marshal([]deploycontracts.AppRelease{{Name: "api", URL: "https://api.dev.getloopscale.com"}})
	store := &fakeReleaseConvergenceStoreForCallback{
		pendingReleases: []db.Release{
			{
				ID: "rel-1", TenantID: "tn-acme", ProjectID: "app",
				Environment: "production", BuildID: "bld-1",
				GitCommit: "commit-abc", Status: "PENDING",
				Apps: appsJSON, CreatedAt: time.Now().Add(-30 * time.Second),
			},
		},
	}
	es := &fakeCallbackEventStore{}
	ets := newFakeEnvTargetStoreStatic()
	locker := &fakeLockerStatic{}

	activator := NewSyncCallbackActivator(store, ets, locker, es, "dev.getloopscale.com", "", testLogger()).
		WithAppLabelReader(labelsFor("tn-acme-app-production", "tn-acme", "production"))

	err := activator.OnSyncCompleted("tn-acme-app-production", "commit-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have activated
	if store.statuses["rel-1"] != string(statemachine.ReleaseSyncCompleted) {
		t.Errorf("expected release activated with %s, got %q",
			statemachine.ReleaseSyncCompleted, store.statuses["rel-1"])
	}
}

type fakeCallbackEventStore struct {
	events []struct {
		BuildID   string
		EventType string
	}
}

func (f *fakeCallbackEventStore) Append(_ context.Context, buildID, eventType string, _ json.RawMessage) (int64, error) {
	f.events = append(f.events, struct {
		BuildID   string
		EventType string
	}{buildID, eventType})
	return int64(len(f.events)), nil
}

// =============================================================================
// Release convergence loop — cellID helper
// =============================================================================

// An empty cell scopes the loop to every cell, as backgroundloops.Deps documents. It
// used to become cell-0, and a release placed on cell-1 stayed PENDING forever (#447).
func TestReleaseConvergenceLoop_CellID_EmptyMeansEveryCell(t *testing.T) {
	loop := &ReleaseConvergenceLoop{
		cfg: ReleaseConvergenceLoopConfig{CellID: ""},
	}
	if loop.cellID() != "" {
		t.Errorf("expected every cell (\"\"), got %q", loop.cellID())
	}
}

func TestReleaseConvergenceLoop_CellID_Custom(t *testing.T) {
	loop := &ReleaseConvergenceLoop{
		cfg: ReleaseConvergenceLoopConfig{CellID: "cell-us-east-1"},
	}
	if loop.cellID() != "cell-us-east-1" {
		t.Errorf("expected cell-us-east-1, got %q", loop.cellID())
	}
}

// =============================================================================
// release_activator — emitActivationFailure and releaseAppsToActivationApps
// =============================================================================

func TestReleaseAppsToActivationApps_EmptyApps(t *testing.T) {
	release := &db.Release{
		TenantID:    "tn-acme",
		ProjectID:   "my-app",
		Environment: "production",
		Apps:        json.RawMessage(`[]`),
	}
	apps := releaseAppsToActivationApps(release, "dev.getloopscale.com", "")
	if len(apps) != 0 {
		t.Errorf("expected 0 apps, got %d", len(apps))
	}
}

func TestReleaseAppsToActivationApps_WithApps(t *testing.T) {
	appsJSON, _ := json.Marshal([]deploycontracts.AppRelease{
		{Name: "api", URL: "https://api.dev.getloopscale.com", Type: "server"},
		{Name: "web", URL: "https://web.dev.getloopscale.com", Type: "static"},
	})
	release := &db.Release{
		TenantID:    "tn-acme",
		ProjectID:   "my-app",
		Environment: "production",
		Apps:        appsJSON,
	}
	apps := releaseAppsToActivationApps(release, "dev.getloopscale.com", "")
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}
	if apps[0].Name != "api" {
		t.Errorf("expected 'api', got %q", apps[0].Name)
	}
}

func TestReleaseAppsToActivationApps_InvalidJSON(t *testing.T) {
	release := &db.Release{
		TenantID: "tn-acme", ProjectID: "my-app", Environment: "production",
		Apps: json.RawMessage(`invalid-json`),
	}
	apps := releaseAppsToActivationApps(release, "dev.getloopscale.com", "")
	if len(apps) != 0 {
		t.Errorf("expected 0 apps for invalid JSON, got %d", len(apps))
	}
}

func TestReleaseAppsToActivationApps_NilApps(t *testing.T) {
	release := &db.Release{
		TenantID: "tn-acme", ProjectID: "my-app", Environment: "production",
		Apps: nil,
	}
	apps := releaseAppsToActivationApps(release, "dev.getloopscale.com", "")
	if len(apps) != 0 {
		t.Errorf("expected 0 apps for nil Apps, got %d", len(apps))
	}
}

// TestReleaseAppsToActivationApps_PublicDomainPreferredOverBase_M61 is the
// release-blockers-2026-06-25-set v1.1 #61 RED test. When both baseDomain
// ("loopruntime.local" — internal, non-resolvable) AND publicDomain
// ("prod-dev.getloopscale.app" — canonical public wildcard) are set, the URL
// emitted on deploy.completed MUST use publicDomain — the SAME URL that
// `loopscale status` returns. Pre-#61 the function only accepted baseDomain →
// `loopscale deploy` announced `.loopruntime.local` while `loopscale status` returned
// `.prod-dev.getloopscale.app`. The mismatch is the bug.
func TestReleaseAppsToActivationApps_PublicDomainPreferredOverBase_M61(t *testing.T) {
	appsJSON, _ := json.Marshal([]deploycontracts.AppRelease{
		{Name: "app", Type: "server"},
	})
	release := &db.Release{
		TenantID:    "tn-cb13ca5299a9391a",
		ProjectID:   "loopzerokit-example",
		Environment: "preview",
		Apps:        appsJSON,
	}
	apps := releaseAppsToActivationApps(release, "loopruntime.local", "prod-dev.getloopscale.app")
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}
	wantURL := "https://tn-cb13ca5299a9391a-loopzerokit-example-app-preview.prod-dev.getloopscale.app"
	if apps[0].URL != wantURL {
		t.Errorf("#61 regression: got URL %q, want %q (publicDomain MUST be preferred over .loopruntime.local)",
			apps[0].URL, wantURL)
	}
	// Critical anti-regression: NEVER announce the internal .loopruntime.local hostname.
	if strings.Contains(apps[0].URL, ".loopruntime.local") {
		t.Fatalf("#61 regression: URL %q must NOT contain the internal .loopruntime.local hostname", apps[0].URL)
	}
}

// TestReleaseAppsToActivationApps_FallsBackToBaseDomainWhenPublicEmpty_M61
// is the backward-compat guard: existing test fixtures + local-dev envs
// without a public override must still get the baseDomain-based URL.
func TestReleaseAppsToActivationApps_FallsBackToBaseDomainWhenPublicEmpty_M61(t *testing.T) {
	appsJSON, _ := json.Marshal([]deploycontracts.AppRelease{
		{Name: "app", Type: "server"},
	})
	release := &db.Release{
		TenantID:    "tn-acme",
		ProjectID:   "my-app",
		Environment: "production",
		Apps:        appsJSON,
	}
	apps := releaseAppsToActivationApps(release, "dev.getloopscale.com", "")
	wantURL := "https://tn-acme-my-app-app-production.dev.getloopscale.com"
	if apps[0].URL != wantURL {
		t.Errorf("expected fallback URL %q, got %q", wantURL, apps[0].URL)
	}
}

// =============================================================================
// deriveDeployID
// =============================================================================

func TestDeriveDeployID_Format(t *testing.T) {
	id := deriveDeployID("rel-abc-123")
	if id == "" {
		t.Error("expected non-empty deploy ID")
	}
	if id[:2] != "d-" {
		t.Errorf("expected d- prefix, got %q", id[:2])
	}
}
