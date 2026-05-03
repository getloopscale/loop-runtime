// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/pkg/statemachine"
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

func (f *fakeEnvTargetStoreStatic) Get(_ context.Context, _, _, _ string) (*db.EnvironmentTarget, error) {
	return nil, nil
}

func (f *fakeEnvTargetStoreStatic) ListByProject(_ context.Context, _, _ string) ([]db.EnvironmentTarget, error) {
	return nil, nil
}

func (f *fakeEnvTargetStoreStatic) DeleteTarget(_ context.Context, _ db.DBTX, _, _, _ string) error {
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
	d := NewPGClaimDedup(nil, false, "pod-1")
	if !d.TryClaim(context.Background(), "builds", "bld-001") {
		t.Error("disabled TryClaim should always return true")
	}
}

func TestPGClaimDedup_Disabled_ReleaseIsNoop(t *testing.T) {
	d := NewPGClaimDedup(nil, false, "pod-1")
	// Should not panic
	d.Release(context.Background(), "builds", "bld-001")
}

func TestPGClaimDedup_Disabled_IsProcessingPGReturnsFalse(t *testing.T) {
	d := NewPGClaimDedup(nil, false, "pod-1")
	if d.IsProcessingPG(context.Background(), "builds", "bld-001") {
		t.Error("disabled IsProcessingPG should return false")
	}
}

func TestPGClaimDedup_NilStore_TryClaimTrue(t *testing.T) {
	d := NewPGClaimDedup(nil, true, "pod-1")
	if !d.TryClaim(context.Background(), "builds", "bld-001") {
		t.Error("nil store TryClaim should return true")
	}
}

func TestPGClaimDedup_NilStore_ReleaseIsNoop(t *testing.T) {
	d := NewPGClaimDedup(nil, true, "pod-1")
	d.Release(context.Background(), "builds", "bld-001")
}

func TestPGClaimDedup_NilStore_IsProcessingPGFalse(t *testing.T) {
	d := NewPGClaimDedup(nil, true, "pod-1")
	if d.IsProcessingPG(context.Background(), "builds", "bld-001") {
		t.Error("nil store IsProcessingPG should return false")
	}
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
	activator := NewSyncCallbackActivator(store, nil, nil, nil, "dev.loopzero.example", nil)
	if activator == nil {
		t.Fatal("expected non-nil activator")
	}
}

func TestSyncCallbackActivator_OnSyncCompleted_InvalidAppName(t *testing.T) {
	store := &fakeReleaseConvergenceStoreForCallback{}
	activator := NewSyncCallbackActivator(store, nil, nil, nil, "dev.loopzero.example", nil)

	err := activator.OnSyncCompleted("nohyphen", "abc123")
	if err == nil {
		t.Fatal("expected error for invalid app name")
	}
}

func TestSyncCallbackActivator_OnSyncCompleted_NoMatchingReleases(t *testing.T) {
	store := &fakeReleaseConvergenceStoreForCallback{
		pendingReleases: nil, // no pending releases
	}
	es := &fakeCallbackEventStore{}
	activator := NewSyncCallbackActivator(store, nil, nil, es, "dev.loopzero.example", testLogger())

	// Should succeed with 0 activations
	err := activator.OnSyncCompleted("tn-acme-production", "rev123")
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
	activator := NewSyncCallbackActivator(store, nil, nil, es, "dev.loopzero.example", testLogger())

	err := activator.OnSyncCompleted("tn-acme-production", "rev123")
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
	activator := NewSyncCallbackActivator(store, nil, nil, es, "dev.loopzero.example", testLogger())

	err := activator.OnSyncCompleted("tn-acme-production", "different-revision")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No activation due to revision mismatch
	if len(store.statuses) > 0 {
		t.Errorf("expected no status changes for mismatched revision, got %v", store.statuses)
	}
}

func TestSyncCallbackActivator_OnSyncCompleted_ActivatesMatchingRelease(t *testing.T) {
	appsJSON, _ := json.Marshal([]AppRelease{{Name: "api", URL: "https://api.dev.loopzero.example"}})
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

	activator := NewSyncCallbackActivator(store, ets, locker, es, "dev.loopzero.example", testLogger())

	err := activator.OnSyncCompleted("tn-acme-production", "commit-abc")
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
// ParseArgoAppName additional edge cases
// =============================================================================

func TestParseArgoAppName_OnlyNumbers(t *testing.T) {
	_, _, ok := ParseArgoAppName("12345")
	if ok {
		t.Error("expected false for string without hyphen")
	}
}

func TestParseArgoAppName_MultipleHyphens(t *testing.T) {
	tenant, env, ok := ParseArgoAppName("tn-my-team-e2e-staging")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tenant != "tn-my-team-e2e" {
		t.Errorf("tenant = %q, want tn-my-team-e2e", tenant)
	}
	if env != "staging" {
		t.Errorf("env = %q, want staging", env)
	}
}

// =============================================================================
// Release convergence loop — cellID helper
// =============================================================================

func TestReleaseConvergenceLoop_CellID_Default(t *testing.T) {
	loop := &ReleaseConvergenceLoop{
		cfg: ReleaseConvergenceLoopConfig{CellID: ""},
	}
	if loop.cellID() != db.CellDefault {
		t.Errorf("expected %q, got %q", db.CellDefault, loop.cellID())
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
	apps := releaseAppsToActivationApps(release, "dev.loopzero.example")
	if len(apps) != 0 {
		t.Errorf("expected 0 apps, got %d", len(apps))
	}
}

func TestReleaseAppsToActivationApps_WithApps(t *testing.T) {
	appsJSON, _ := json.Marshal([]AppRelease{
		{Name: "api", URL: "https://api.dev.loopzero.example", Type: "server"},
		{Name: "web", URL: "https://web.dev.loopzero.example", Type: "static"},
	})
	release := &db.Release{
		TenantID:    "tn-acme",
		ProjectID:   "my-app",
		Environment: "production",
		Apps:        appsJSON,
	}
	apps := releaseAppsToActivationApps(release, "dev.loopzero.example")
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
	apps := releaseAppsToActivationApps(release, "dev.loopzero.example")
	if len(apps) != 0 {
		t.Errorf("expected 0 apps for invalid JSON, got %d", len(apps))
	}
}

func TestReleaseAppsToActivationApps_NilApps(t *testing.T) {
	release := &db.Release{
		TenantID: "tn-acme", ProjectID: "my-app", Environment: "production",
		Apps: nil,
	}
	apps := releaseAppsToActivationApps(release, "dev.loopzero.example")
	if len(apps) != 0 {
		t.Errorf("expected 0 apps for nil Apps, got %d", len(apps))
	}
}

// =============================================================================
// safeJSONMarshal — cover error handling
// =============================================================================

func TestSafeJSONMarshal_NilInput(t *testing.T) {
	data := safeJSONMarshal(nil)
	if data == nil {
		t.Error("expected non-nil result for nil input")
	}
}

func TestSafeJSONMarshal_MapWithNestedValues(t *testing.T) {
	data := safeJSONMarshal(map[string]interface{}{
		"deploy_id": "d-abc123",
		"status":    "success",
		"duration":  45,
	})
	if data == nil {
		t.Error("expected non-nil result")
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if m["deploy_id"] != "d-abc123" {
		t.Errorf("expected 'd-abc123', got %v", m["deploy_id"])
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
