// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secretsync

import (
	"context"
	"reflect"
	"sort"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

const (
	shopTenant    = "tn-1"
	shopProject   = "shop"
	shopEnv       = "production"
	shopNamespace = "loopzero-tn-1-shop-production"
)

func shopSecret(app, name string) ports.SecretRef {
	return ports.SecretRef{TenantID: shopTenant, Project: shopProject, Environment: shopEnv, App: app, Name: name}
}

// newRuntimeFake is a runtime cluster that can list ExternalSecrets and already
// holds objs.
func newRuntimeFake(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{externalSecretGVR: "ExternalSecretList"}, objs...)
}

func managerOn(dyn *dynamicfake.FakeDynamicClient) *SecretK8sManager {
	return &SecretK8sManager{
		mgmtClient: fake.NewClientset(), runtimeDynamic: dyn, mgmtNamespace: "loopzero-system",
		tenantCells: fixedCell("cell-0"), runtimeCellID: "cell-0",
	}
}

// perSecretExternalSecret is the object earlier versions wrote: one
// ExternalSecret per secret, named es-<app>-<name>, all with the same target.
func perSecretExternalSecret(app, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "external-secrets.io/v1beta1",
		"kind":       "ExternalSecret",
		"metadata": map[string]interface{}{
			"name":      externalSecretName(app, name),
			"namespace": shopNamespace,
			"labels":    map[string]interface{}{"loopzero.io/managed-by": "loopzero-api"},
		},
		"spec": map[string]interface{}{
			"target": map[string]interface{}{
				"name":           naming.UserSecretName(shopTenant, shopProject, shopEnv, app),
				"creationPolicy": "Owner",
			},
			"data": []interface{}{
				map[string]interface{}{"secretKey": name},
			},
		},
	}}
}

func externalSecretsIn(t *testing.T, dyn *dynamicfake.FakeDynamicClient) map[string]*unstructured.Unstructured {
	t.Helper()
	list, err := dyn.Resource(externalSecretGVR).Namespace(shopNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list ExternalSecrets: %v", err)
	}
	byName := make(map[string]*unstructured.Unstructured, len(list.Items))
	for i := range list.Items {
		byName[list.Items[i].GetName()] = &list.Items[i]
	}
	return byName
}

func namesOf(objects map[string]*unstructured.Unstructured) []string {
	names := make([]string, 0, len(objects))
	for name := range objects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func secretKeysOf(t *testing.T, es *unstructured.Unstructured) []string {
	t.Helper()
	data, _, err := unstructured.NestedSlice(es.Object, "spec", "data")
	if err != nil {
		t.Fatalf("spec.data of %s: %v", es.GetName(), err)
	}
	keys := make([]string, 0, len(data))
	for _, raw := range data {
		entry, _ := raw.(map[string]interface{})
		key, _, _ := unstructured.NestedString(entry, "secretKey")
		keys = append(keys, key)
	}
	return keys
}

// External Secrets lets one ExternalSecret own a target Secret. Every secret of
// an app is delivered into the same Secret, so they must all be entries of the
// one ExternalSecret that owns it: a second object never syncs.
func TestEverySecretOfAnAppIsAnEntryOfOneExternalSecret(t *testing.T) {
	// Arrange
	dyn := newRuntimeFake()
	mgr := managerOn(dyn)

	// Act
	for _, name := range []string{"PROBE_SECRET", "SECOND_SECRET"} {
		if err := mgr.CreateSecret(context.Background(), shopSecret("api", name), "v"); err != nil {
			t.Fatalf("CreateSecret %s: %v", name, err)
		}
	}

	// Assert
	target := naming.UserSecretName(shopTenant, shopProject, shopEnv, "api")
	objects := externalSecretsIn(t, dyn)
	if got := namesOf(objects); !reflect.DeepEqual(got, []string{target}) {
		t.Fatalf("ExternalSecrets = %v, want only %q: a second owner of the target never syncs", got, target)
	}
	es := objects[target]
	if got, want := secretKeysOf(t, es), []string{"PROBE_SECRET", "SECOND_SECRET"}; !reflect.DeepEqual(got, want) {
		t.Errorf("spec.data keys = %v, want %v", got, want)
	}
	if got, _, _ := unstructured.NestedString(es.Object, "spec", "target", "name"); got != target {
		t.Errorf("spec.target.name = %q, want %q", got, target)
	}
	if got, _, _ := unstructured.NestedString(es.Object, "spec", "target", "creationPolicy"); got != "Owner" {
		t.Errorf("spec.target.creationPolicy = %q, want Owner so a removed entry leaves the Secret", got)
	}
	if got, _, _ := unstructured.NestedString(es.Object, "spec", "secretStoreRef", "name"); got != "loopzero-broker" {
		t.Errorf("spec.secretStoreRef.name = %q, want loopzero-broker", got)
	}
	data, _, _ := unstructured.NestedSlice(es.Object, "spec", "data")
	for _, raw := range data {
		entry, _ := raw.(map[string]interface{})
		key, _, _ := unstructured.NestedString(entry, "secretKey")
		if got, _, _ := unstructured.NestedString(entry, "remoteRef", "decodingStrategy"); got != "Base64" {
			t.Errorf("%s: remoteRef.decodingStrategy = %q, want Base64", key, got)
		}
		if got, _, _ := unstructured.NestedString(entry, "remoteRef", "property"); got != key+".shop.production" {
			t.Errorf("%s: remoteRef.property = %q, want %q", key, got, key+".shop.production")
		}
	}
}

// The Rollout mounts two Secrets: the environment's (secrets set without an
// app) and the app's. Each has its own ExternalSecret.
func TestEnvironmentWideAndAppSecretsHaveSeparateExternalSecrets(t *testing.T) {
	// Arrange
	dyn := newRuntimeFake()
	mgr := managerOn(dyn)

	// Act
	for _, ref := range []ports.SecretRef{shopSecret("", "SHARED_KEY"), shopSecret("api", "DB_URL")} {
		if err := mgr.CreateSecret(context.Background(), ref, "v"); err != nil {
			t.Fatalf("CreateSecret %s: %v", ref.Name, err)
		}
	}

	// Assert
	envWide := naming.UserSecretName(shopTenant, shopProject, shopEnv, "")
	appWide := naming.UserSecretName(shopTenant, shopProject, shopEnv, "api")
	objects := externalSecretsIn(t, dyn)
	if got := namesOf(objects); !reflect.DeepEqual(got, []string{envWide, appWide}) {
		t.Fatalf("ExternalSecrets = %v, want %v", got, []string{envWide, appWide})
	}
	if got := secretKeysOf(t, objects[envWide]); !reflect.DeepEqual(got, []string{"SHARED_KEY"}) {
		t.Errorf("%s keys = %v, want [SHARED_KEY]", envWide, got)
	}
	if got := secretKeysOf(t, objects[appWide]); !reflect.DeepEqual(got, []string{"DB_URL"}) {
		t.Errorf("%s keys = %v, want [DB_URL]", appWide, got)
	}
}

// An environment written by an earlier version holds one ExternalSecret per
// secret. The first write to a target takes their secrets over and removes
// them, leaving the Secret in place for the new owner to adopt.
func TestWritingASecretReplacesThePerSecretExternalSecretsOfItsTarget(t *testing.T) {
	// Arrange
	otherApp := perSecretExternalSecret("worker", "QUEUE_URL")
	dyn := newRuntimeFake(perSecretExternalSecret("api", "PROBE_SECRET"), perSecretExternalSecret("api", "SECOND_SECRET"), otherApp)
	mgr := managerOn(dyn)

	// Act
	if err := mgr.CreateSecret(context.Background(), shopSecret("api", "THIRD_SECRET"), "v"); err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}

	// Assert
	target := naming.UserSecretName(shopTenant, shopProject, shopEnv, "api")
	objects := externalSecretsIn(t, dyn)
	if got, want := namesOf(objects), []string{otherApp.GetName(), target}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ExternalSecrets = %v, want %v: the per-secret objects of the target are removed and no other", got, want)
	}
	if got, want := secretKeysOf(t, objects[target]), []string{"PROBE_SECRET", "SECOND_SECRET", "THIRD_SECRET"}; !reflect.DeepEqual(got, want) {
		t.Errorf("spec.data keys = %v, want %v: the secrets the removed objects delivered are kept", got, want)
	}
	for _, action := range dyn.Actions() {
		del, ok := action.(k8stesting.DeleteActionImpl)
		if !ok {
			continue
		}
		policy := del.DeleteOptions.PropagationPolicy
		if policy == nil || *policy != metav1.DeletePropagationOrphan {
			t.Errorf("delete of %s does not orphan its Secret: the app's Secret would be deleted with it", del.Name)
		}
	}
}

func TestDeletingASecretRemovesItsEntryFromTheExternalSecret(t *testing.T) {
	// Arrange
	dyn := newRuntimeFake()
	mgr := managerOn(dyn)
	for _, name := range []string{"PROBE_SECRET", "SECOND_SECRET"} {
		if err := mgr.CreateSecret(context.Background(), shopSecret("api", name), "v"); err != nil {
			t.Fatalf("CreateSecret %s: %v", name, err)
		}
	}

	// Act
	if err := mgr.DeleteSecret(context.Background(), shopSecret("api", "PROBE_SECRET")); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}

	// Assert
	target := naming.UserSecretName(shopTenant, shopProject, shopEnv, "api")
	es, ok := externalSecretsIn(t, dyn)[target]
	if !ok {
		t.Fatalf("ExternalSecret %q is gone, want it kept for the remaining secret", target)
	}
	if got := secretKeysOf(t, es); !reflect.DeepEqual(got, []string{"SECOND_SECRET"}) {
		t.Errorf("spec.data keys = %v, want [SECOND_SECRET]", got)
	}
}

func TestDeletingTheLastSecretOfATargetRemovesItsExternalSecret(t *testing.T) {
	// Arrange
	dyn := newRuntimeFake()
	mgr := managerOn(dyn)
	if err := mgr.CreateSecret(context.Background(), shopSecret("api", "PROBE_SECRET"), "v"); err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}

	// Act
	if err := mgr.DeleteSecret(context.Background(), shopSecret("api", "PROBE_SECRET")); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}

	// Assert
	if got := namesOf(externalSecretsIn(t, dyn)); len(got) != 0 {
		t.Errorf("ExternalSecrets = %v, want none: an ExternalSecret without data is refused by the operator", got)
	}
}

// A secret deleted while its target is still delivered by per-secret objects
// must not come back through them.
func TestDeletingASecretRemovesItsPerSecretExternalSecret(t *testing.T) {
	// Arrange
	dyn := newRuntimeFake(perSecretExternalSecret("api", "PROBE_SECRET"), perSecretExternalSecret("api", "SECOND_SECRET"))
	mgr := managerOn(dyn)

	// Act
	if err := mgr.DeleteSecret(context.Background(), shopSecret("api", "PROBE_SECRET")); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}

	// Assert
	target := naming.UserSecretName(shopTenant, shopProject, shopEnv, "api")
	objects := externalSecretsIn(t, dyn)
	if got := namesOf(objects); !reflect.DeepEqual(got, []string{target}) {
		t.Fatalf("ExternalSecrets = %v, want only %q", got, target)
	}
	if got := secretKeysOf(t, objects[target]); !reflect.DeepEqual(got, []string{"SECOND_SECRET"}) {
		t.Errorf("spec.data keys = %v, want [SECOND_SECRET]", got)
	}
}
