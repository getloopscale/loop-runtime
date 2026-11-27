// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domains

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// TestDomainProvisionerRendersWithTheConfiguredDeliverySwitches pins that the
// manifests the provisioner pushes follow DomainProvisionerConfig — the
// composition root's Config — and not the API process environment.
func TestDomainProvisionerRendersWithTheConfiguredDeliverySwitches(t *testing.T) {
	for _, tc := range []struct {
		name        string
		progressive bool
		lean        bool
	}{
		{"progressive and lean", true, true},
		{"legacy deployment, default sizing", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LOOP_RUNTIME_PROGRESSIVE_DELIVERY", "false")
			t.Setenv("LOOP_RUNTIME_LEAN_RESOURCES", "true")
			store := newFakeDomainProvisionerStore()
			store.verifiedDomains = []ports.CustomDomain{{
				ID: "dom-1", Domain: "example.com", TenantID: "tn-1",
				ProjectName: "my-app", AppName: "api", Environment: "production",
				Status: "provisioning",
			}}
			releaseStore := &fakeDomainReleaseReader{releases: map[string]*db.Release{
				"tn-1-my-app-production": {
					ID: "rel-1", BuildID: "build-1", TenantID: "tn-1",
					ProjectID: "my-app", Environment: "production",
					ImageDigest: strPtr(validImageDigest),
				},
			}}
			buildStore := &fakeDomainBuildReader{
				builds: map[string]*db.Build{"build-1": {ID: "build-1", CommitSHA: "abc123"}},
				apps:   map[string][]db.BuildAppSpec{"build-1": {{Name: "api", Rendering: "container"}}},
			}
			gitops := &fakeGitOpsProvisioner{}
			provisioner := NewDomainProvisioner(store, releaseStore, buildStore, gitops, &fakeLockerProvisioner{},
				slog.Default(),
				DomainProvisionerConfig{
					Interval:            time.Second,
					BaseDomain:          "dev.getloopscale.com",
					RegistryURL:         "registry.example.com",
					ClusterIssuer:       "test-issuer",
					ProgressiveDelivery: tc.progressive,
					LeanResources:       tc.lean,
				})

			provisioner.ProcessOnce(context.Background())

			if !gitops.pushCalled {
				t.Fatal("expected the verified domain to be pushed")
			}
			if got := strings.Contains(gitops.lastManifests, "kind: Rollout"); got != tc.progressive {
				t.Errorf("pushed a Rollout = %v, want %v (ProgressiveDelivery=%v)", got, tc.progressive, tc.progressive)
			}
			if got := strings.Contains(gitops.lastManifests, "64Mi"); got != tc.lean {
				t.Errorf("pushed lean 64Mi memory request = %v, want %v (LeanResources=%v)", got, tc.lean, tc.lean)
			}
		})
	}
}
