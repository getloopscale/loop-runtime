// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/loopzero/loopzero/operators/internal/constants"
)

func TestDeleteApplications_NotFoundIsIdempotent(t *testing.T) {
	// Arrange: Create a fake client with one Application CR.
	// Inject a reactor that returns NotFound on Delete (simulating concurrent deletion).
	scheme := runtime.NewScheme()
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "argoproj.io", Version: "v1alpha1", Kind: "Application",
	})
	app.SetName("acme-production")
	app.SetNamespace("argocd")
	app.SetLabels(map[string]string{constants.LabelTenant: "acme"})

	client := dynamicfake.NewSimpleDynamicClient(scheme, app)

	// Prepend a reactor that makes Delete return NotFound (simulates concurrent delete)
	client.PrependReactor("delete", "applications", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(
			schema.GroupResource{Group: "argoproj.io", Resource: "applications"},
			"acme-production",
		)
	})

	provisioner := NewRealProvisioner("argocd", client)

	// Act
	err := provisioner.DeleteApplications(context.Background(), "acme")

	// Assert: NotFound must NOT be treated as error
	if err != nil {
		t.Errorf("DeleteApplications should ignore NotFound errors (concurrent delete), got: %v", err)
	}
}

func TestDeleteApplications_OtherErrorPropagates(t *testing.T) {
	// Arrange: a non-NotFound error should still propagate
	scheme := runtime.NewScheme()
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "argoproj.io", Version: "v1alpha1", Kind: "Application",
	})
	app.SetName("acme-production")
	app.SetNamespace("argocd")
	app.SetLabels(map[string]string{constants.LabelTenant: "acme"})

	client := dynamicfake.NewSimpleDynamicClient(scheme, app)

	// Prepend a reactor that makes Delete return Forbidden
	client.PrependReactor("delete", "applications", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "argoproj.io", Resource: "applications"},
			"acme-production",
			fmt.Errorf("forbidden"),
		)
	})

	provisioner := NewRealProvisioner("argocd", client)

	// Act
	err := provisioner.DeleteApplications(context.Background(), "acme")

	// Assert: non-NotFound errors must propagate
	if err == nil {
		t.Error("DeleteApplications should propagate non-NotFound errors")
	}
}

func TestDeleteAppProject_NotFoundIsIdempotent(t *testing.T) {
	// Arrange: DeleteAppProject already handles NotFound — this is a regression test
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClient(scheme)

	provisioner := NewRealProvisioner("argocd", client)

	// Act: delete a non-existent AppProject
	err := provisioner.DeleteAppProject(context.Background(), "nonexistent")

	// Assert: should not error
	if err != nil {
		t.Errorf("DeleteAppProject should ignore NotFound, got: %v", err)
	}
}

func TestBuildApplicationObject_ManualSyncPolicy(t *testing.T) {
	// The Application CR created by EnsureApplication must use manual sync
	// (automated: nil), matching the spec, Helm template, and infra test.
	// Auto-sync (selfHeal/prune) creates race conditions with the API's
	// manual POST /sync trigger. See MEET-2026-03-27-012.

	spec := ApplicationSpec{
		Name:           "acme-production",
		Namespace:      "argocd",
		TenantID:       "acme",
		RepoURL:        "http://gitea:3000/acme/gitops.git",
		TargetRevision: "HEAD",
		Path:           "production",
		DestinationURL: "https://workload:6443",
		DestinationNS:  "loopzero-acme-production",
	}

	app := buildApplicationObject(spec)

	// Extract syncPolicy from the unstructured object
	appSpec, ok := app.Object["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("missing spec in Application object")
	}

	syncPolicy, ok := appSpec["syncPolicy"].(map[string]interface{})
	if !ok {
		t.Fatal("missing syncPolicy in Application spec")
	}

	// automated must be nil/absent (manual sync — API controls timing)
	automated, exists := syncPolicy["automated"]
	if exists && automated != nil {
		t.Errorf("syncPolicy.automated must be nil (manual sync), got %v", automated)
	}

	// syncOptions must include PrunePropagationPolicy=foreground and PruneLast=true
	syncOptions, ok := syncPolicy["syncOptions"].([]interface{})
	if !ok {
		t.Fatal("missing syncOptions in syncPolicy")
	}

	// CreateNamespace must be false (TenantController creates namespaces)
	foundCreateNS := false
	for _, opt := range syncOptions {
		if s, ok := opt.(string); ok && s == "CreateNamespace=false" {
			foundCreateNS = true
		}
	}
	if !foundCreateNS {
		t.Error("syncOptions must include CreateNamespace=false (namespace created by TenantController)")
	}
}

func TestBuildApplicationObject_Labels(t *testing.T) {
	spec := ApplicationSpec{
		Name:           "acme-production",
		Namespace:      "argocd",
		TenantID:       "acme",
		RepoURL:        "http://gitea:3000/acme/gitops.git",
		TargetRevision: "HEAD",
		Path:           "production",
		DestinationURL: "https://workload:6443",
		DestinationNS:  "loopzero-acme-production",
	}

	app := buildApplicationObject(spec)

	metadata, ok := app.Object["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("missing metadata")
	}
	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		t.Fatal("missing labels")
	}

	if labels["loopzero.io/tenant"] != "acme" {
		t.Errorf("expected tenant label 'acme', got %v", labels["loopzero.io/tenant"])
	}
	if labels["loopzero.io/managed-by"] != "tenant-controller" {
		t.Errorf("expected managed-by label 'tenant-controller', got %v", labels["loopzero.io/managed-by"])
	}
}

// MEET-2026-04-22-058 (B12): skip-reconcile annotation must not be set by the
// operator anymore. The ArgoCD controller reconciles apps normally; amplification
// risk is bounded by manual sync + self-heal=false + default 3m reconcile interval.
func TestBuildApplicationObject_HasNoSkipReconcile(t *testing.T) {
	spec := ApplicationSpec{
		Name:           "acme-production",
		TenantID:       "acme",
		RepoURL:        "http://gitea/loopzero/acme-gitops.git",
		TargetRevision: "main",
		Path:           "production",
		DestinationURL: "https://workload-cluster:6443",
		DestinationNS:  "acme-production",
	}
	obj := buildApplicationObject(spec)
	meta := obj.Object["metadata"].(map[string]interface{})
	annotations, _ := meta["annotations"].(map[string]interface{})
	if _, present := annotations["argocd.argoproj.io/skip-reconcile"]; present {
		t.Errorf("skip-reconcile annotation must NOT be set on new Applications; got %v", annotations)
	}
}

// Notifications subscription MUST stay — it enables the on-sync-healthy webhook.
func TestBuildApplicationObject_KeepsNotificationSubscription(t *testing.T) {
	spec := ApplicationSpec{Name: "acme-production", TenantID: "acme"}
	obj := buildApplicationObject(spec)
	meta := obj.Object["metadata"].(map[string]interface{})
	annotations, _ := meta["annotations"].(map[string]interface{})
	key := "notifications.argoproj.io/subscribe.on-sync-healthy.loopzero-api"
	if _, present := annotations[key]; !present {
		t.Errorf("notification subscription annotation must be preserved; got %v", annotations)
	}
}

// MEET-2026-04-22-058 (B12): convergence path — any pre-existing Application
// with the legacy skip-reconcile annotation must have it stripped when the
// operator reconciles.
func TestMergeExistingMetadata_StripsSkipReconcile(t *testing.T) {
	existing := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"annotations": map[string]interface{}{
					"argocd.argoproj.io/skip-reconcile": "true",
					"keep-me":                           "yes",
				},
			},
		},
	}
	desired := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"annotations": map[string]interface{}{
					"overlay-me": "on",
				},
			},
		},
	}
	mergeExistingMetadata(existing, desired)
	merged := desired.Object["metadata"].(map[string]interface{})["annotations"].(map[string]interface{})
	if _, present := merged["argocd.argoproj.io/skip-reconcile"]; present {
		t.Errorf("skip-reconcile must be stripped during merge; got %v", merged)
	}
	if merged["keep-me"] != "yes" {
		t.Errorf("unrelated existing annotation must be preserved; got %v", merged)
	}
	if merged["overlay-me"] != "on" {
		t.Errorf("desired overlay annotation must be present; got %v", merged)
	}
}
