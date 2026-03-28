// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/services"
)

func TestParseArgoAppName(t *testing.T) {
	tests := []struct {
		input    string
		tenant   string
		env      string
		ok       bool
	}{
		{"tn-loopzero-production", "tn-loopzero", "production", true},
		{"acme-staging", "acme", "staging", true},
		{"my-tenant-with-dashes-production", "my-tenant-with-dashes", "production", true},
		{"", "", "", false},
		{"nohyphen", "", "", false},
		{"-leading", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tenant, env, ok := services.ParseArgoAppName(tt.input)
			if ok != tt.ok {
				t.Fatalf("ParseArgoAppName(%q) ok=%v, want %v", tt.input, ok, tt.ok)
			}
			if ok {
				if tenant != tt.tenant {
					t.Errorf("tenant=%q, want %q", tenant, tt.tenant)
				}
				if env != tt.env {
					t.Errorf("env=%q, want %q", env, tt.env)
				}
			}
		})
	}
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

	activator := services.NewSyncCallbackActivator(
		releaseStore, envTargetStore, locker, eventStore, testLogger,
	)

	// Act: simulate ArgoCD callback for tn-loopzero-production
	err := activator.OnSyncCompleted("tn-loopzero-production", "abc123")

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

	activator := services.NewSyncCallbackActivator(
		releaseStore, envTargetStore, locker, eventStore, testLogger,
	)

	// Act: callback with WRONG revision
	err := activator.OnSyncCompleted("tn-loopzero-production", "wrong-sha")

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

	activator := services.NewSyncCallbackActivator(
		releaseStore, envTargetStore, locker, eventStore, testLogger,
	)

	// Act: callback for DIFFERENT tenant
	err := activator.OnSyncCompleted("other-tenant-production", "abc123")

	// Assert: release stays PENDING
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if releaseStore.statuses["rel-cb-003"] != "PENDING" {
		t.Errorf("expected release to stay PENDING (wrong tenant), got %q", releaseStore.statuses["rel-cb-003"])
	}
}
