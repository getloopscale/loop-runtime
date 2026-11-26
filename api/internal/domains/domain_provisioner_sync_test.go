// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domains

import (
	"context"
	"errors"
	"testing"
)

// recordingSync stands for the Argo CD sync client.
type recordingSync struct {
	apps []string
	err  error
}

func (r *recordingSync) TriggerSync(_ context.Context, appName string) error {
	r.apps = append(r.apps, appName)
	return r.err
}

// The Application of an environment has no automated sync: a push alone
// changes nothing on the cell.
func TestDomainProvisioner_SyncsTheEnvironmentAfterThePush(t *testing.T) {
	for _, status := range []string{"provisioning", "removing"} {
		t.Run(status, func(t *testing.T) {
			provisioner, gitops, _ := provisionerForDomain(domainInStatus(status))
			sync := &recordingSync{}
			provisioner.WithSync(sync)

			provisioner.ProcessOnce(context.Background())

			if !gitops.pushCalled {
				t.Fatal("expected a push")
			}
			if len(sync.apps) != 1 || sync.apps[0] != "tn-1-my-app-production" {
				t.Fatalf("synced %v; want the environment's Application once", sync.apps)
			}
		})
	}
}

func TestDomainProvisioner_ADomainIsNotActiveUntilItsSyncStarted(t *testing.T) {
	provisioner, _, store := provisionerForDomain(domainInStatus("provisioning"))
	provisioner.WithSync(&recordingSync{err: errors.New("argocd down")})

	provisioner.ProcessOnce(context.Background())

	for _, moved := range store.transitionedPairs {
		if moved.To == "active" {
			t.Fatal("the domain is active although its route never reached the cell")
		}
	}
}

func TestDomainProvisioner_ARemovedDomainKeepsItsRowUntilItsSyncStarted(t *testing.T) {
	provisioner, _, store := provisionerForDomain(domainInStatus("removing"))
	provisioner.WithSync(&recordingSync{err: errors.New("argocd down")})

	provisioner.ProcessOnce(context.Background())

	if len(store.deletedDomains) != 0 {
		t.Fatal("the row was deleted although the route is still on the cell")
	}
}
