// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// These tests cover the paths where a seed used to drop an error and report
// PASS (error-handling.md: never swallow; fail fast, fail loud).

// A transient read error is not "the Secret is absent": regenerating on it
// silently rotated the broker token on any API hiccup.
func TestBrokerAcquireCellTokens_DoesNotRotateOnTransientReadError(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			return errors.New("etcdserver: request timed out")
		},
	}).Build()
	_, _, err := brokerAcquireCellTokens(Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}, []string{"cell-0"})
	if err == nil {
		t.Fatal("a transient read error must surface, not trigger a new token")
	}
}

// settledDeploymentStatus is the status of a one-replica Deployment whose
// rollout has completed — what a happy-path fixture must present now that a
// seed fails when the workload it restarted never becomes ready.
var settledDeploymentStatus = appsv1.DeploymentStatus{ReadyReplicas: 1, UpdatedReplicas: 1}

func neverReadyDeployment(ns, name string) *appsv1.Deployment {
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 0},
	}
}

func TestNetworkingCorednsSeed_FailsWhenCorednsNeverSettles(t *testing.T) {
	var host Host
	host.DockerContainerIP = func(string) (string, error) { return "172.20.0.10", nil }
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "coredns"},
		Data:       map[string]string{"NodeHosts": ""},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(cm, neverReadyDeployment("kube-system", "coredns")).Build()
	deps := Deps{Host: host, RolloutSettleTimeout: time.Millisecond, Ctx: context.Background(), Kube: NewKubeClient(c), CtrlClient: c}

	got := NewNetworkingCrossClusterCorednsSeed().Run(deps)
	if got.Verdict != FAIL || !strings.Contains(got.Reason, "coredns") {
		t.Fatalf("Verdict = %s (%s), want FAIL naming coredns", got.Verdict, got.Reason)
	}
}

func TestDiscoverCorednsHosts_SkipsAbsentContainers(t *testing.T) {
	var host Host
	host.DockerContainerIP = func(c string) (string, error) {
		if c == "k3d-loop-runtime-build-serverlb" {
			return "172.20.0.10", nil
		}
		return "", errContainerAbsent
	}
	hosts, err := discoverCorednsHosts(host)
	if err != nil {
		t.Fatalf("an absent container is a skip, not an error: %v", err)
	}
	if hosts["k3d-loop-runtime-build-serverlb"] != "172.20.0.10" || len(hosts) != 1 {
		t.Errorf("hosts = %v", hosts)
	}
}

func TestDiscoverCorednsHosts_PropagatesDockerFailures(t *testing.T) {
	var host Host
	host.DockerContainerIP = func(string) (string, error) {
		return "", errors.New("Cannot connect to the Docker daemon")
	}
	if _, err := discoverCorednsHosts(host); err == nil {
		t.Fatal("a docker daemon failure must not read as 'no containers'")
	}
}

// A malformed base URL used to yield a nil request (the NewRequest error was
// discarded), which panicked on the first header write instead of failing.
func TestHTTPHelpers_ReturnAnErrorOnAMalformedBaseURL(t *testing.T) {
	const bad = "http://bad host\x7f"
	ctx := context.Background()
	cases := map[string]func() error{
		"giteaProbeAuth":          func() error { return giteaProbeAuth(ctx, bad, "u", "p") },
		"giteaEnsureOrg":          func() error { return giteaEnsureOrg(ctx, bad, "u", "p", "o") },
		"giteaDeleteLegacyTokens": func() error { return giteaDeleteLegacyTokens(ctx, bad, "u", "p") },
		"giteaMintToken": func() error {
			_, err := giteaMintToken(ctx, bad, "u", "p", "n", nil)
			return err
		},
		"argocdSessionLogin": func() error { _, err := argocdSessionLogin(ctx, bad, "u", "p"); return err },
		"argocdMintAccountToken": func() error {
			_, err := argocdMintAccountToken(ctx, bad, "s", "a", "n")
			return err
		},
		"openBaoReadRoleID": func() error { _, err := openBaoReadRoleID(ctx, bad, "t"); return err },
		"openBaoKVPut":      func() error { return openBaoKVPut(ctx, bad, "t", "p", map[string]string{"k": "v"}) },
		"openBaoKVExists":   func() error { _, err := openBaoKVExists(ctx, bad, "t", "p"); return err },
		"adminHydraGetClient": func() error {
			_, err := adminHydraGetClient(ctx, bad, "c")
			return err
		},
		"adminHydraRegisterOrRefresh": func() error {
			_, err := adminHydraRegisterOrRefresh(ctx, bad, "c", "/nonexistent")
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked instead of returning an error: %v", r)
				}
			}()
			if err := call(); err == nil {
				t.Fatal("want an error for a malformed base URL")
			}
		})
	}
}

// The Stripe helpers used http.DefaultClient, which has no timeout: a hung
// connection hung the whole seed chain.
func TestStripeHTTPClient_HasATimeout(t *testing.T) {
	if (Host{}).stripeHTTP().Timeout <= 0 {
		t.Fatal("stripe HTTP client must bound every request")
	}
}
