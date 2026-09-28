// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	releasesvc "github.com/getloopscale/loopzero/api/internal/release"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
)

// fakeDeployEventStore is a no-op AppendDeployEvent stub. The kernel calls it
// on the SAME tx as the state transition (J-16 outbox — MEET-2026-07-03-005
// Option C). These tests exercise state-transition semantics; the outbox emit
// is covered by dedicated tests in the outbox package. Returning nil keeps the
// kernel's transactional emit path exercised without pulling PG into unit
// tests.
type fakeDeployEventStore struct{}

func (fakeDeployEventStore) AppendDeployEvent(_ context.Context, _ db.DBTX, _ store.DeployEvent) error {
	return nil
}

// --- TestActivationKernel_HappyPath ---
// Full 7-step activation sequence: re-fetch confirms PENDING, gets previous
// active release, transitions to ACTIVE, emits deploy.live, sets active env
// target, supersedes previous release, emits deploy.completed.

func TestActivationKernel_HappyPath(t *testing.T) {
	// Arrange
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-happy-001", BuildID: "build-happy-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-happy-001"] = "PENDING"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	eventStore := &fakeConvergenceEventStore{}

	release := &db.Release{
		ID: "rel-happy-001", BuildID: "build-happy-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now(),
	}

	apps := []releasesvc.ActivationAppInfo{
		{Name: "web", Status: "deployed", URL: "https://acme-my-app-web-production.test.local"},
	}

	// Act
	err := releasesvc.ActivationKernel{
		Releases: releaseStore, EnvTargets: envTargetStore, Events: eventStore, DeployEvents: fakeDeployEventStore{}, Logger: testLogger,
	}.Activate(context.Background(), nil, release, apps, 42)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if releaseStore.getStatus("rel-happy-001") != "ACTIVE" {
		t.Errorf("expected release ACTIVE, got %q", releaseStore.getStatus("rel-happy-001"))
	}
	if envTargetStore.getActiveCalls() != 1 {
		t.Errorf("expected SetActive called once, got %d", envTargetStore.getActiveCalls())
	}

	events := eventStore.getEvents()
	wantEvents := map[string]bool{"deploy.live": false, "deploy.completed": false}
	for _, e := range events {
		if _, ok := wantEvents[e]; ok {
			wantEvents[e] = true
		}
	}
	for name, found := range wantEvents {
		if !found {
			t.Errorf("expected %s event, not found in %v", name, events)
		}
	}
}

// --- TestActivationKernel_SupersedesPreviousRelease ---
// Regression test for the known supersede bug: when a previous ACTIVE release
// exists, it must be superseded after the new release goes ACTIVE.

func TestActivationKernel_SupersedesPreviousRelease(t *testing.T) {
	// Arrange: previous release is ACTIVE
	releaseStore := newFakeConvergenceReleaseStore()

	// New release (PENDING)
	releaseStore.pending = []db.Release{{
		ID: "rel-new-001", BuildID: "build-new-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-new-001"] = "PENDING"

	// Previous release (ACTIVE) — register in the active map
	prevRelease := &db.Release{
		ID: "rel-prev-001", BuildID: "build-prev-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "ACTIVE", CreatedAt: time.Now().Add(-10 * time.Minute),
	}
	releaseStore.active["acme:my-app:production"] = prevRelease
	releaseStore.statuses["rel-prev-001"] = "ACTIVE"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	eventStore := &fakeConvergenceEventStore{}

	release := &db.Release{
		ID: "rel-new-001", BuildID: "build-new-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now(),
	}

	// Act
	err := releasesvc.ActivationKernel{
		Releases: releaseStore, EnvTargets: envTargetStore, Events: eventStore, DeployEvents: fakeDeployEventStore{}, Logger: testLogger,
	}.Activate(context.Background(), nil, release, nil, 10)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if releaseStore.getStatus("rel-new-001") != "ACTIVE" {
		t.Errorf("expected new release ACTIVE, got %q", releaseStore.getStatus("rel-new-001"))
	}
	if releaseStore.getStatus("rel-prev-001") != "SUPERSEDED" {
		t.Errorf("expected previous release SUPERSEDED, got %q", releaseStore.getStatus("rel-prev-001"))
	}
}

// --- TestActivationKernel_NoPreviousRelease ---
// First deploy: no previous active release. Supersede step should be skipped
// without error.

func TestActivationKernel_NoPreviousRelease(t *testing.T) {
	// Arrange
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-first-001", BuildID: "build-first-001",
		TenantID: "acme", ProjectID: "new-app", Environment: "staging",
		Status: "PENDING", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-first-001"] = "PENDING"
	// No active map entry → no previous release

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	eventStore := &fakeConvergenceEventStore{}

	release := &db.Release{
		ID: "rel-first-001", BuildID: "build-first-001",
		TenantID: "acme", ProjectID: "new-app", Environment: "staging",
		Status: "PENDING", CreatedAt: time.Now(),
	}

	// Act
	err := releasesvc.ActivationKernel{
		Releases: releaseStore, EnvTargets: envTargetStore, Events: eventStore, DeployEvents: fakeDeployEventStore{}, Logger: testLogger,
	}.Activate(context.Background(), nil, release, nil, 5)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if releaseStore.getStatus("rel-first-001") != "ACTIVE" {
		t.Errorf("expected release ACTIVE, got %q", releaseStore.getStatus("rel-first-001"))
	}
	if envTargetStore.getActiveCalls() != 1 {
		t.Errorf("expected SetActive called once, got %d", envTargetStore.getActiveCalls())
	}
}

// --- TestActivationKernel_AlreadyActive ---
// Release already ACTIVE (idempotent guard). Should skip without error.
// This test is complementary to the existing
// TestActivationKernel_SkipsAlreadyActiveRelease in release_convergence_loop_test.go,
// verifying no side effects at all.

func TestActivationKernel_AlreadyActive_NoSideEffects(t *testing.T) {
	// Arrange
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-idem-001", BuildID: "build-idem-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "ACTIVE", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-idem-001"] = "ACTIVE"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	eventStore := &fakeConvergenceEventStore{}

	release := &db.Release{
		ID: "rel-idem-001", BuildID: "build-idem-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now(),
	}

	// Act
	err := releasesvc.ActivationKernel{
		Releases: releaseStore, EnvTargets: envTargetStore, Events: eventStore, DeployEvents: fakeDeployEventStore{}, Logger: testLogger,
	}.Activate(context.Background(), nil, release, []releasesvc.ActivationAppInfo{{Name: "web", Status: "deployed"}}, 10)

	// Assert: idempotent skip
	if err != nil {
		t.Fatalf("expected nil error (idempotent), got %v", err)
	}
	if envTargetStore.getActiveCalls() != 0 {
		t.Errorf("expected SetActive NOT called, got %d calls", envTargetStore.getActiveCalls())
	}
	if len(eventStore.getEvents()) != 0 {
		t.Errorf("expected no events, got %v", eventStore.getEvents())
	}
}

// --- TestDeriveDeployID_Format ---
// deriveDeployID is unexported, but we can test it indirectly through the
// deploy.live event payload in ActivationKernel.Activate.

func TestDeriveDeployID_Format_ViaCompletedEvent(t *testing.T) {
	// Arrange: release ID "rel-8f3a2b1c" should produce deploy_id "d-8f3a2b1c"
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-8f3a2b1cdeadbeef", BuildID: "build-fmt-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "staging",
		Status: "PENDING", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-8f3a2b1cdeadbeef"] = "PENDING"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	eventStore := &fakeDeployIDEventStore{}

	release := &db.Release{
		ID: "rel-8f3a2b1cdeadbeef", BuildID: "build-fmt-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "staging",
		Status: "PENDING", CreatedAt: time.Now(),
	}

	// Act
	err := releasesvc.ActivationKernel{
		Releases: releaseStore, EnvTargets: envTargetStore, Events: eventStore, DeployEvents: fakeDeployEventStore{}, Logger: testLogger,
	}.Activate(context.Background(), nil, release, nil, 5)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check deploy.live event payload for deploy_id format
	for _, evt := range eventStore.getAll() {
		if evt.eventType == "deploy.live" {
			var payload map[string]interface{}
			if err := json.Unmarshal(evt.data, &payload); err != nil {
				t.Fatalf("failed to unmarshal deploy.live payload: %v", err)
			}
			deployID, ok := payload["deploy_id"].(string)
			if !ok {
				t.Fatal("deploy_id not found in deploy.live payload")
			}
			if deployID != "d-8f3a2b1c" {
				t.Errorf("expected deploy_id 'd-8f3a2b1c', got %q", deployID)
			}
			return
		}
	}
	t.Error("deploy.live event not found")
}

// --- TestReleaseAppsToActivationApps ---
// releaseAppsToActivationApps is unexported, but we test its behavior via
// ActivationKernel.Activate's deploy.completed event payload.

func TestReleaseAppsToActivationApps_ViaCompletedEvent(t *testing.T) {
	// Arrange
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-apps-001", BuildID: "build-apps-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "staging",
		Status: "PENDING", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-apps-001"] = "PENDING"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	eventStore := &fakeDeployIDEventStore{}

	release := &db.Release{
		ID: "rel-apps-001", BuildID: "build-apps-001",
		TenantID: "acme", ProjectID: "my-app", Environment: "staging",
		Status: "PENDING", CreatedAt: time.Now(),
	}

	apps := []releasesvc.ActivationAppInfo{
		{Name: "api", Status: "deployed", URL: "https://acme-my-app-api-staging.test.local"},
		{Name: "worker", Status: "deployed"}, // no URL for worker
	}

	// Act
	err := releasesvc.ActivationKernel{
		Releases: releaseStore, EnvTargets: envTargetStore, Events: eventStore, DeployEvents: fakeDeployEventStore{}, Logger: testLogger,
	}.Activate(context.Background(), nil, release, apps, 15)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, evt := range eventStore.getAll() {
		if evt.eventType == "deploy.completed" {
			var payload map[string]interface{}
			if err := json.Unmarshal(evt.data, &payload); err != nil {
				t.Fatalf("failed to unmarshal deploy.completed payload: %v", err)
			}
			appsRaw, ok := payload["apps"].([]interface{})
			if !ok {
				t.Fatal("apps not found in deploy.completed payload")
			}
			if len(appsRaw) != 2 {
				t.Fatalf("expected 2 apps, got %d", len(appsRaw))
			}
			// First app should have URL
			first := appsRaw[0].(map[string]interface{})
			if first["name"] != "api" {
				t.Errorf("expected first app name 'api', got %q", first["name"])
			}
			if first["url"] != "https://acme-my-app-api-staging.test.local" {
				t.Errorf("expected URL in first app, got %q", first["url"])
			}
			// Second app (worker) should NOT have URL
			second := appsRaw[1].(map[string]interface{})
			if _, hasURL := second["url"]; hasURL {
				t.Errorf("expected no URL for worker app, got %q", second["url"])
			}
			return
		}
	}
	t.Error("deploy.completed event not found")
}

// --- fakeDeployIDEventStore captures full event details for payload assertions ---

type fakeEventRecord struct {
	buildID   string
	eventType string
	data      json.RawMessage
}

type fakeDeployIDEventStore struct {
	events []fakeEventRecord
}

func (f *fakeDeployIDEventStore) Append(_ context.Context, buildID string, eventType string, data json.RawMessage) (int64, error) {
	f.events = append(f.events, fakeEventRecord{
		buildID:   buildID,
		eventType: eventType,
		data:      data,
	})
	return int64(len(f.events)), nil
}

func (f *fakeDeployIDEventStore) getAll() []fakeEventRecord {
	cp := make([]fakeEventRecord, len(f.events))
	copy(cp, f.events)
	return cp
}
