// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// A pod reads envFrom once, when it starts. A deploy that follows a secret
// change by a few seconds used to roll the pods before External Secrets wrote
// the new value into the cluster, and nothing restarted them afterwards (#119).

// stepLog records the order of what a delivery did.
type stepLog struct{ steps []string }

// orderedSecrets is the environment's secrets, confirmed or not.
type orderedSecrets struct {
	log    *stepLog
	err    error
	asked  []string
	called int
}

func (s *orderedSecrets) AwaitDelivered(_ context.Context, tenantID, projectID, environment string) error {
	s.called++
	s.asked = []string{tenantID, projectID, environment}
	s.log.steps = append(s.log.steps, "secrets confirmed in the cluster")
	return s.err
}

// orderedGitOps is a GitOps repository that records when manifests reach it.
type orderedGitOps struct {
	fakeGitOpsContainer
	log *stepLog
}

func (g *orderedGitOps) Push(ctx context.Context, opts deploycontracts.GitOpsPushOpts) (string, error) {
	g.log.steps = append(g.log.steps, "manifests pushed")
	return g.fakeGitOpsContainer.Push(ctx, opts)
}

var secretsBuild = deploycontracts.ActivationBuild{
	ID: "build-1", TenantID: "tn-1", ProjectID: "shop", Environment: "production", CreatedBy: "user-1",
}

var secretsResult = deploycontracts.BuildCallbackRequest{
	Status: "succeeded", Apps: []deploycontracts.AppRelease{{Name: "api", Type: "server"}},
	Manifests: "apiVersion: argoproj.io/v1alpha1\nkind: Rollout",
}

func secretsActivator(log *stepLog, secrets deploycontracts.EnvironmentSecrets) (*ContainerDeliveryActivator, *fakeReleaseStoreContainer, *fakeSyncContainer) {
	releases := newFakeReleaseStoreContainer()
	sync := &fakeSyncContainer{}
	return NewContainerDeliveryActivator(ContainerDeliveryDeps{
		ReleaseStore: releases, EnvTargetStore: newFakeEnvTargetStoreContainer(),
		EventStore: &fakeEventStoreContainer{}, Sync: sync, Locker: &fakeLockerContainer{},
		GitOps:  &orderedGitOps{fakeGitOpsContainer: fakeGitOpsContainer{commitSHA: "sha"}, log: log},
		Secrets: secrets,
	}), releases, sync
}

func TestActivate_DeliversTheRolloutOnlyAfterTheSecretsAreInTheCluster(t *testing.T) {
	log := &stepLog{}
	secrets := &orderedSecrets{log: log}
	activator, _, sync := secretsActivator(log, secrets)

	if err := activator.Activate(context.Background(), secretsBuild, secretsResult); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	want := []string{"secrets confirmed in the cluster", "manifests pushed"}
	if !reflect.DeepEqual(log.steps, want) {
		t.Errorf("order = %v, want %v", log.steps, want)
	}
	if !reflect.DeepEqual(secrets.asked, []string{"tn-1", "shop", "production"}) {
		t.Errorf("waited for the secrets of %v, want the deploy's environment", secrets.asked)
	}
	if !sync.triggered {
		t.Error("the sync was not triggered after the secrets were confirmed")
	}
}

func TestActivate_UndeliveredSecretsStopTheDeployBeforeAnyPodIsReplaced(t *testing.T) {
	log := &stepLog{}
	undelivered := &deploycontracts.SecretsNotDeliveredError{
		Store: "loopruntime-broker", Namespace: "loopruntime-tn-1-shop-production",
		ExternalSecret: "user-secrets-tn-1-shop-production-api", Reason: "no refresh after the deploy asked for one",
	}
	activator, releases, sync := secretsActivator(log, &orderedSecrets{log: log, err: undelivered})

	err := activator.Activate(context.Background(), secretsBuild, secretsResult)

	var got *deploycontracts.SecretsNotDeliveredError
	if !errors.As(err, &got) || got.Store != "loopruntime-broker" {
		t.Fatalf("err = %v, want a SecretsNotDeliveredError naming the store", err)
	}
	if errors.Is(err, deploycontracts.ErrActivationAlreadyHandled) {
		t.Error("the failure is reported as already handled, so nobody would fail the build")
	}
	if !reflect.DeepEqual(log.steps, []string{"secrets confirmed in the cluster"}) {
		t.Errorf("steps = %v: manifests were pushed with stale secrets in the cluster", log.steps)
	}
	if sync.triggered {
		t.Error("the sync was triggered with stale secrets in the cluster")
	}
	if len(releases.releases) != 0 {
		t.Errorf("%d release(s) created for a deploy that delivered nothing", len(releases.releases))
	}
}

// A rollback does not restart pods for a secret: it must not wait for them, nor
// fail because the secret store is down.
func TestRollback_DoesNotWaitForSecrets(t *testing.T) {
	secrets := &orderedSecrets{log: &stepLog{}, err: errors.New("the secret store is down")}
	activator := NewContainerDeliveryActivator(ContainerDeliveryDeps{
		Sync: &fakeSyncContainer{}, GitOps: &fakeGitOpsContainer{commitSHA: "sha"},
		GitReader: &fakeGitReaderContainer{manifests: []byte("m")}, Locker: &fakeLockerContainer{},
		Secrets: secrets,
	})

	if err := activator.Rollback(context.Background(), rollbackDeliveryRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if secrets.called != 0 {
		t.Errorf("the rollback asked for the secrets %d time(s)", secrets.called)
	}
}
