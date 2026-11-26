// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cellrouting_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/cellrouting"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/operatorboot"
	"github.com/getloopscale/loop-runtime/operators/operatortest"
)

// operatorIdentity is the ServiceAccount the operator charts bind their RBAC to.
const operatorIdentity = "system:serviceaccount:loopzero-system:loopzero-tenant-operator"

// TestCellAwareFactory_ResolvesACellWithSecretReadGrantedOnlyInThePlatformNamespace
// runs the factory the way an operator binary does — through a cached client —
// under an identity holding exactly what the operator charts grant: read on
// ClusterCredentials, and read on Secrets in the platform namespace ONLY, via a
// namespaced Role.
//
// A cache that watches Secrets in every namespace needs a cluster-wide list on
// Secrets, which that identity does not have: the informer never syncs and the
// kubeconfig read blocks until the deadline. So this test fails unless the
// manager's Secret cache is confined to the namespaces the Role covers.
func TestCellAwareFactory_ResolvesACellWithSecretReadGrantedOnlyInThePlatformNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("client-go scheme: %v", err)
	}
	if err := loopzerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("loopzero scheme: %v", err)
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: operatortest.BinaryAssetsDirectory(),
	}
	adminCfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := testEnv.Stop(); stopErr != nil {
			t.Errorf("envtest stop: %v", stopErr)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, err := client.New(adminCfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	seedCell(ctx, t, admin)
	grantOperatorSecretReadInPlatformNamespace(ctx, t, admin)

	user, err := testEnv.AddUser(envtest.User{Name: operatorIdentity}, adminCfg)
	if err != nil {
		t.Fatalf("add operator identity: %v", err)
	}
	operatorCfg := user.Config()
	assertNoClusterWideSecretList(ctx, t, operatorCfg, scheme)

	secretScope, err := operatorboot.SecretCache(constants.NamespacePlatformSystem)
	if err != nil {
		t.Fatalf("SecretCache: %v", err)
	}
	mgmt := startCachedClient(ctx, t, operatorCfg, scheme, cache.Options{
		Scheme:   scheme,
		ByObject: map[client.Object]cache.ByObject{&corev1.Secret{}: secretScope},
	})

	factory := cellrouting.NewCellAwareRuntimeClientFactory(mgmt, nil, constants.NamespacePlatformSystem)
	lookupCtx, lookupCancel := context.WithTimeout(ctx, 15*time.Second)
	defer lookupCancel()
	if _, err := factory.For(lookupCtx, "cell-1"); err != nil {
		t.Fatalf("factory.For(cell-1) with Secret read granted only in %s: %v",
			constants.NamespacePlatformSystem, err)
	}
}

// seedCell writes the ClusterCredential of cell-1 and its kubeconfig Secret
// in the platform namespace.
func seedCell(ctx context.Context, t *testing.T, admin client.Client) {
	t.Helper()
	ns := constants.NamespacePlatformSystem
	for _, obj := range []client.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "cell-1-kubeconfig", Namespace: ns},
			Data:       map[string][]byte{"kubeconfig": kubeconfigFor("https://127.0.0.1:6443", "cell-1-token")},
		},
		&loopzerov1.ClusterCredential{
			ObjectMeta: metav1.ObjectMeta{Name: "cell-1", Namespace: ns},
			Spec: loopzerov1.ClusterCredentialSpec{
				ClusterName: "loopzero-cell-1",
				APIServer:   "https://127.0.0.1:6443",
				CellID:      "cell-1",
				SecretRef:   loopzerov1.SecretReference{Name: "cell-1-kubeconfig", Namespace: ns},
				Scope:       loopzerov1.CredentialScope{Operators: true},
			},
		},
	} {
		if err := admin.Create(ctx, obj); err != nil {
			t.Fatalf("create %T %s: %v", obj, obj.GetName(), err)
		}
	}
}

// grantOperatorSecretReadInPlatformNamespace gives the operator identity the
// shape of access the operator charts render: ClusterCredentials cluster-wide
// (a ClusterRole), Secrets in the platform namespace only (a Role).
func grantOperatorSecretReadInPlatformNamespace(ctx context.Context, t *testing.T, admin client.Client) {
	t.Helper()
	ns := constants.NamespacePlatformSystem
	subject := rbacv1.Subject{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: operatorIdentity}
	read := []string{"get", "list", "watch"}
	for _, obj := range []client.Object{
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "operator-cluster-credentials"},
			Rules:      []rbacv1.PolicyRule{{APIGroups: []string{"loopzero.io"}, Resources: []string{"clustercredentials"}, Verbs: read}},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "operator-cluster-credentials"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "operator-cluster-credentials"},
			Subjects:   []rbacv1.Subject{subject},
		},
		&rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: "operator-secrets", Namespace: ns},
			Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: read}},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "operator-secrets", Namespace: ns},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "operator-secrets"},
			Subjects:   []rbacv1.Subject{subject},
		},
	} {
		if err := admin.Create(ctx, obj); err != nil {
			t.Fatalf("create %T %s: %v", obj, obj.GetName(), err)
		}
	}
}

// assertNoClusterWideSecretList proves the identity really is narrowed: a
// test that passed because the identity could read every Secret would prove
// nothing about the cache scoping.
func assertNoClusterWideSecretList(ctx context.Context, t *testing.T, cfg *rest.Config, scheme *runtime.Scheme) {
	t.Helper()
	direct, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("operator direct client: %v", err)
	}
	var all corev1.SecretList
	if err := direct.List(ctx, &all); !apierrors.IsForbidden(err) {
		t.Fatalf("cluster-wide Secret list as the operator: err = %v, want Forbidden", err)
	}
}

// startCachedClient builds the client an operator manager hands its
// controllers: reads through an informer cache built with opts.
func startCachedClient(ctx context.Context, t *testing.T, cfg *rest.Config, scheme *runtime.Scheme, opts cache.Options) client.Client {
	t.Helper()
	informers, err := cache.New(cfg, opts)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	cacheCtx, stop := context.WithCancel(ctx)
	t.Cleanup(stop)
	go func() {
		if startErr := informers.Start(cacheCtx); startErr != nil {
			t.Errorf("cache start: %v", startErr)
		}
	}()
	if !informers.WaitForCacheSync(cacheCtx) {
		t.Fatal("cache did not start")
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme, Cache: &client.CacheOptions{Reader: informers}})
	if err != nil {
		t.Fatalf("cached client: %v", err)
	}
	return c
}

// kubeconfigFor builds a syntactically valid kubeconfig. Building a client
// from it does not dial the server.
func kubeconfigFor(server, token string) []byte {
	return []byte(`apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: ` + server + `
    insecure-skip-tls-verify: true
contexts:
- name: c
  context:
    cluster: c
    user: u
current-context: c
users:
- name: u
  user:
    token: ` + token + `
`)
}
