// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domains

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// validImageDigest is a 64-hex sha256 string used in DomainProvisioner
// tests to satisfy the supply-chain VAP contract enforced by
// BuildManifestApps (CHG-2026-04-29-DOGFOOD iter 8).
const validImageDigest = "sha256:0eadbeefcafef00d1234567890abcdef0123456789abcdef0123456789abcdef"

// strPtr returns a pointer to its argument — keeps test fixtures
// readable when populating *string fields like db.Release.ImageDigest.
func strPtr(s string) *string { return &s }

// --- Test doubles for DomainProvisioner ---

type fakeDomainProvisionerStore struct {
	verifiedDomains   []ports.CustomDomain
	removingDomains   []ports.CustomDomain
	pendingNSDomains  []ports.CustomDomain
	nsActiveDomains   []ports.CustomDomain
	transitionedPairs []struct{ Domain, From, To string }
	updatedStatuses   map[string]string
	updatedFailures   map[string]string
	deletedDomains    []string
	cfZoneStatuses    map[string]string
	activeByApp       map[string][]ports.CustomDomain
}

func newFakeDomainProvisionerStore() *fakeDomainProvisionerStore {
	return &fakeDomainProvisionerStore{
		updatedStatuses: make(map[string]string),
		updatedFailures: make(map[string]string),
		cfZoneStatuses:  make(map[string]string),
		activeByApp:     make(map[string][]ports.CustomDomain),
	}
}

func (f *fakeDomainProvisionerStore) FindAndClaimVerified(_ context.Context) ([]ports.CustomDomain, error) {
	return f.verifiedDomains, nil
}
func (f *fakeDomainProvisionerStore) FindRemoving(_ context.Context) ([]ports.CustomDomain, error) {
	return f.removingDomains, nil
}
func (f *fakeDomainProvisionerStore) ListActiveByApp(_ context.Context, tenant, project, app, env string) ([]ports.CustomDomain, error) {
	key := tenant + "-" + project + "-" + app + "-" + env
	return f.activeByApp[key], nil
}
func (f *fakeDomainProvisionerStore) UpdateStatus(_ context.Context, domain, status string) error {
	f.updatedStatuses[domain] = status
	return nil
}
func (f *fakeDomainProvisionerStore) TransitionStatus(_ context.Context, domain, expected, newStatus string) error {
	f.transitionedPairs = append(f.transitionedPairs, struct{ Domain, From, To string }{domain, expected, newStatus})
	return nil
}
func (f *fakeDomainProvisionerStore) UpdateCFZoneStatus(_ context.Context, domain, status string) error {
	f.cfZoneStatuses[domain] = status
	return nil
}
func (f *fakeDomainProvisionerStore) UpdateFailure(_ context.Context, domain, status, reason string) error {
	f.updatedFailures[domain] = reason
	return nil
}
func (f *fakeDomainProvisionerStore) Delete(_ context.Context, domain string) error {
	f.deletedDomains = append(f.deletedDomains, domain)
	return nil
}
func (f *fakeDomainProvisionerStore) FindPendingNS(_ context.Context) ([]ports.CustomDomain, error) {
	return f.pendingNSDomains, nil
}
func (f *fakeDomainProvisionerStore) FindNSActive(_ context.Context) ([]ports.CustomDomain, error) {
	return f.nsActiveDomains, nil
}

type fakeDomainReleaseReader struct {
	releases map[string]*db.Release
}

func (f *fakeDomainReleaseReader) GetActiveByEnv(_ context.Context, tenant, project, env string) (*db.Release, error) {
	return f.releases[tenant+"-"+project+"-"+env], nil
}

type fakeDomainBuildReader struct {
	builds map[string]*db.Build
	apps   map[string][]db.BuildAppSpec
}

func (f *fakeDomainBuildReader) GetByID(_ context.Context, buildID string) (*db.Build, error) {
	return f.builds[buildID], nil
}
func (f *fakeDomainBuildReader) GetBuildApps(_ context.Context, buildID string) ([]db.BuildAppSpec, error) {
	return f.apps[buildID], nil
}

type fakeLockerProvisioner struct{}

func (f *fakeLockerProvisioner) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

type fakeGitOpsProvisioner struct {
	pushCalled    bool
	pushErr       error
	removeCalled  bool
	lastManifests string
}

func (f *fakeGitOpsProvisioner) Push(_ context.Context, opts deploycontracts.GitOpsPushOpts) (string, error) {
	f.pushCalled = true
	f.lastManifests = opts.Manifests
	return "abc123", f.pushErr
}
func (f *fakeGitOpsProvisioner) Remove(_ context.Context, _ deploycontracts.GitOpsRemoveOpts) (string, error) {
	f.removeCalled = true
	return "def456", nil
}

type fakeCFZoneChecker struct {
	statuses   map[string]string
	deleteErr  error
	triggerErr error
}

func (f *fakeCFZoneChecker) GetZoneStatus(_ context.Context, zoneID string) (string, error) {
	s, ok := f.statuses[zoneID]
	if !ok {
		return "pending", nil
	}
	return s, nil
}
func (f *fakeCFZoneChecker) TriggerActivationCheck(_ context.Context, _ string) error {
	return f.triggerErr
}
func (f *fakeCFZoneChecker) DeleteZone(_ context.Context, _ string) error {
	return f.deleteErr
}

// --- Tests ---

func TestDomainProvisioner_ProcessOnce_ProvisionVerifiedDomain(t *testing.T) {
	// Arrange
	store := newFakeDomainProvisionerStore()
	store.verifiedDomains = []ports.CustomDomain{
		{
			ID: "dom-1", Domain: "example.com", TenantID: "tn-1",
			ProjectName: "my-app", AppName: "api", Environment: "production",
			Status: "provisioning",
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
		builds: map[string]*db.Build{
			"build-1": {ID: "build-1", CommitSHA: "abc123"},
		},
		apps: map[string][]db.BuildAppSpec{
			"build-1": {{Name: "api", Rendering: "container"}},
		},
	}
	gitops := &fakeGitOpsProvisioner{}

	provisioner := NewDomainProvisioner(
		store, releaseStore, buildStore, gitops, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{
			Interval:      time.Second,
			BaseDomain:    "dev.getloopscale.com",
			RegistryURL:   "registry.example.com",
			ClusterIssuer: "test-issuer",
		},
	)

	// Act
	provisioner.ProcessOnce(context.Background())

	// Assert
	if !gitops.pushCalled {
		t.Error("expected gitops push to be called for verified domain")
	}
	// Should transition provisioning → active
	found := false
	for _, pair := range store.transitionedPairs {
		if pair.Domain == "example.com" && pair.From == "provisioning" && pair.To == "active" {
			found = true
		}
	}
	if !found {
		t.Error("expected domain to transition from provisioning to active")
	}
}

func TestDomainProvisioner_ProcessOnce_NoActiveRelease_Skips(t *testing.T) {
	// Arrange
	store := newFakeDomainProvisionerStore()
	store.verifiedDomains = []ports.CustomDomain{
		{
			ID: "dom-2", Domain: "no-release.com", TenantID: "tn-2",
			ProjectName: "app", AppName: "web", Environment: "staging",
		},
	}

	releaseStore := &fakeDomainReleaseReader{releases: map[string]*db.Release{}}
	gitops := &fakeGitOpsProvisioner{}

	provisioner := NewDomainProvisioner(
		store, releaseStore, &fakeDomainBuildReader{}, gitops, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, ClusterIssuer: "test-issuer"},
	)

	// Act
	provisioner.ProcessOnce(context.Background())

	// Assert: should skip (no gitops push, no transition)
	if gitops.pushCalled {
		t.Error("should not push when no active release")
	}
}

func TestDomainProvisioner_ProcessOnce_GitOpsFails_StaysInProvisioning(t *testing.T) {
	// Arrange
	store := newFakeDomainProvisionerStore()
	store.verifiedDomains = []ports.CustomDomain{
		{
			ID: "dom-3", Domain: "fail.com", TenantID: "tn-1",
			ProjectName: "my-app", AppName: "api", Environment: "production",
			Status: "provisioning",
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
		builds: map[string]*db.Build{"build-1": {ID: "build-1", CommitSHA: "abc"}},
		apps:   map[string][]db.BuildAppSpec{"build-1": {{Name: "api", Rendering: "container"}}},
	}
	gitops := &fakeGitOpsProvisioner{pushErr: fmt.Errorf("gitea unreachable")}

	provisioner := NewDomainProvisioner(
		store, releaseStore, buildStore, gitops, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, BaseDomain: "dev.getloopscale.com", RegistryURL: "reg.io", ClusterIssuer: "test-issuer"},
	)

	// Act
	provisioner.ProcessOnce(context.Background())

	// Assert: NO transition to active (stays in provisioning for retry)
	for _, pair := range store.transitionedPairs {
		if pair.Domain == "fail.com" && pair.To == "active" {
			t.Error("should NOT transition to active when gitops push fails")
		}
	}
}

func TestDomainProvisioner_ProcessOnce_RemovesDomain(t *testing.T) {
	// Arrange
	store := newFakeDomainProvisionerStore()
	store.removingDomains = []ports.CustomDomain{
		{
			ID: "dom-4", Domain: "old.com", TenantID: "tn-1",
			ProjectName: "my-app", AppName: "api", Environment: "production",
			Status: "removing",
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
		builds: map[string]*db.Build{"build-1": {ID: "build-1", CommitSHA: "abc"}},
		apps:   map[string][]db.BuildAppSpec{"build-1": {{Name: "api", Rendering: "container"}}},
	}
	gitops := &fakeGitOpsProvisioner{}

	provisioner := NewDomainProvisioner(
		store, releaseStore, buildStore, gitops, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, BaseDomain: "dev.getloopscale.com", RegistryURL: "reg.io", ClusterIssuer: "test-issuer"},
	)

	// Act
	provisioner.ProcessOnce(context.Background())

	// Assert
	if !gitops.pushCalled {
		t.Error("expected gitops push for domain removal")
	}
	if len(store.deletedDomains) != 1 || store.deletedDomains[0] != "old.com" {
		t.Errorf("expected old.com to be deleted, got %v", store.deletedDomains)
	}
}

func TestDomainProvisioner_ProcessOnce_RemoveNoRelease_DeletesDirectly(t *testing.T) {
	// Arrange
	store := newFakeDomainProvisionerStore()
	store.removingDomains = []ports.CustomDomain{
		{
			ID: "dom-5", Domain: "orphan.com", TenantID: "tn-2",
			ProjectName: "gone", AppName: "web", Environment: "staging",
			Status: "removing",
		},
	}

	releaseStore := &fakeDomainReleaseReader{releases: map[string]*db.Release{}}
	gitops := &fakeGitOpsProvisioner{}

	provisioner := NewDomainProvisioner(
		store, releaseStore, &fakeDomainBuildReader{}, gitops, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, ClusterIssuer: "test-issuer"},
	)

	// Act
	provisioner.ProcessOnce(context.Background())

	// Assert: no gitops push needed, just delete
	if gitops.pushCalled {
		t.Error("should not push gitops when no release exists")
	}
	if len(store.deletedDomains) != 1 || store.deletedDomains[0] != "orphan.com" {
		t.Errorf("expected orphan.com to be deleted, got %v", store.deletedDomains)
	}
}

func TestDomainProvisioner_PendingNS_ZoneActive_TransitionsToNSActive(t *testing.T) {
	// Arrange
	zoneID := "zone-123"
	store := newFakeDomainProvisionerStore()
	store.pendingNSDomains = []ports.CustomDomain{
		{
			ID: "dom-6", Domain: "cf-managed.com", TenantID: "tn-1",
			ProjectName: "app", AppName: "api", Environment: "production",
			CFZoneID: &zoneID, Status: "pending_ns",
			CreatedAt: time.Now(), // not timed out
		},
	}

	cfZone := &fakeCFZoneChecker{statuses: map[string]string{"zone-123": "active"}}

	provisioner := NewDomainProvisioner(
		store, &fakeDomainReleaseReader{}, &fakeDomainBuildReader{},
		&fakeGitOpsProvisioner{}, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, ClusterIssuer: "test-issuer"},
	)
	provisioner.WithCFZone(cfZone)

	// Act
	provisioner.ProcessOnce(context.Background())

	// Assert
	found := false
	for _, pair := range store.transitionedPairs {
		if pair.Domain == "cf-managed.com" && pair.From == "pending_ns" && pair.To == "ns_active" {
			found = true
		}
	}
	if !found {
		t.Error("expected transition from pending_ns to ns_active")
	}
	if store.cfZoneStatuses["cf-managed.com"] != "active" {
		t.Error("expected CF zone status to be updated to active")
	}
}

func TestDomainProvisioner_PendingNS_Timeout_FailsDomain(t *testing.T) {
	// Arrange
	zoneID := "zone-456"
	store := newFakeDomainProvisionerStore()
	store.pendingNSDomains = []ports.CustomDomain{
		{
			ID: "dom-7", Domain: "slow.com", TenantID: "tn-1",
			ProjectName: "app", AppName: "api", Environment: "production",
			CFZoneID: &zoneID, Status: "pending_ns",
			CreatedAt: time.Now().Add(-49 * time.Hour), // over 48h
		},
	}

	cfZone := &fakeCFZoneChecker{statuses: map[string]string{"zone-456": "pending"}}

	provisioner := NewDomainProvisioner(
		store, &fakeDomainReleaseReader{}, &fakeDomainBuildReader{},
		&fakeGitOpsProvisioner{}, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, ClusterIssuer: "test-issuer"},
	)
	provisioner.WithCFZone(cfZone)

	// Act
	provisioner.ProcessOnce(context.Background())

	// Assert
	if _, ok := store.updatedFailures["slow.com"]; !ok {
		t.Error("expected failure to be recorded for timed-out NS propagation")
	}
}

func TestDomainProvisioner_NoCFZone_SkipsPendingNS(t *testing.T) {
	// Arrange: no cfZone set (v1 mode)
	store := newFakeDomainProvisionerStore()
	store.pendingNSDomains = []ports.CustomDomain{
		{ID: "dom-8", Domain: "should-skip.com"},
	}

	provisioner := NewDomainProvisioner(
		store, &fakeDomainReleaseReader{}, &fakeDomainBuildReader{},
		&fakeGitOpsProvisioner{}, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, ClusterIssuer: "test-issuer"},
	)
	// No WithCFZone call

	// Act
	provisioner.ProcessOnce(context.Background())

	// Assert: no transitions (processPendingNS is a no-op without cfZone)
	if len(store.transitionedPairs) > 0 {
		t.Error("should not process pending NS without CF zone checker")
	}
}

func TestDomainProvisioner_RemoveCFManaged_DeletesZone(t *testing.T) {
	// Arrange
	zoneID := "zone-del"
	store := newFakeDomainProvisionerStore()
	store.removingDomains = []ports.CustomDomain{
		{
			ID: "dom-9", Domain: "cf-remove.com", TenantID: "tn-1",
			ProjectName: "app", AppName: "api", Environment: "production",
			CFZoneID: &zoneID, Status: "removing",
		},
	}

	// No active release - direct delete path
	releaseStore := &fakeDomainReleaseReader{releases: map[string]*db.Release{}}
	cfZone := &fakeCFZoneChecker{statuses: map[string]string{}}

	provisioner := NewDomainProvisioner(
		store, releaseStore, &fakeDomainBuildReader{},
		&fakeGitOpsProvisioner{}, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{Interval: time.Second, ClusterIssuer: "test-issuer"},
	)
	provisioner.WithCFZone(cfZone)

	// Act
	provisioner.ProcessOnce(context.Background())

	// Assert: domain deleted (no release = direct delete, but CF zone delete happens
	// only in the gitops path which requires a release)
	if len(store.deletedDomains) != 1 || store.deletedDomains[0] != "cf-remove.com" {
		t.Errorf("expected cf-remove.com deleted, got %v", store.deletedDomains)
	}
}

// TestDomainProvisioner_TriggerActivationCheckFails_IsRecorded pins the one
// silent failure site in this file. TriggerActivationCheck is fire-and-forget
// by design — no control flow depends on it and the next tick re-reads zone
// status regardless — but a discarded error reaches nobody, while the sibling
// Cloudflare call eighteen lines up reports its failure at Warn.
//
// The zone id assertion is what binds the record to the logger derived inside
// the loop (carrying domain and zone_id): asserting only the error string
// would pass on a record emitted through the bare provisioner logger, which
// loses both.
//
// Backlog item B-069.
func TestDomainProvisioner_TriggerActivationCheckFails_IsRecorded(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	zoneID := "zone-456"
	store := newFakeDomainProvisionerStore()
	store.pendingNSDomains = []ports.CustomDomain{
		{
			ID: "dom-9", Domain: "slow.com", TenantID: "tn-1",
			ProjectName: "app", AppName: "api", Environment: "production",
			CFZoneID: &zoneID, Status: "pending_ns",
			CreatedAt: time.Now(), // well inside the 48h window
		},
	}
	// "pending" (not "active") routes to the else-branch holding the discard.
	cfZone := &fakeCFZoneChecker{
		statuses:   map[string]string{zoneID: "pending"},
		triggerErr: errors.New("cloudflare: 429 too many requests"),
	}

	provisioner := NewDomainProvisioner(
		store, &fakeDomainReleaseReader{}, &fakeDomainBuildReader{},
		&fakeGitOpsProvisioner{}, &fakeLockerProvisioner{},
		logger,
		DomainProvisionerConfig{Interval: time.Second, ClusterIssuer: "test-issuer"},
	)
	provisioner.WithCFZone(cfZone)

	// Act
	provisioner.ProcessOnce(context.Background())

	// Assert: the failure reaches an operator...
	out := buf.String()
	if !strings.Contains(out, "429 too many requests") {
		t.Errorf("the discarded Cloudflare error must reach the log; got output: %q", out)
	}
	// ...on the logger that carries domain and zone_id.
	if !strings.Contains(out, zoneID) {
		t.Errorf("the record must be emitted on the logger carrying domain and zone_id; "+
			"got output: %q", out)
	}
}
