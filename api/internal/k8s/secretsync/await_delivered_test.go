// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secretsync

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/pkg/naming"
)

// A deploy waits until External Secrets has refreshed the environment's
// ExternalSecrets after the deploy asked, so its pods start with the values
// stored when it was requested (#119). These tests stand in for the operator
// by writing the status External Secrets v0.14 writes: status.refreshTime (the
// start of the reconcile), status.syncedResourceVersion (the generation and a
// hash of the labels and annotations it read) and the Ready condition.

// syncedExternalSecret is the ExternalSecret of app, as the operator left it
// after a refresh one minute ago.
func syncedExternalSecret(app string) *unstructured.Unstructured {
	name := naming.UserSecretName(shopTenant, shopProject, shopEnv, app)
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "external-secrets.io/v1beta1",
		"kind":       "ExternalSecret",
		"metadata": map[string]interface{}{
			"name": name, "namespace": shopNamespace,
			"labels": map[string]interface{}{"loopzero.io/managed-by": "loopzero-api"},
		},
		"spec": map[string]interface{}{
			"secretStoreRef": map[string]interface{}{"name": "loopzero-broker", "kind": "ClusterSecretStore"},
			"target":         map[string]interface{}{"name": name},
			"data":           []interface{}{map[string]interface{}{"secretKey": "API_KEY"}},
		},
		"status": operatorStatus(time.Now().Add(-time.Minute), "1-before", "True", "SecretSynced", "Secret was synced"),
	}}
}

func operatorStatus(refreshed time.Time, synced, ready, reason, message string) map[string]interface{} {
	return map[string]interface{}{
		"refreshTime":           refreshed.UTC().Format(time.RFC3339),
		"syncedResourceVersion": synced,
		"conditions": []interface{}{map[string]interface{}{
			"type": "Ready", "status": ready, "reason": reason, "message": message,
		}},
	}
}

// operator answers a forced sync the way External Secrets does, after delay.
type operator struct {
	dyn   *dynamicfake.FakeDynamicClient
	delay time.Duration
	// status is what the operator reports for a sync forced at forcedAt with
	// the annotation value token. Nil reports nothing.
	status func(forcedAt time.Time, token string) map[string]interface{}

	mu     sync.Mutex
	forced map[string]string // ExternalSecret name -> the last force-sync value seen
	wg     sync.WaitGroup
}

func runOperator(t *testing.T, dyn *dynamicfake.FakeDynamicClient, delay time.Duration,
	status func(forcedAt time.Time, token string) map[string]interface{}) *operator {
	t.Helper()
	op := &operator{dyn: dyn, delay: delay, status: status, forced: map[string]string{}}
	dyn.PrependReactor("update", "externalsecrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updated := action.(k8stesting.UpdateAction).GetObject().(*unstructured.Unstructured)
		token := updated.GetAnnotations()[forceSyncAnnotation]
		op.mu.Lock()
		seen := op.forced[updated.GetName()]
		op.forced[updated.GetName()] = token
		op.mu.Unlock()
		if token != "" && token != seen && op.status != nil {
			op.wg.Add(1)
			go op.report(updated.GetName(), token, time.Now())
		}
		return false, nil, nil
	})
	t.Cleanup(op.wg.Wait)
	return op
}

func (op *operator) report(name, token string, forcedAt time.Time) {
	defer op.wg.Done()
	time.Sleep(op.delay)
	client := op.dyn.Resource(externalSecretGVR).Namespace(shopNamespace)
	current, err := client.Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return
	}
	current.Object["status"] = op.status(forcedAt, token)
	_ = op.dyn.Tracker().Update(externalSecretGVR, current, shopNamespace)
}

func (op *operator) forcedSyncs() map[string]string {
	op.mu.Lock()
	defer op.mu.Unlock()
	forced := make(map[string]string, len(op.forced))
	for name, token := range op.forced {
		forced[name] = token
	}
	return forced
}

// refreshedAfterTheForce is a successful reconcile that began after the force.
func refreshedAfterTheForce(forcedAt time.Time, token string) map[string]interface{} {
	return operatorStatus(forcedAt.Add(time.Second), "1-"+token, "True", "SecretSynced", "Secret was synced")
}

func waitingManager(dyn *dynamicfake.FakeDynamicClient, timeout time.Duration) *SecretK8sManager {
	m := managerOn(dyn)
	m.deliveryTimeout = timeout
	m.deliveryPoll = 5 * time.Millisecond
	return m
}

func TestAwaitDelivered_ReturnsOnceEveryExternalSecretRefreshedAfterTheDeployAsked(t *testing.T) {
	dyn := newRuntimeFake(syncedExternalSecret("api"), syncedExternalSecret("worker"))
	const operatorDelay = 80 * time.Millisecond
	op := runOperator(t, dyn, operatorDelay, refreshedAfterTheForce)

	started := time.Now()
	err := waitingManager(dyn, 5*time.Second).AwaitDelivered(context.Background(), shopTenant, shopProject, shopEnv)

	if err != nil {
		t.Fatalf("AwaitDelivered: %v", err)
	}
	if waited := time.Since(started); waited < operatorDelay {
		t.Errorf("returned after %s, before the operator refreshed (%s)", waited, operatorDelay)
	}
	forced := op.forcedSyncs()
	for _, app := range []string{"api", "worker"} {
		if forced[naming.UserSecretName(shopTenant, shopProject, shopEnv, app)] == "" {
			t.Errorf("no sync was forced on the ExternalSecret of %s: %v", app, forced)
		}
	}
}

func TestAwaitDelivered_AnEnvironmentWithoutSecretsDoesNotWait(t *testing.T) {
	dyn := newRuntimeFake()

	started := time.Now()
	err := waitingManager(dyn, 5*time.Second).AwaitDelivered(context.Background(), shopTenant, shopProject, shopEnv)

	if err != nil {
		t.Fatalf("AwaitDelivered: %v", err)
	}
	if waited := time.Since(started); waited > time.Second {
		t.Errorf("waited %s for an environment with no secret", waited)
	}
	for _, action := range dyn.Actions() {
		if action.GetVerb() != "list" {
			t.Errorf("wrote to the cluster for an environment with no secret: %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
}

func notDelivered(t *testing.T, err error) *deploycontracts.SecretsNotDeliveredError {
	t.Helper()
	var undelivered *deploycontracts.SecretsNotDeliveredError
	if !errors.As(err, &undelivered) {
		t.Fatalf("err = %v, want a SecretsNotDeliveredError", err)
	}
	if undelivered.Store != "loopzero-broker" || undelivered.Namespace != shopNamespace ||
		undelivered.ExternalSecret != naming.UserSecretName(shopTenant, shopProject, shopEnv, "api") {
		t.Errorf("the error names store %q and %s/%s, want loopzero-broker and the ExternalSecret of api",
			undelivered.Store, undelivered.Namespace, undelivered.ExternalSecret)
	}
	if !strings.Contains(err.Error(), `"loopzero-broker"`) {
		t.Errorf("the message does not name the secret store: %s", err)
	}
	return undelivered
}

// Ready from the refresh of a minute ago says nothing about the values stored
// since: the deploy must not go on with it.
func TestAwaitDelivered_AnOperatorThatDoesNotRefreshTimesOutNamingTheStore(t *testing.T) {
	dyn := newRuntimeFake(syncedExternalSecret("api"))
	runOperator(t, dyn, 0, nil)

	err := waitingManager(dyn, 150*time.Millisecond).AwaitDelivered(context.Background(), shopTenant, shopProject, shopEnv)

	undelivered := notDelivered(t, err)
	if undelivered.Waited != 150*time.Millisecond {
		t.Errorf("Waited = %s, want the deadline", undelivered.Waited)
	}
	if !strings.Contains(undelivered.Reason, "no refresh") {
		t.Errorf("Reason = %q, want it to say no refresh followed the request", undelivered.Reason)
	}
}

func TestAwaitDelivered_ARefreshThatFailsTimesOutWithWhatTheOperatorReported(t *testing.T) {
	dyn := newRuntimeFake(syncedExternalSecret("api"))
	runOperator(t, dyn, 10*time.Millisecond, func(forcedAt time.Time, _ string) map[string]interface{} {
		// markAsFailed sets the condition and leaves refreshTime and
		// syncedResourceVersion as the last success wrote them.
		return operatorStatus(forcedAt.Add(-time.Minute), "1-before", "False", "SecretSyncedError", "could not get secret data from provider")
	})

	err := waitingManager(dyn, 150*time.Millisecond).AwaitDelivered(context.Background(), shopTenant, shopProject, shopEnv)

	if undelivered := notDelivered(t, err); !strings.Contains(undelivered.Reason, "could not get secret data from provider") {
		t.Errorf("Reason = %q, want what the operator reported", undelivered.Reason)
	}
}

// A reconcile that began before the deploy asked may have read the old values,
// even when it finishes afterwards and reports Ready with another
// syncedResourceVersion.
func TestAwaitDelivered_ARefreshThatBeganBeforeTheDeployAskedDoesNotCount(t *testing.T) {
	dyn := newRuntimeFake(syncedExternalSecret("api"))
	runOperator(t, dyn, 10*time.Millisecond, func(forcedAt time.Time, _ string) map[string]interface{} {
		return operatorStatus(forcedAt.Add(-10*time.Second), "1-in-flight", "True", "SecretSynced", "Secret was synced")
	})

	err := waitingManager(dyn, 150*time.Millisecond).AwaitDelivered(context.Background(), shopTenant, shopProject, shopEnv)

	if undelivered := notDelivered(t, err); !strings.Contains(undelivered.Reason, "began before") {
		t.Errorf("Reason = %q, want it to say the refresh began before the request", undelivered.Reason)
	}
}

// The request that waits may be cancelled: that is not the secret store's
// failure, and the deploy is tried again instead of failed.
func TestAwaitDelivered_ACancelledCallerIsNotASecretStoreFailure(t *testing.T) {
	dyn := newRuntimeFake(syncedExternalSecret("api"))
	runOperator(t, dyn, 0, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := waitingManager(dyn, 5*time.Second).AwaitDelivered(ctx, shopTenant, shopProject, shopEnv)

	var undelivered *deploycontracts.SecretsNotDeliveredError
	if err == nil || errors.As(err, &undelivered) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the caller's deadline and no SecretsNotDeliveredError", err)
	}
}

// This instance writes ExternalSecrets to one cell only (requireServedCell), so
// a tenant placed elsewhere has none it could wait for.
func TestAwaitDelivered_ATenantOnACellThisInstanceDoesNotServeDoesNotWait(t *testing.T) {
	dyn := newRuntimeFake(syncedExternalSecret("api"))
	m := waitingManager(dyn, 5*time.Second)
	m.tenantCells = fixedCell("cell-1")

	if err := m.AwaitDelivered(context.Background(), shopTenant, shopProject, shopEnv); err != nil {
		t.Fatalf("AwaitDelivered: %v", err)
	}
	if len(dyn.Actions()) != 0 {
		t.Errorf("touched a cluster that does not host the tenant: %v", dyn.Actions())
	}
}
