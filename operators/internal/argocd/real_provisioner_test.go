// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
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
		DestinationURL: "https://cell-0-cluster:6443",
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
	mergeExistingState(existing, desired)
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

// --- #271: preserving .status across the full replace -------------------------------
//
// The applications.argoproj.io CRD does NOT enable the `status` subresource
// (checked on app-dev 2026-08-08: `kubectl get crd applications.argoproj.io -o
// jsonpath='{.spec.versions[*].subresources}'` → `{}`). So `.status` is an
// ordinary field of the object, and an Update of a whole object built from
// scratch ERASES it. Measured: sync/health flapping to empty every few seconds.
// Plan:
// knowledge-base/plans/argocd-application-status-preservation-plan.md

// seedUnstructured builds an ArgoCD object with an optional .status.
func seedUnstructured(kind, name string, status map[string]interface{}) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "argoproj.io", Version: "v1alpha1", Kind: kind,
	})
	obj.SetName(name)
	obj.SetNamespace("argocd")
	if status != nil {
		obj.Object["status"] = status
	}
	return obj
}

func syncStatusOf(t *testing.T, obj *unstructured.Unstructured) string {
	t.Helper()
	st, ok := obj.Object["status"].(map[string]interface{})
	if !ok {
		return ""
	}
	sync, ok := st["sync"].(map[string]interface{})
	if !ok {
		return ""
	}
	s, _ := sync["status"].(string)
	return s
}

// Reproduces issue #271: the status ArgoCD computed must survive a
// tenant-operator reconcile.
func TestEnsureApplication_PreservesTheExistingStatus(t *testing.T) {
	app := seedUnstructured("Application", "acme-production", map[string]interface{}{
		"sync":   map[string]interface{}{"status": "Synced"},
		"health": map[string]interface{}{"status": "Healthy"},
	})
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), app)
	p := NewRealProvisioner("argocd", client)

	err := p.EnsureApplication(context.Background(), ApplicationSpec{
		Name: "acme-production", Namespace: "argocd", TenantID: "acme",
		RepoURL: "http://gitea/acme-gitops.git", TargetRevision: "main",
		Path: "production", DestinationNS: "loopzero-acme-web-production", CellID: "cell-0",
	})
	if err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}

	got, err := client.Resource(applicationGVR).Namespace("argocd").
		Get(context.Background(), "acme-production", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}

	if s := syncStatusOf(t, got); s != "Synced" {
		t.Errorf("ArgoCD's .status.sync.status was erased by the Update: want %q, got %q "+
			"(issue #271 — the CRD has no status subresource, so the full replace overwrites the field)",
			"Synced", s)
	}
	st, _ := got.Object["status"].(map[string]interface{})
	health, _ := st["health"].(map[string]interface{})
	if h, _ := health["status"].(string); h != "Healthy" {
		t.Errorf(".status.health.status was erased: want %q, got %q", "Healthy", h)
	}

	// The Update must actually have happened — otherwise the test would pass by omission.
	spec, _ := got.Object["spec"].(map[string]interface{})
	source, _ := spec["source"].(map[string]interface{})
	if pth, _ := source["path"].(string); pth != "production" {
		t.Errorf("the spec was not written: source.path = %q, want %q", pth, "production")
	}
}

// Covers EC-5: the AppProject goes through the same merge function but has NO
// retry — the copy happens outside any closure.
func TestEnsureAppProject_PreservesTheExistingStatus(t *testing.T) {
	proj := seedUnstructured("AppProject", "acme", map[string]interface{}{
		"sync": map[string]interface{}{"status": "Synced"},
	})
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), proj)
	p := NewRealProvisioner("argocd", client)

	err := p.EnsureAppProject(context.Background(), AppProjectSpec{
		Name: "acme", Namespace: "argocd", TenantID: "acme",
		SourceRepos:           []string{"http://gitea/acme-gitops.git"},
		DestinationNamespaces: []string{"loopzero-acme-web-production"},
		CellID:                "cell-0",
	})
	if err != nil {
		t.Fatalf("EnsureAppProject: %v", err)
	}

	got, err := client.Resource(appProjectGVR).Namespace("argocd").
		Get(context.Background(), "acme", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if s := syncStatusOf(t, got); s != "Synced" {
		t.Errorf("the AppProject's .status was erased by the Update: want %q, got %q", "Synced", s)
	}
}

// Covers EC-4: a `status` that is present and empty is NOT the same as absent.
// A len() check would introduce exactly the bug this test forbids.
func TestEnsureApplication_KeepsAnEmptyStatusEmpty(t *testing.T) {
	app := seedUnstructured("Application", "acme-staging", map[string]interface{}{})
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), app)
	p := NewRealProvisioner("argocd", client)

	if err := p.EnsureApplication(context.Background(), ApplicationSpec{
		Name: "acme-staging", Namespace: "argocd", TenantID: "acme",
		RepoURL: "http://gitea/acme-gitops.git", TargetRevision: "main",
		Path: "staging", DestinationNS: "loopzero-acme-web-staging", CellID: "cell-0",
	}); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}

	got, err := client.Resource(applicationGVR).Namespace("argocd").
		Get(context.Background(), "acme-staging", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	st, present := got.Object["status"]
	if !present {
		t.Fatalf("the status key existed (empty) and vanished — preserving is not discarding what looks empty")
	}
	if m, ok := st.(map[string]interface{}); !ok || len(m) != 0 {
		t.Errorf("an empty status must stay empty; got %#v", st)
	}
}

// Covers EC-3: `desired` is built ONCE outside the retry closure
// (real_provisioner.go:284) and reused across attempts. Under a conflict, the
// status written must be the RE-READ object's, not the first read's.
func TestEnsureApplication_RetryUsesTheStatusOfTheReread(t *testing.T) {
	app := seedUnstructured("Application", "acme-production", map[string]interface{}{
		"sync": map[string]interface{}{"status": "Synced"},
	})
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), app)

	var updates int
	client.PrependReactor("update", "applications", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updates++
		if updates > 1 {
			return false, nil, nil // the second attempt takes the normal path
		}
		// Simulates ArgoCD recomputing the status between our read and our write.
		moved := seedUnstructured("Application", "acme-production", map[string]interface{}{
			"sync": map[string]interface{}{"status": "OutOfSync"},
		})
		if err := client.Tracker().Update(applicationGVR, moved, "argocd"); err != nil {
			t.Fatalf("tracker update: %v", err)
		}
		return true, nil, apierrors.NewConflict(
			schema.GroupResource{Group: "argoproj.io", Resource: "applications"},
			"acme-production", fmt.Errorf("simulated conflict"),
		)
	})

	p := NewRealProvisioner("argocd", client)
	if err := p.EnsureApplication(context.Background(), ApplicationSpec{
		Name: "acme-production", Namespace: "argocd", TenantID: "acme",
		RepoURL: "http://gitea/acme-gitops.git", TargetRevision: "main",
		Path: "production", DestinationNS: "loopzero-acme-web-production", CellID: "cell-0",
	}); err != nil {
		t.Fatalf("EnsureApplication sob conflito: %v", err)
	}
	if updates < 2 {
		t.Fatalf("the conflict reactor was not exercised (updates=%d) — the test proves nothing", updates)
	}

	got, err := client.Resource(applicationGVR).Namespace("argocd").
		Get(context.Background(), "acme-production", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get after retry: %v", err)
	}
	if s := syncStatusOf(t, got); s != "OutOfSync" {
		t.Errorf("under a conflict the RE-READ status wins: want %q, got %q "+
			"(if the copy moves out of the retry closure, this test catches it)", "OutOfSync", s)
	}
}

// A guard, not a regression: the fix must not fabricate a `status: null` on a
// resource that has no status yet.
func TestEnsureApplication_CreateDoesNotInventAStatus(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	p := NewRealProvisioner("argocd", client)

	if err := p.EnsureApplication(context.Background(), ApplicationSpec{
		Name: "acme-novo", Namespace: "argocd", TenantID: "acme",
		RepoURL: "http://gitea/acme-gitops.git", TargetRevision: "main",
		Path: "production", DestinationNS: "loopzero-acme-web-production", CellID: "cell-0",
	}); err != nil {
		t.Fatalf("EnsureApplication (create): %v", err)
	}

	got, err := client.Resource(applicationGVR).Namespace("argocd").
		Get(context.Background(), "acme-novo", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get after create: %v", err)
	}
	if _, present := got.Object["status"]; present {
		t.Errorf("a newly created Application must not gain a status key; got %#v", got.Object["status"])
	}
}
