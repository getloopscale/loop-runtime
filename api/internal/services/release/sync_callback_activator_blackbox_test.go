// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release_test

import (
	"context"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/services/release"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/pkg/naming"
)

// labelledApps is an ArgoAppLabelReader answering each Application with the
// tenant and environment labels the tenant operator sets on it.
type labelledApps map[string][2]string // app name → {tenant, environment}

func (l labelledApps) GetAppLabels(_ context.Context, appName string) (map[string]string, error) {
	ids, ok := l[appName]
	if !ok {
		return nil, nil
	}
	return map[string]string{naming.LabelTenant: ids[0], naming.LabelEnvironment: ids[1]}, nil
}

var callbackTestApps = labelledApps{
	"tn-loopzero-my-app-production":   {"tn-loopzero", "production"},
	"other-tenant-my-app-production": {"other-tenant", "production"},
	"acme-my-app-production":         {"acme", "production"},
}

func TestSyncCallbackActivator_ActivatesPendingRelease(t *testing.T) {
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-cb-001", BuildID: "build-cb-001",
		TenantID: "tn-loopzero", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", GitCommit: "abc123", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-cb-001"] = "PENDING"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	locker := &fakeConvergenceLocker{}
	eventStore := &fakeConvergenceEventStore{}

	activator := release.NewSyncCallbackActivator(
		releaseStore, envTargetStore, locker, eventStore, "test.local", "", testLogger,
	).WithAppLabelReader(callbackTestApps)

	// Act: simulate the ArgoCD callback for the tenant's production application
	err := activator.OnSyncCompleted("tn-loopzero-my-app-production", "abc123")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if releaseStore.statuses["rel-cb-001"] != "ACTIVE" {
		t.Errorf("expected release ACTIVE, got %q", releaseStore.statuses["rel-cb-001"])
	}
	if envTargetStore.activeCalls == 0 {
		t.Error("expected SetActive to be called")
	}
}

func TestSyncCallbackActivator_SkipsMismatchedRevision(t *testing.T) {
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-cb-002", BuildID: "build-cb-002",
		TenantID: "tn-loopzero", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", GitCommit: "abc123", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-cb-002"] = "PENDING"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	locker := &fakeConvergenceLocker{}
	eventStore := &fakeConvergenceEventStore{}

	activator := release.NewSyncCallbackActivator(
		releaseStore, envTargetStore, locker, eventStore, "test.local", "", testLogger,
	).WithAppLabelReader(callbackTestApps)

	// Act: callback with WRONG revision
	err := activator.OnSyncCompleted("tn-loopzero-my-app-production", "wrong-sha")

	// Assert: release stays PENDING (revision mismatch)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if releaseStore.statuses["rel-cb-002"] != "PENDING" {
		t.Errorf("expected release to stay PENDING (revision mismatch), got %q", releaseStore.statuses["rel-cb-002"])
	}
}

func TestSyncCallbackActivator_IgnoresOtherTenants(t *testing.T) {
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-cb-003", BuildID: "build-cb-003",
		TenantID: "tn-loopzero", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-cb-003"] = "PENDING"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	locker := &fakeConvergenceLocker{}
	eventStore := &fakeConvergenceEventStore{}

	activator := release.NewSyncCallbackActivator(
		releaseStore, envTargetStore, locker, eventStore, "test.local", "", testLogger,
	).WithAppLabelReader(callbackTestApps)

	// Act: callback for DIFFERENT tenant
	err := activator.OnSyncCompleted("other-tenant-my-app-production", "abc123")

	// Assert: release stays PENDING
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if releaseStore.statuses["rel-cb-003"] != "PENDING" {
		t.Errorf("expected release to stay PENDING (wrong tenant), got %q", releaseStore.statuses["rel-cb-003"])
	}
}

// --- BUG-03 defense: release without GitCommit must be skipped ---

func TestSyncCallbackActivator_SkipsEmptyGitCommit(t *testing.T) {
	// Arrange: release with empty GitCommit (BUG-03 scenario)
	releaseStore := newFakeConvergenceReleaseStore()
	releaseStore.pending = []db.Release{{
		ID: "rel-cb-nocommit", BuildID: "build-cb-nocommit",
		TenantID: "tn-loopzero", ProjectID: "my-app", Environment: "production",
		Status: "PENDING", GitCommit: "", // Empty! BUG-03 defense
		CreatedAt: time.Now(),
	}}
	releaseStore.statuses["rel-cb-nocommit"] = "PENDING"

	envTargetStore := &fakeConvergenceEnvTargetStore{}
	locker := &fakeConvergenceLocker{}
	eventStore := &fakeConvergenceEventStore{}

	activator := release.NewSyncCallbackActivator(
		releaseStore, envTargetStore, locker, eventStore, "test.local", "", testLogger,
	).WithAppLabelReader(callbackTestApps)

	// Act: callback arrives — release should be skipped (deferred to convergence loop)
	err := activator.OnSyncCompleted("tn-loopzero-my-app-production", "abc123")

	// Assert: release stays PENDING, no activation
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if releaseStore.statuses["rel-cb-nocommit"] != "PENDING" {
		t.Errorf("expected release to stay PENDING (empty GitCommit), got %q", releaseStore.statuses["rel-cb-nocommit"])
	}
	if envTargetStore.getActiveCalls() != 0 {
		t.Errorf("expected SetActive NOT called (empty GitCommit skip), got %d calls", envTargetStore.getActiveCalls())
	}
}
