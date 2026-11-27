// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"context"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// undeliveredSecretsDelivery refuses every activation because the environment's
// secrets did not reach the cluster.
type undeliveredSecretsDelivery struct {
	deploycontracts.DeliveryActivator
	activations int
}

var errUndeliveredSecrets = &deploycontracts.SecretsNotDeliveredError{
	Store: "loop-runtime-broker", Namespace: "loop-runtime-tn-1-proj-1-production",
	ExternalSecret: "user-secrets-tn-1-proj-1-production-api", Reason: "no refresh after the deploy asked for one",
}

func (d *undeliveredSecretsDelivery) Activate(context.Context, deploycontracts.ActivationBuild, deploycontracts.BuildCallbackRequest) error {
	d.activations++
	return errUndeliveredSecrets
}

func undeliveredSecretsOrchestrator(status string) (*BuildOrchestrator, *fakeBuildStoreOrch, *fakeEventStoreContainer, *undeliveredSecretsDelivery) {
	builds := newFakeBuildStoreOrch()
	builds.builds["bld-1"] = &db.Build{
		ID: "bld-1", TenantID: "tn-1", ProjectID: "proj-1", Environment: "production", Status: status,
	}
	events := &fakeEventStoreContainer{}
	delivery := &undeliveredSecretsDelivery{}
	return NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: builds, ReleaseStore: newFakeReleaseStoreContainer(),
		EnvTargetStore: newFakeEnvTargetStoreContainer(), Locker: &fakeLockerContainer{},
		Workflow: &fakeWorkflowSubmitterOrch{}, EventStore: events,
		Sync: &fakeSyncContainer{}, GitOps: &fakeGitOpsContainer{}, Delivery: delivery,
	}), builds, events, delivery
}

var succeededCallback = deploycontracts.BuildCallbackRequest{
	Status: "succeeded", Apps: []deploycontracts.AppRelease{{Name: "api", Type: "server"}},
	Manifests: "apiVersion: argoproj.io/v1alpha1\nkind: Rollout",
}

// A deploy whose secrets did not reach the cluster ends failed, saying which
// secret store did not deliver. Left in RELEASING it would be retried for
// minutes and then fail with a message about attempts (#119).
func TestOnBuildCompleted_UndeliveredSecretsFailTheBuildNamingTheStore(t *testing.T) {
	orch, builds, events, _ := undeliveredSecretsOrchestrator("RUNNING")

	if err := orch.OnBuildCompleted(context.Background(), "bld-1", succeededCallback); err != nil {
		t.Fatalf("OnBuildCompleted: %v", err)
	}

	build := builds.builds["bld-1"]
	if build.Status != string(statemachine.BuildFailed) {
		t.Fatalf("build status = %s, want FAILED", build.Status)
	}
	if reason := builds.errorMessages["bld-1"]; !strings.Contains(reason, `"loop-runtime-broker"`) {
		t.Errorf("build error = %q, want it to name the secret store", reason)
	}
	failed := 0
	for _, event := range events.events {
		if event.EventType == "build.failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("%d build.failed event(s), want 1", failed)
	}
}

func TestRetryReleaseActivation_UndeliveredSecretsFailTheBuild(t *testing.T) {
	orch, builds, _, delivery := undeliveredSecretsOrchestrator("RELEASING")

	err := orch.RetryReleaseActivation(context.Background(), builds.builds["bld-1"], succeededCallback)

	if err == nil {
		t.Fatal("the retry reported success for a deploy that delivered nothing")
	}
	if delivery.activations != 1 {
		t.Errorf("%d activation(s), want 1", delivery.activations)
	}
	if got := builds.builds["bld-1"].Status; got != string(statemachine.BuildFailed) {
		t.Errorf("build status = %s, want FAILED", got)
	}
}
