// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/argocd"
	"github.com/getloopscale/loopzero/operators/internal/constants"
)

// The basis paths of reconcileDelete, one per test: finalizer absent, the
// three phases (child Environments, external cleanup with backoff, build
// namespace verification) and the finalizer release.

type deletionArgo struct {
	deleteAppsErr error
	deleted       []string
}

func (f *deletionArgo) EnsureAppProject(context.Context, argocd.AppProjectSpec) error   { return nil }
func (f *deletionArgo) EnsureApplication(context.Context, argocd.ApplicationSpec) error { return nil }
func (f *deletionArgo) DeleteApplications(_ context.Context, tenant string) error {
	f.deleted = append(f.deleted, "apps/"+tenant)
	return f.deleteAppsErr
}
func (f *deletionArgo) DeleteAppProject(_ context.Context, tenant string) error {
	f.deleted = append(f.deleted, "project/"+tenant)
	return nil
}
func (f *deletionArgo) ListApplicationNames(context.Context, string) ([]string, error) {
	return nil, nil
}
func (f *deletionArgo) DeleteApplication(context.Context, string) error { return nil }

type deletionGit struct{ archived []string }

func (f *deletionGit) EnsureRepo(context.Context, string, string) (string, error) { return "", nil }
func (f *deletionGit) ArchiveRepo(_ context.Context, name string) error {
	f.archived = append(f.archived, name)
	return nil
}

type deletionMinIO struct{ deleteErr error }

func (f *deletionMinIO) EnsureTenantCredentials(context.Context, string) (string, string, error) {
	return "", "", nil
}
func (f *deletionMinIO) DeleteTenantCredentials(context.Context, string) error { return f.deleteErr }

const deletingTenantName = "tn-del"

func deletingTenant(retries string) *loopzerov1.Tenant {
	now := metav1.Now()
	t := &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: deletingTenantName, Namespace: constants.NamespacePlatformSystem, UID: "tenant-uid",
			Finalizers: []string{tenantFinalizer}, DeletionTimestamp: &now,
		},
		Spec:   loopzerov1.TenantSpec{Plan: loopzerov1.PlanStarter, GitOps: loopzerov1.GitOpsSpec{RepoName: "gitops-" + deletingTenantName}},
		Status: loopzerov1.TenantStatus{Phase: loopzerov1.TenantPhaseActive},
	}
	if retries != "" {
		t.Annotations = map[string]string{constants.AnnotationDeletionRetryCount: retries}
	}
	return t
}

func childEnvironment(name string, owner *loopzerov1.Tenant, deleting bool) *loopzerov1.Environment {
	env := &loopzerov1.Environment{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: owner.Namespace,
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "loopzero.io/v1", Kind: "Tenant", Name: owner.Name, UID: owner.UID}},
	}}
	if deleting {
		now := metav1.Now()
		env.DeletionTimestamp = &now
		env.Finalizers = []string{constants.FinalizerEnvironment}
	}
	return env
}

func buildNamespace(tenant string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "loopzero-build-" + tenant, Labels: map[string]string{constants.LabelTenant: tenant},
	}}
}

type deletionFixture struct {
	r     *TenantReconciler
	mgmt  client.Client
	argo  *deletionArgo
	git   *deletionGit
	build client.Client
}

func newDeletionFixture(t *testing.T, tenant *loopzerov1.Tenant, mgmtObjs []client.Object, build client.Client) *deletionFixture {
	t.Helper()
	s := cellScheme(t)
	mgmt := fake.NewClientBuilder().WithScheme(s).WithObjects(append([]client.Object{tenant}, mgmtObjs...)...).
		WithStatusSubresource(&loopzerov1.Tenant{}).Build()
	if build == nil {
		build = fake.NewClientBuilder().WithScheme(s).Build()
	}
	f := &deletionFixture{mgmt: mgmt, argo: &deletionArgo{}, git: &deletionGit{}, build: build}
	f.r = &TenantReconciler{Client: mgmt, Scheme: s, BuildClient: build, ArgoCDProvisioner: f.argo, GitProvisioner: f.git}
	return f
}

func (f *deletionFixture) run(t *testing.T) (ctrl.Result, error) {
	t.Helper()
	var tenant loopzerov1.Tenant
	if err := f.mgmt.Get(context.Background(), types.NamespacedName{Name: deletingTenantName, Namespace: constants.NamespacePlatformSystem}, &tenant); err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	return f.r.reconcileDelete(context.Background(), &tenant)
}

func (f *deletionFixture) tenant(t *testing.T) (*loopzerov1.Tenant, bool) {
	t.Helper()
	var tenant loopzerov1.Tenant
	err := f.mgmt.Get(context.Background(), types.NamespacedName{Name: deletingTenantName, Namespace: constants.NamespacePlatformSystem}, &tenant)
	if err != nil {
		return nil, false
	}
	return &tenant, true
}

func TestReconcileDelete_MovesThePhaseToDeleting(t *testing.T) {
	f := newDeletionFixture(t, deletingTenant(""), []client.Object{childEnvironment("env-a", deletingTenant(""), true)}, nil)

	if _, err := f.run(t); err != nil {
		t.Fatalf("reconcileDelete: %v", err)
	}

	got, _ := f.tenant(t)
	if got.Status.Phase != loopzerov1.TenantPhaseDeleting {
		t.Fatalf("phase = %q, want Deleting", got.Status.Phase)
	}
}

func TestReconcileDelete_DeletesChildEnvironmentsAndWaits(t *testing.T) {
	owner := deletingTenant("")
	f := newDeletionFixture(t, owner, []client.Object{childEnvironment("env-a", owner, false)}, nil)

	result, err := f.run(t)

	if err != nil || result.RequeueAfter != constants.RequeueDeletionWait {
		t.Fatalf("result = %+v, err = %v; want a wait requeue while the child goes", result, err)
	}
	var env loopzerov1.Environment
	if err := f.mgmt.Get(context.Background(), types.NamespacedName{Name: "env-a", Namespace: owner.Namespace}, &env); err == nil {
		t.Fatal("child Environment was not deleted")
	}
	if len(f.argo.deleted) != 0 {
		t.Fatal("external cleanup ran before the child Environments were gone")
	}
}

func TestReconcileDelete_IgnoresEnvironmentsOfOtherTenants(t *testing.T) {
	stranger := &loopzerov1.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tn-other", UID: "other-uid", Namespace: constants.NamespacePlatformSystem}}
	f := newDeletionFixture(t, deletingTenant(""), []client.Object{childEnvironment("env-other", stranger, false)}, nil)

	result, err := f.run(t)

	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("result = %+v, err = %v; another tenant's Environment must not hold this deletion", result, err)
	}
	var env loopzerov1.Environment
	if err := f.mgmt.Get(context.Background(), types.NamespacedName{Name: "env-other", Namespace: constants.NamespacePlatformSystem}, &env); err != nil {
		t.Fatalf("another tenant's Environment was deleted: %v", err)
	}
}

func TestReconcileDelete_ReturnsTheChildDeleteError(t *testing.T) {
	owner := deletingTenant("")
	deleteErr := errors.New("forbidden")
	s := cellScheme(t)
	mgmt := interceptor.NewClient(fake.NewClientBuilder().WithScheme(s).
		WithObjects(owner, childEnvironment("env-a", owner, false)).WithStatusSubresource(&loopzerov1.Tenant{}).Build(),
		interceptor.Funcs{Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error { return deleteErr }})
	f := newDeletionFixture(t, deletingTenant(""), nil, nil)
	f.mgmt, f.r.Client = mgmt, mgmt

	_, err := f.run(t)

	if !errors.Is(err, deleteErr) {
		t.Fatalf("error = %v, want the child delete error", err)
	}
}

func TestReconcileDelete_BacksOffWhenACleanupStepFails(t *testing.T) {
	f := newDeletionFixture(t, deletingTenant(""), nil, nil)
	f.argo.deleteAppsErr = errors.New("argocd down")

	result, err := f.run(t)

	if err != nil || result.RequeueAfter != deletionBackoff(1) {
		t.Fatalf("result = %+v, err = %v; want the first backoff %v", result, err, deletionBackoff(1))
	}
	got, _ := f.tenant(t)
	if getDeletionRetryCount(got) != 1 || !containsString(got.Finalizers, tenantFinalizer) {
		t.Fatalf("retries = %d, finalizers = %v; want one recorded retry and the finalizer kept", getDeletionRetryCount(got), got.Finalizers)
	}
	if len(f.git.archived) != 1 {
		t.Fatal("a failing step must not stop the remaining cleanup steps")
	}
}

func TestReconcileDelete_ReportsStuckAfterTheLastAttempt(t *testing.T) {
	f := newDeletionFixture(t, deletingTenant("9"), nil, nil)
	f.argo.deleteAppsErr = errors.New("argocd down")

	result, err := f.run(t)

	if err != nil || result.RequeueAfter != constants.RequeueDeletionStuck {
		t.Fatalf("result = %+v, err = %v; want the stuck requeue", result, err)
	}
}

func TestReconcileDelete_RefusesToArchiveARepoTheTenantDoesNotOwn(t *testing.T) {
	tenant := deletingTenant("")
	tenant.Spec.GitOps.RepoName = "someone-else"
	f := newDeletionFixture(t, tenant, nil, nil)

	result, err := f.run(t)

	if err != nil || result.RequeueAfter != deletionBackoff(1) {
		t.Fatalf("result = %+v, err = %v; want the ownership refusal treated as a failed step", result, err)
	}
	if len(f.git.archived) != 0 {
		t.Fatalf("archived %v, a repo the tenant does not own", f.git.archived)
	}
}

func TestReconcileDelete_TreatsAMinIOFailureAsAFailedStep(t *testing.T) {
	f := newDeletionFixture(t, deletingTenant(""), nil, nil)
	f.r.MinIOProvisioner = &deletionMinIO{deleteErr: errors.New("minio down")}

	result, err := f.run(t)

	if err != nil || result.RequeueAfter != deletionBackoff(1) {
		t.Fatalf("result = %+v, err = %v; want the backoff", result, err)
	}
}

func TestReconcileDelete_WaitsForBuildNamespacesToGo(t *testing.T) {
	s := cellScheme(t)
	ns := buildNamespace(deletingTenantName)
	ns.Finalizers = []string{"kubernetes"}
	build := fake.NewClientBuilder().WithScheme(s).WithObjects(ns).Build()
	f := newDeletionFixture(t, deletingTenant(""), nil, build)

	result, err := f.run(t)

	if err != nil || result.RequeueAfter != constants.RequeueDeletionWait {
		t.Fatalf("result = %+v, err = %v; want a wait while the build namespace terminates", result, err)
	}
	if got, _ := f.tenant(t); !containsString(got.Finalizers, tenantFinalizer) {
		t.Fatal("finalizer released while a build namespace still exists")
	}
}

// A build cluster that cannot be read is a failure, not a pause: it must
// reach controller-runtime's error handling and backoff.
func TestReconcileDelete_ReturnsTheBuildNamespaceVerificationError(t *testing.T) {
	s := cellScheme(t)
	listErr := errors.New("build cluster unreachable")
	lists := 0
	build := interceptor.NewClient(fake.NewClientBuilder().WithScheme(s).Build(), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			lists++
			if lists > 1 {
				return listErr
			}
			return c.List(ctx, list, opts...)
		},
	})
	f := newDeletionFixture(t, deletingTenant(""), nil, build)

	_, err := f.run(t)

	if !errors.Is(err, listErr) {
		t.Fatalf("error = %v, want the build namespace list error", err)
	}
	if got, _ := f.tenant(t); !containsString(got.Finalizers, tenantFinalizer) {
		t.Fatal("finalizer released although the build namespaces could not be verified")
	}
}

func TestReconcileDelete_ReleasesTheFinalizerWhenEverythingIsGone(t *testing.T) {
	f := newDeletionFixture(t, deletingTenant(""), nil, nil)

	result, err := f.run(t)

	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if got, ok := f.tenant(t); ok && containsString(got.Finalizers, tenantFinalizer) {
		t.Fatal("finalizer still present after a clean deletion")
	}
	if len(f.argo.deleted) != 2 || len(f.git.archived) != 1 {
		t.Fatalf("argo deletions %v, archived %v; want apps, project and the repo", f.argo.deleted, f.git.archived)
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
