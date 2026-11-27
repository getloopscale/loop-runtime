// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domains

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// latestReleases stands for the release store's newest-first list.
type latestReleases struct {
	releases []db.Release
	err      error
}

func (l latestReleases) ListByEnv(_ context.Context, _, _, _ string, limit int) ([]db.Release, error) {
	if l.err != nil {
		return nil, l.err
	}
	return l.releases[:min(limit, len(l.releases))], nil
}

// A deploy writes its manifest to the GitOps repository before its release is
// the active one. A provisioner that renders the active release in that window
// writes the previous build back, and the deploy never reaches the cell.
func TestDomainProvisioner_LeavesTheManifestOfAReleaseBeingDelivered(t *testing.T) {
	for _, releaseStatus := range []string{"READY", "PENDING"} {
		for _, domainStatus := range []string{"provisioning", "removing"} {
			t.Run(releaseStatus+"/"+domainStatus, func(t *testing.T) {
				provisioner, gitops, store := provisionerForDomain(domainInStatus(domainStatus))
				provisioner.WithLatestReleases(latestReleases{releases: []db.Release{
					{ID: "rel-2", BuildID: "build-2", Status: releaseStatus},
					{ID: "rel-1", BuildID: "build-1", Status: "ACTIVE"},
				}})

				provisioner.ProcessOnce(context.Background())

				if gitops.pushCalled {
					t.Fatal("pushed the running release over the manifest of the release being delivered")
				}
				if len(store.deletedDomains) != 0 {
					t.Error("the row was deleted although its host is still in the manifest")
				}
				if len(store.updatedFailures) != 0 {
					t.Errorf("the domain was failed for a deploy in progress: %v", store.updatedFailures)
				}
				for _, moved := range store.transitionedPairs {
					if moved.To == "active" {
						t.Error("the domain is active although nothing was pushed")
					}
				}
			})
		}
	}
}

func TestDomainProvisioner_PushesOnceTheNewestReleaseIsTheRunningOne(t *testing.T) {
	provisioner, gitops, _ := provisionerForDomain(domainInStatus("provisioning"))
	provisioner.WithLatestReleases(latestReleases{releases: []db.Release{
		{ID: "rel-1", BuildID: "build-1", Status: "ACTIVE"},
	}})

	provisioner.ProcessOnce(context.Background())

	if !gitops.pushCalled {
		t.Fatal("expected a push: no release is being delivered")
	}
}

// A release that stopped before it ran (failed sync, cancelled) holds nothing
// back: the environment keeps the running release, and its domains follow it.
func TestDomainProvisioner_AReleaseThatStoppedDoesNotHoldDomainsBack(t *testing.T) {
	provisioner, gitops, _ := provisionerForDomain(domainInStatus("removing"))
	provisioner.WithLatestReleases(latestReleases{releases: []db.Release{
		{ID: "rel-2", BuildID: "build-2", Status: "SYNC_FAILED"},
	}})

	provisioner.ProcessOnce(context.Background())

	if !gitops.pushCalled {
		t.Fatal("expected a push: the newest release is not being delivered")
	}
}

func TestDomainProvisioner_PushesNothingWhenTheNewestReleaseCannotBeRead(t *testing.T) {
	provisioner, gitops, store := provisionerForDomain(domainInStatus("provisioning"))
	provisioner.WithLatestReleases(latestReleases{err: errors.New("connection refused")})

	provisioner.ProcessOnce(context.Background())

	if gitops.pushCalled {
		t.Fatal("pushed without knowing whether a release is being delivered")
	}
	if len(store.updatedFailures) != 0 {
		t.Errorf("the domain was failed for a read error: %v", store.updatedFailures)
	}
}

// heldLock records whether the environment lock is held.
type heldLock struct{ held bool }

func (l *heldLock) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	l.held = true
	defer func() { l.held = false }()
	return fn(nil)
}

type pushUnderLock struct {
	lock        *heldLock
	pushes      int
	outsideLock int
}

func (p *pushUnderLock) Push(_ context.Context, _ deploycontracts.GitOpsPushOpts) (string, error) {
	p.pushes++
	if !p.lock.held {
		p.outsideLock++
	}
	return "abc123", nil
}

func (p *pushUnderLock) Remove(_ context.Context, _ deploycontracts.GitOpsRemoveOpts) (string, error) {
	return "", nil
}

// A deploy creates its release under the environment lock. A push made after
// the lock is released can land after that deploy's push.
func TestDomainProvisioner_PushesWhileItHoldsTheEnvironmentLock(t *testing.T) {
	for _, status := range []string{"provisioning", "removing"} {
		t.Run(status, func(t *testing.T) {
			store := newFakeDomainProvisionerStore()
			if status == "removing" {
				store.removingDomains = []ports.CustomDomain{domainInStatus(status)}
			} else {
				store.verifiedDomains = []ports.CustomDomain{domainInStatus(status)}
			}
			releases := &fakeDomainReleaseReader{releases: map[string]*db.Release{
				"tn-1-my-app-production": {
					ID: "rel-1", BuildID: "build-1", TenantID: "tn-1", ProjectID: "my-app",
					Environment: "production", ImageDigest: strPtr(validImageDigest),
				},
			}}
			builds := &fakeDomainBuildReader{
				builds: map[string]*db.Build{"build-1": {ID: "build-1", CommitSHA: "abc123"}},
				apps:   map[string][]db.BuildAppSpec{"build-1": {{Name: "api", Rendering: "container", Replicas: 1}}},
			}
			lock := &heldLock{}
			gitops := &pushUnderLock{lock: lock}
			provisioner := NewDomainProvisioner(store, releases, builds, gitops, lock, slog.Default(),
				DomainProvisionerConfig{
					Interval: time.Second, BaseDomain: "dev.getloopscale.com",
					RegistryURL: "registry.example.com", ClusterIssuer: "test-issuer",
				})

			provisioner.ProcessOnce(context.Background())

			if gitops.pushes != 1 {
				t.Fatalf("pushes = %d, want 1", gitops.pushes)
			}
			if gitops.outsideLock != 0 {
				t.Fatal("the push ran after the environment lock was released")
			}
		})
	}
}

// Argo CD refuses a sync while another runs. The host is then in the GitOps
// repository and not on the cell, and nothing else starts that sync.
func TestDomainProvisioner_ProvisionsAgainADomainThatStayedProvisioning(t *testing.T) {
	provisioner, gitops, store := provisionerForDomain(domainInStatus("provisioning"))
	store.staleProvisioning = store.verifiedDomains
	store.verifiedDomains = nil
	sync := &recordingSync{}
	provisioner.WithSync(sync)

	provisioner.ProcessOnce(context.Background())

	if !gitops.pushCalled || len(sync.apps) != 1 {
		t.Fatalf("pushed=%v synced=%v; want the domain pushed and synced again", gitops.pushCalled, sync.apps)
	}
	if store.staleAge != time.Second {
		t.Errorf("asked for domains provisioning for %s; want one interval (1s), so a domain claimed in this cycle is not taken twice", store.staleAge)
	}
	active := false
	for _, moved := range store.transitionedPairs {
		active = active || (moved.From == "provisioning" && moved.To == "active")
	}
	if !active {
		t.Fatalf("transitions = %v; want provisioning to active", store.transitionedPairs)
	}
}
