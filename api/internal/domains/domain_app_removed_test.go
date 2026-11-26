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

// fakeDeployedApps answers which apps the running release of an environment has.
type fakeDeployedApps struct {
	apps  []string
	found bool
	err   error
}

func (f fakeDeployedApps) DeployedApps(context.Context, string, string, string) ([]string, bool, error) {
	return f.apps, f.found, f.err
}

func activeDomainOfApp(app string) ports.CustomDomain {
	return ports.CustomDomain{
		Domain: "shop.example.com", TenantID: "tn-1", ProjectName: "shop",
		AppName: app, Environment: "production", Status: "active",
	}
}

func checkerOver(store *fakeDomainHealthStore, apps DeployedAppsReader) *DomainHealthChecker {
	c := NewDomainHealthChecker(store, slog.Default(), DomainHealthCheckerConfig{Interval: time.Second})
	c.WithDeployedApps(apps)
	return c
}

// A deploy can replace the app a domain points to. The route of that domain is
// then no longer rendered and the host answers 404, so the domain cannot stay
// active.
func TestDomainHealthChecker_FailsAnActiveDomainWhoseAppIsNoLongerDeployed(t *testing.T) {
	store := newFakeDomainHealthStore()
	store.activeDomains = []ports.CustomDomain{activeDomainOfApp("app")}

	checkerOver(store, fakeDeployedApps{apps: []string{"web"}, found: true}).runChecks(context.Background())

	reason, failed := store.failedDomains["shop.example.com"]
	if !failed {
		t.Fatal("the domain of a removed app stayed active")
	}
	if !strings.Contains(reason, `"app"`) || !strings.Contains(reason, "production") {
		t.Errorf("the reason does not name the app and the environment: %q", reason)
	}
}

func TestDomainHealthChecker_KeepsAnActiveDomainWhoseAppIsDeployed(t *testing.T) {
	store := newFakeDomainHealthStore()
	store.activeDomains = []ports.CustomDomain{activeDomainOfApp("web")}

	checkerOver(store, fakeDeployedApps{apps: []string{"web", "worker"}, found: true}).runChecks(context.Background())

	if reason, failed := store.failedDomains["shop.example.com"]; failed {
		t.Fatalf("a domain whose app is deployed was failed: %q", reason)
	}
}

// With no running release, or when the lookup fails, there is nothing to
// compare the domain against. Failing it then would take down a working host.
func TestDomainHealthChecker_KeepsADomainItCannotCompare(t *testing.T) {
	cases := map[string]fakeDeployedApps{
		"no running release": {found: false},
		"lookup failed":      {err: errors.New("database is down")},
	}
	for name, apps := range cases {
		t.Run(name, func(t *testing.T) {
			store := newFakeDomainHealthStore()
			store.activeDomains = []ports.CustomDomain{activeDomainOfApp("app")}

			checkerOver(store, apps).runChecks(context.Background())

			if reason, failed := store.failedDomains["shop.example.com"]; failed {
				t.Fatalf("the domain was failed: %q", reason)
			}
		})
	}
}

// The provisioner used to mark a domain active after rendering a release that
// does not have the domain's app, which writes no route for it.
func TestDomainProvisioner_FailsADomainWhoseAppIsNotInTheRunningRelease(t *testing.T) {
	store := newFakeDomainProvisionerStore()
	store.verifiedDomains = []ports.CustomDomain{{
		ID: "dom-1", Domain: "shop.example.com", TenantID: "tn-1",
		ProjectName: "shop", AppName: "app", Environment: "production",
		Status: "provisioning",
	}}
	releaseStore := &fakeDomainReleaseReader{releases: map[string]*db.Release{
		"tn-1-shop-production": {
			ID: "rel-1", BuildID: "build-1", TenantID: "tn-1",
			ProjectID: "shop", Environment: "production",
			ImageDigest: strPtr(validImageDigest),
		},
	}}
	buildStore := &fakeDomainBuildReader{
		builds: map[string]*db.Build{"build-1": {ID: "build-1", CommitSHA: "abc123"}},
		apps:   map[string][]db.BuildAppSpec{"build-1": {{Name: "web", Rendering: "container"}}},
	}
	gitops := &fakeGitOpsProvisioner{}
	provisioner := NewDomainProvisioner(
		store, releaseStore, buildStore, gitops, &fakeLockerProvisioner{}, slog.Default(),
		DomainProvisionerConfig{
			Interval: time.Second, BaseDomain: "dev.getloopscale.com",
			RegistryURL: "registry.example.com", ClusterIssuer: "test-issuer",
		},
	)

	provisioner.ProcessOnce(context.Background())

	if gitops.pushCalled {
		t.Error("manifests were pushed for a domain with no app to route to")
	}
	reason, failed := store.updatedFailures["shop.example.com"]
	if !failed {
		t.Fatal("the domain was not failed")
	}
	if !strings.Contains(reason, `"app"`) {
		t.Errorf("the reason does not name the app: %q", reason)
	}
}

func TestDeployedAppsOfRelease_ListsTheAppsOfTheRunningRelease(t *testing.T) {
	reader := ReleaseDeployedApps{
		Releases: &fakeDomainReleaseReader{releases: map[string]*db.Release{
			"tn-1-shop-production": {ID: "rel-1", BuildID: "build-1"},
		}},
		Builds: &fakeDomainBuildReader{apps: map[string][]db.BuildAppSpec{
			"build-1": {{Name: "web"}, {Name: "worker"}},
		}},
	}

	apps, found, err := reader.DeployedApps(context.Background(), "tn-1", "shop", "production")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v; want the running release", found, err)
	}
	if strings.Join(apps, ",") != "web,worker" {
		t.Errorf("apps = %v; want web, worker", apps)
	}

	_, found, err = reader.DeployedApps(context.Background(), "tn-1", "shop", "preview")
	if err != nil || found {
		t.Errorf("found=%v err=%v for an environment with no release; want not found, no error", found, err)
	}
}
