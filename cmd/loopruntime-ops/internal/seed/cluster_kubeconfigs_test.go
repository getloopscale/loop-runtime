// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"errors"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const testCAPEM = "-----BEGIN CERTIFICATE-----\nMIIBtest\n-----END CERTIFICATE-----\n"

// The data keys loopruntime-api mounts (charts/loopruntime-api templates/_helpers.tpl).
const (
	coreKey  = "core" + ".kubeconfig"
	buildKey = "build" + ".kubeconfig"
	cell0Key = "cell-0" + ".kubeconfig"
)

type tokenRequestLog struct {
	namespace, name string
	seconds         int64
}

// peerClusterClient is a peer cluster holding the live CA ConfigMap and the
// ServiceAccount the seed mints for. Every TokenRequest it serves is logged.
func peerClusterClient(t *testing.T, saNamespace, saName string, withCA bool, log *[]tokenRequestLog) client.Client {
	t.Helper()
	objs := []client.Object{
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: saNamespace, Name: saName}},
	}
	if withCA {
		objs = append(objs, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "kube-root-ca.crt"},
			Data:       map[string]string{"ca.crt": testCAPEM},
		})
	}
	return fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(objs...).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceCreate: func(ctx context.Context, c client.Client, sub string, obj client.Object, subObj client.Object, opts ...client.SubResourceCreateOption) error {
				if tr, ok := subObj.(*authenticationv1.TokenRequest); ok && tr.Spec.ExpirationSeconds != nil {
					*log = append(*log, tokenRequestLog{obj.GetNamespace(), obj.GetName(), *tr.Spec.ExpirationSeconds})
				}
				return c.SubResource(sub).Create(ctx, obj, subObj, opts...)
			},
		}).Build()
}

func existingClusterKubeconfigs() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopruntime-system", Name: "loopruntime-cluster-kubeconfigs"},
		Data:       map[string][]byte{coreKey: []byte("in-cluster-default")},
	}
}

func clusterKubeconfigDeps(t *testing.T, withCA bool, log *[]tokenRequestLog) (Deps, client.Client) {
	t.Helper()
	core := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(existingClusterKubeconfigs()).Build()
	ips := map[string]string{"k3d-loopruntime-build-server-0": "172.18.0.5", "k3d-loopruntime-cell-0-server-0": "172.18.0.7"}
	return Deps{
		Ctx:  context.Background(),
		Kube: NewKubeClient(core),
		Clients: map[string]KubeClient{
			"k3d-loopruntime-build":  NewKubeClient(peerClusterClient(t, "argo", "loopruntime-workflow-submitter", withCA, log)),
			"k3d-loopruntime-cell-0": NewKubeClient(peerClusterClient(t, "kube-system", "default", withCA, log)),
		},
		Host: Host{DockerNetworkIP: func(network, container string) (string, error) {
			if network != "k3d-loopruntime" {
				return "", errors.New("unexpected network " + network)
			}
			ip, ok := ips[container]
			if !ok {
				return "", errors.New("no container " + container)
			}
			return ip, nil
		}},
	}, core
}

func readClusterKubeconfigs(t *testing.T, c client.Client) *corev1.Secret {
	t.Helper()
	var s corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopruntime-system", "loopruntime-cluster-kubeconfigs"), &s); err != nil {
		t.Fatal(err)
	}
	return &s
}

func TestClusterKubeconfigs_WritesATokenKubeconfigPerPeerCluster(t *testing.T) {
	var log []tokenRequestLog
	deps, core := clusterKubeconfigDeps(t, true, &log)

	if got := NewClusterKubeconfigsSeed().Run(deps); got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}

	s := readClusterKubeconfigs(t, core)
	for key, server := range map[string]string{
		buildKey: "https://172.18.0.5:6443",
		cell0Key: "https://172.18.0.7:6443",
	} {
		cfg, err := clientcmd.Load(s.Data[key])
		if err != nil {
			t.Fatalf("%s is not a kubeconfig: %v", key, err)
		}
		ctx := cfg.Contexts[cfg.CurrentContext]
		if ctx == nil {
			t.Fatalf("%s has no current context", key)
		}
		cluster, user := cfg.Clusters[ctx.Cluster], cfg.AuthInfos[ctx.AuthInfo]
		if cluster.Server != server {
			t.Errorf("%s server = %q, want %q", key, cluster.Server, server)
		}
		if string(cluster.CertificateAuthorityData) != testCAPEM {
			t.Errorf("%s CA = %q, want the peer's live kube-root-ca.crt", key, cluster.CertificateAuthorityData)
		}
		if cluster.InsecureSkipTLSVerify {
			t.Errorf("%s skips TLS verification", key)
		}
		if user.Token != "fake-token" {
			t.Errorf("%s token = %q, want the minted token", key, user.Token)
		}
	}
	// loopruntime-api runs in core and reaches it in-cluster; the seed leaves that key alone.
	if string(s.Data[coreKey]) != "in-cluster-default" {
		t.Errorf("%s = %q, want it untouched", coreKey, s.Data[coreKey])
	}
}

// Sprint 7 (#36): the build cluster ties `create workflows` to
// argo/loopruntime-workflow-submitter, and loopruntime-api watches only the argo namespace
// for that reason. #242: a 24h token expired between re-seeds on the always-on
// droplet and broke every deploy.
func TestClusterKubeconfigs_MintsTheScopedBuildIdentityForThirtyDays(t *testing.T) {
	var log []tokenRequestLog
	deps, _ := clusterKubeconfigDeps(t, true, &log)

	if got := NewClusterKubeconfigsSeed().Run(deps); got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}

	want := map[string]bool{"argo/loopruntime-workflow-submitter": false, "kube-system/default": false}
	for _, r := range log {
		id := r.namespace + "/" + r.name
		if _, ok := want[id]; !ok {
			t.Errorf("minted a token for unexpected ServiceAccount %s", id)
			continue
		}
		want[id] = true
		if r.seconds != int64((720 * time.Hour).Seconds()) {
			t.Errorf("%s token TTL = %ds, want 720h", id, r.seconds)
		}
	}
	for id, minted := range want {
		if !minted {
			t.Errorf("no token minted for %s", id)
		}
	}
}

func TestClusterKubeconfigs_FailsWithoutThePeersLiveCA(t *testing.T) {
	var log []tokenRequestLog
	deps, core := clusterKubeconfigDeps(t, false, &log)

	if got := NewClusterKubeconfigsSeed().Run(deps); got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if _, ok := readClusterKubeconfigs(t, core).Data[buildKey]; ok {
		t.Errorf("wrote %s although the CA could not be read", buildKey)
	}
}

func TestClusterKubeconfigs_FailsWhenAPeerHasNoAddress(t *testing.T) {
	var log []tokenRequestLog
	deps, _ := clusterKubeconfigDeps(t, true, &log)
	deps.Host.DockerNetworkIP = func(string, string) (string, error) { return "", errors.New("no such container") }

	if got := NewClusterKubeconfigsSeed().Run(deps); got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestClusterKubeconfigs_DryRunMintsNothing(t *testing.T) {
	var log []tokenRequestLog
	deps, core := clusterKubeconfigDeps(t, true, &log)
	deps.DryRun = true

	if got := NewClusterKubeconfigsSeed().Run(deps); got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if len(log) != 0 {
		t.Errorf("dry-run minted %d token(s)", len(log))
	}
	if _, ok := readClusterKubeconfigs(t, core).Data[buildKey]; ok {
		t.Errorf("dry-run wrote %s", buildKey)
	}
}

func TestClusterKubeconfigs_VerifyPassesAfterRun(t *testing.T) {
	var log []tokenRequestLog
	deps, _ := clusterKubeconfigDeps(t, true, &log)
	seed := NewClusterKubeconfigsSeed()
	if got := seed.Run(deps); got.Verdict != PASS {
		t.Fatalf("Run: %s (%s)", got.Verdict, got.Reason)
	}
	if got := seed.Verify(deps); got.Verdict != PASS {
		t.Fatalf("Verify = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

// The value generate-secrets.sh installs on a fresh bootstrap points at
// kubernetes.default.svc — the core cluster, not the peer.
func TestClusterKubeconfigs_VerifyFailsOnTheInClusterPlaceholder(t *testing.T) {
	var log []tokenRequestLog
	deps, core := clusterKubeconfigDeps(t, true, &log)
	placeholder := []byte("apiVersion: v1\nkind: Config\nclusters:\n- name: in-cluster\n  cluster:\n    server: https://kubernetes.default.svc\n" +
		"contexts:\n- name: in-cluster\n  context: {cluster: in-cluster, user: in-cluster}\ncurrent-context: in-cluster\n" +
		"users:\n- name: in-cluster\n  user: {tokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token}\n")
	s := readClusterKubeconfigs(t, core)
	s.Data[buildKey] = placeholder
	s.Data[cell0Key] = placeholder
	if err := core.Update(context.Background(), s); err != nil {
		t.Fatal(err)
	}

	if got := NewClusterKubeconfigsSeed().Verify(deps); got.Verdict != FAIL {
		t.Fatalf("Verify = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestClusterKubeconfigs_Name(t *testing.T) {
	if got := NewClusterKubeconfigsSeed().Name(); got != "cluster-kubeconfigs" {
		t.Errorf("Name = %q", got)
	}
}
