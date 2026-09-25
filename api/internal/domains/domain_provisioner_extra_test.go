// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domains

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

func TestDefaultDomainProvisionerConfig(t *testing.T) {
	cfg := DefaultDomainProvisionerConfig()
	if cfg.Interval != 30*time.Second {
		t.Errorf("expected 30s interval, got %v", cfg.Interval)
	}
}

func TestDomainProvisioner_NSActive_ProvisionsDomain(t *testing.T) {
	store := newFakeDomainProvisionerStore()
	store.nsActiveDomains = []ports.CustomDomain{
		{
			ID: "dom-ns", Domain: "ns-active.com", TenantID: "tn-1",
			ProjectName: "my-app", AppName: "api", Environment: "production",
			Status: "ns_active",
		},
	}

	releaseStore := &fakeDomainReleaseReader{
		releases: map[string]*db.Release{
			"tn-1-my-app-production": {
				ID: "rel-1", BuildID: "build-1", TenantID: "tn-1",
				ProjectID: "my-app", Environment: "production",
				ImageDigest: strPtr(validImageDigest),
			},
		},
	}
	buildStore := &fakeDomainBuildReader{
		builds: map[string]*db.Build{"build-1": {ID: "build-1", CommitSHA: "abc123"}},
		apps:   map[string][]db.BuildAppSpec{"build-1": {{Name: "api", Rendering: "container"}}},
	}
	gitops := &fakeGitOpsProvisioner{}
	cfZone := &fakeCFZoneChecker{statuses: map[string]string{}}

	provisioner := NewDomainProvisioner(
		store, releaseStore, buildStore, gitops, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{
			Interval:    time.Second,
			BaseDomain:  "dev.loopzero.example",
			RegistryURL: "registry.example.com", ClusterIssuer: "test-issuer"},
	)
	provisioner.WithCFZone(cfZone)

	provisioner.ProcessOnce(context.Background())

	// Should transition ns_active → provisioning (then provisioning → active via provisionDomain)
	foundNSTransition := false
	foundActiveTransition := false
	for _, pair := range store.transitionedPairs {
		if pair.Domain == "ns-active.com" && pair.From == "ns_active" && pair.To == "provisioning" {
			foundNSTransition = true
		}
		if pair.Domain == "ns-active.com" && pair.From == "provisioning" && pair.To == "active" {
			foundActiveTransition = true
		}
	}
	if !foundNSTransition {
		t.Error("expected ns_active → provisioning transition")
	}
	if !foundActiveTransition {
		t.Error("expected provisioning → active transition")
	}
	if !gitops.pushCalled {
		t.Error("expected gitops push for ns_active domain")
	}
}

func TestDomainProvisioner_NSActive_NoRelease_Skips(t *testing.T) {
	store := newFakeDomainProvisionerStore()
	store.nsActiveDomains = []ports.CustomDomain{
		{
			ID: "dom-ns2", Domain: "no-rel.com", TenantID: "tn-1",
			ProjectName: "app", AppName: "api", Environment: "staging",
			Status: "ns_active",
		},
	}

	releaseStore := &fakeDomainReleaseReader{releases: map[string]*db.Release{}}
	gitops := &fakeGitOpsProvisioner{}
	cfZone := &fakeCFZoneChecker{}

	provisioner := NewDomainProvisioner(
		store, releaseStore, &fakeDomainBuildReader{}, gitops, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, ClusterIssuer: "test-issuer"},
	)
	provisioner.WithCFZone(cfZone)

	provisioner.ProcessOnce(context.Background())

	if gitops.pushCalled {
		t.Error("should not push when no active release for ns_active domain")
	}
	// Should NOT transition
	for _, pair := range store.transitionedPairs {
		if pair.Domain == "no-rel.com" {
			t.Error("should not transition domain without active release")
		}
	}
}

func TestDomainProvisioner_PendingNS_StillPending_TriggersActivationCheck(t *testing.T) {
	zoneID := "zone-pending"
	store := newFakeDomainProvisionerStore()
	store.pendingNSDomains = []ports.CustomDomain{
		{
			ID: "dom-pns", Domain: "waiting.com", TenantID: "tn-1",
			ProjectName: "app", CFZoneID: &zoneID, Status: "pending_ns",
			CreatedAt: time.Now(), // not timed out
		},
	}

	cfZone := &fakeCFZoneChecker{statuses: map[string]string{"zone-pending": "pending"}}

	provisioner := NewDomainProvisioner(
		store, &fakeDomainReleaseReader{}, &fakeDomainBuildReader{},
		&fakeGitOpsProvisioner{}, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, ClusterIssuer: "test-issuer"},
	)
	provisioner.WithCFZone(cfZone)

	provisioner.ProcessOnce(context.Background())

	// Should NOT transition (still pending)
	for _, pair := range store.transitionedPairs {
		if pair.Domain == "waiting.com" && pair.To == "ns_active" {
			t.Error("should not transition to ns_active when zone is still pending")
		}
	}
}

func TestDomainProvisioner_PendingNS_NilZoneID_Skips(t *testing.T) {
	store := newFakeDomainProvisionerStore()
	store.pendingNSDomains = []ports.CustomDomain{
		{
			ID: "dom-nil", Domain: "nil-zone.com", TenantID: "tn-1",
			CFZoneID: nil, Status: "pending_ns",
			CreatedAt: time.Now(),
		},
	}

	cfZone := &fakeCFZoneChecker{}

	provisioner := NewDomainProvisioner(
		store, &fakeDomainReleaseReader{}, &fakeDomainBuildReader{},
		&fakeGitOpsProvisioner{}, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, ClusterIssuer: "test-issuer"},
	)
	provisioner.WithCFZone(cfZone)

	provisioner.ProcessOnce(context.Background())

	// Should not transition
	if len(store.transitionedPairs) > 0 {
		t.Error("should skip domains with nil zone ID")
	}
}

func TestDomainProvisioner_RemoveCFManaged_WithRelease_DeletesZone(t *testing.T) {
	zoneID := "zone-del-2"
	store := newFakeDomainProvisionerStore()
	store.removingDomains = []ports.CustomDomain{
		{
			ID: "dom-rm", Domain: "rm-cf.com", TenantID: "tn-1",
			ProjectName: "app", AppName: "api", Environment: "production",
			CFZoneID: &zoneID, Status: "removing",
		},
	}

	releaseStore := &fakeDomainReleaseReader{
		releases: map[string]*db.Release{
			"tn-1-app-production": {
				ID: "rel-1", BuildID: "build-1", TenantID: "tn-1",
				ProjectID: "app", Environment: "production",
				ImageDigest: strPtr(validImageDigest),
			},
		},
	}
	buildStore := &fakeDomainBuildReader{
		builds: map[string]*db.Build{"build-1": {ID: "build-1", CommitSHA: "abc"}},
		apps:   map[string][]db.BuildAppSpec{"build-1": {{Name: "api", Rendering: "container"}}},
	}
	gitops := &fakeGitOpsProvisioner{}
	cfZone := &fakeCFZoneChecker{}

	provisioner := NewDomainProvisioner(
		store, releaseStore, buildStore, gitops, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, BaseDomain: "dev.loopzero.example", RegistryURL: "reg.io", ClusterIssuer: "test-issuer"},
	)
	provisioner.WithCFZone(cfZone)

	provisioner.ProcessOnce(context.Background())

	if !gitops.pushCalled {
		t.Error("expected gitops push for domain removal with active release")
	}
	if len(store.deletedDomains) != 1 || store.deletedDomains[0] != "rm-cf.com" {
		t.Errorf("expected rm-cf.com deleted, got %v", store.deletedDomains)
	}
}
