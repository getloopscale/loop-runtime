// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domains

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// fixedScale stands for scaling.StoredScaling with a saved fixed scale.
type fixedScale struct {
	replicas int
	err      error
}

func (f fixedScale) Apply(_ context.Context, _, _, _ string, apps []db.BuildAppSpec) ([]db.BuildAppSpec, error) {
	if f.err != nil {
		return nil, f.err
	}
	scaled := append([]db.BuildAppSpec(nil), apps...)
	for i := range scaled {
		scaled[i].Replicas = f.replicas
	}
	return scaled, nil
}

// provisionerForDomain returns a provisioner whose environment runs a release
// built with one replica of app "api", with d waiting to be processed.
func provisionerForDomain(d ports.CustomDomain) (*DomainProvisioner, *fakeGitOpsProvisioner, *fakeDomainProvisionerStore) {
	store := newFakeDomainProvisionerStore()
	if d.Status == "removing" {
		store.removingDomains = []ports.CustomDomain{d}
	} else {
		store.verifiedDomains = []ports.CustomDomain{d}
	}
	releaseStore := &fakeDomainReleaseReader{releases: map[string]*db.Release{
		"tn-1-my-app-production": {
			ID: "rel-1", BuildID: "build-1", TenantID: "tn-1",
			ProjectID: "my-app", Environment: "production",
			ImageDigest: strPtr(validImageDigest),
		},
	}}
	buildStore := &fakeDomainBuildReader{
		builds: map[string]*db.Build{"build-1": {ID: "build-1", CommitSHA: "abc123"}},
		apps:   map[string][]db.BuildAppSpec{"build-1": {{Name: "api", Rendering: "container", Replicas: 1}}},
	}
	gitops := &fakeGitOpsProvisioner{}
	provisioner := NewDomainProvisioner(store, releaseStore, buildStore, gitops, &fakeLockerProvisioner{},
		slog.Default(),
		DomainProvisionerConfig{
			Interval: time.Second, BaseDomain: "dev.getloopscale.com",
			RegistryURL: "registry.example.com", ClusterIssuer: "test-issuer",
		})
	return provisioner, gitops, store
}

func domainInStatus(status string) ports.CustomDomain {
	return ports.CustomDomain{
		ID: "dom-1", Domain: "example.com", TenantID: "tn-1",
		ProjectName: "my-app", AppName: "api", Environment: "production", Status: status,
	}
}

func TestDomainProvisioner_RendersTheSavedScaleNotTheBuilds(t *testing.T) {
	for _, status := range []string{"provisioning", "removing"} {
		t.Run(status, func(t *testing.T) {
			provisioner, gitops, _ := provisionerForDomain(domainInStatus(status))
			provisioner.WithSavedScaling(fixedScale{replicas: 5})

			provisioner.ProcessOnce(context.Background())

			if !gitops.pushCalled {
				t.Fatal("expected a push")
			}
			if !strings.Contains(gitops.lastManifests, "replicas: 5") {
				t.Errorf("the pushed manifests do not carry the saved 5 replicas:\n%s", gitops.lastManifests)
			}
		})
	}
}

func TestDomainProvisioner_PushesNothingWhenTheSavedScaleCannotBeRead(t *testing.T) {
	provisioner, gitops, _ := provisionerForDomain(domainInStatus("provisioning"))
	provisioner.WithSavedScaling(fixedScale{err: errors.New("db down")})

	provisioner.ProcessOnce(context.Background())

	if gitops.pushCalled {
		t.Fatal("pushed manifests with the build's replicas after failing to read the saved scale")
	}
}
