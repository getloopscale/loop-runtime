// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestParseNodeHostsBlock(t *testing.T) {
	block := "10.0.0.1 host-a\n10.0.0.2 host-b\n\n# comment\n"
	got := parseNodeHostsBlock(block)
	if got["host-a"] != "10.0.0.1" || got["host-b"] != "10.0.0.2" {
		t.Errorf("parse: got %v", got)
	}
}

func TestMergeCorednsNodeHosts_OverridesTarget(t *testing.T) {
	existing := "10.0.0.1 host-a\n10.0.0.2 host-old\n"
	target := "192.168.0.1 host-b\n"
	got := mergeCorednsNodeHosts(existing, target)
	if !strings.Contains(got, "10.0.0.1 host-a") {
		t.Errorf("preserved host-a: %q", got)
	}
	if !strings.Contains(got, "192.168.0.1 host-b") {
		t.Errorf("added host-b: %q", got)
	}
	if !strings.Contains(got, "10.0.0.2 host-old") {
		t.Errorf("preserved host-old: %q", got)
	}
}

func TestMergeCorednsNodeHosts_UpdatesExisting(t *testing.T) {
	existing := "10.0.0.1 host-a\n10.0.0.2 host-target\n"
	target := "192.168.0.1 host-target\n"
	got := mergeCorednsNodeHosts(existing, target)
	if strings.Contains(got, "10.0.0.2 host-target") {
		t.Errorf("stale IP for host-target not overwritten: %q", got)
	}
	if !strings.Contains(got, "192.168.0.1 host-target") {
		t.Errorf("fresh IP for host-target absent: %q", got)
	}
}

func TestRenderCorednsNodeHosts_SortedDeterministic(t *testing.T) {
	got := renderCorednsNodeHosts(map[string]string{
		"host-c": "10.0.0.3", "host-a": "10.0.0.1", "host-b": "10.0.0.2",
	})
	want := "10.0.0.1 host-a\n10.0.0.2 host-b\n10.0.0.3 host-c\n"
	if got != want {
		t.Errorf("render:\ngot  %q\nwant %q", got, want)
	}
}

func TestNetworkingCorednsSeed_HappyPath(t *testing.T) {
	var host Host
	host.DockerContainerIP = func(container string) (string, error) {
		switch container {
		case "k3d-loopzero-build-serverlb":
			return "172.20.0.10", nil
		case "k3d-loopzero-cell-0-server-0":
			return "172.20.0.20", nil
		case "k3d-loopzero-secrets-server-0":
			return "172.20.0.30", nil
		default:
			return "", nil
		}
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "coredns"},
		Data:       map[string]string{"NodeHosts": "127.0.0.1 preexisting-alias\n"},
	}
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "coredns"}, Status: settledDeploymentStatus}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm, dep).Build()
	deps := Deps{Host: host, RolloutSettleTimeout: time.Millisecond, Ctx: context.Background(), Kube: NewKubeClient(c), CtrlClient: c}

	got := NewNetworkingCrossClusterCorednsSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	var updated corev1.ConfigMap
	_ = c.Get(context.Background(), fakeKey("kube-system", "coredns"), &updated)
	block := updated.Data["NodeHosts"]
	for _, want := range []string{
		"172.20.0.10 k3d-loopzero-build-serverlb",
		"172.20.0.20 k3d-loopzero-cell-0-server-0",
		"172.20.0.20 k3d-loopzero-cell-0-serverlb", // alias
		"172.20.0.30 k3d-loopzero-secrets-server-0",
		"172.20.0.30 k3d-loopzero-secrets-serverlb", // alias
		"127.0.0.1 preexisting-alias",           // preserved
	} {
		if !strings.Contains(block, want) {
			t.Errorf("NodeHosts missing %q\nGot:\n%s", want, block)
		}
	}
}

func TestNetworkingCorednsSeed_NoopWhenCanonical(t *testing.T) {
	var host Host
	host.DockerContainerIP = func(c string) (string, error) { return "10.0.0.1", nil }
	// Pre-seed with the exact target block.
	canonical := renderCorednsNodeHosts(map[string]string{
		"k3d-loopzero-build-serverlb":   "10.0.0.1",
		"k3d-loopzero-cell-0-server-0":  "10.0.0.1",
		"k3d-loopzero-cell-0-serverlb":  "10.0.0.1",
		"k3d-loopzero-secrets-server-0": "10.0.0.1",
		"k3d-loopzero-secrets-serverlb": "10.0.0.1",
	})
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "coredns"},
		Data:       map[string]string{"NodeHosts": canonical},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).Build()
	deps := Deps{Host: host, RolloutSettleTimeout: time.Millisecond, Ctx: context.Background(), Kube: NewKubeClient(c), CtrlClient: c}

	got := NewNetworkingCrossClusterCorednsSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "no diff") {
		t.Errorf("Reason = %q, want 'no diff'", got.Reason)
	}
}

func TestNetworkingCorednsSeed_FailWhenNoContainers(t *testing.T) {
	var host Host
	host.DockerContainerIP = func(container string) (string, error) {
		return "", errContainerAbsent // every container absent
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "coredns"},
		Data:       map[string]string{"NodeHosts": ""},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).Build()
	deps := Deps{Host: host, RolloutSettleTimeout: time.Millisecond, Ctx: context.Background(), Kube: NewKubeClient(c), CtrlClient: c}

	got := NewNetworkingCrossClusterCorednsSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestNetworkingCorednsSeed_Name(t *testing.T) {
	if got := NewNetworkingCrossClusterCorednsSeed().Name(); got != "networking-cross-cluster-coredns" {
		t.Errorf("Name = %q, want networking-cross-cluster-coredns", got)
	}
}
