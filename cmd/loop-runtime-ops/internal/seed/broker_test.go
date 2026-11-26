// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBrokerMgmtSecret_HasBothKeys(t *testing.T) {
	got := brokerMgmtSecret(map[string]string{"cell-0": "t"}, []string{"cell-0"})
	if string(got.Data["token"]) != "t" || string(got.Data["LOOP_RUNTIME_BROKER_TOKEN"]) != "t" {
		t.Errorf("mgmt Secret missing keys: %v", got.Data)
	}
}

func TestBrokerWorkloadSecret_HasCanonicalLabel(t *testing.T) {
	got := brokerWorkloadSecret([]byte("t"))
	if got.Labels["external-secrets.io/type"] != "webhook" {
		t.Errorf("workload Secret missing canonical webhook label: %v", got.Labels)
	}
}

func TestBrokerSeed_Name(t *testing.T) {
	if got := NewBrokerSeed().Name(); got != "broker" {
		t.Errorf("Name = %q, want broker", got)
	}
}

func TestBrokerSeed_Run_DryRunReports(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(cellScheme(t)).WithObjects(clusterCredential("cell-0", "cell-0")).Build()
	deps := Deps{
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(c),
		CtrlClient:           c,
		DryRun:               true,
	}
	got := NewBrokerSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if got.Reason == "" {
		t.Error("empty reason on dry-run")
	}
}

func TestBrokerSeed_Run_FailsWhenClientCtorMissing(t *testing.T) {
	// mgmt Kube present but no ClientCtor → workload resolution fails at Layer 2.
	c := fake.NewClientBuilder().WithScheme(cellScheme(t)).WithObjects(clusterCredential("cell-0", "cell-0")).Build()
	deps := Deps{RolloutSettleTimeout: time.Millisecond, Ctx: context.Background(), Kube: NewKubeClient(c), CtrlClient: c}
	got := NewBrokerSeed().Run(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	// Must fail no later than Layer 2 (cell resolve).
	if !stringContainsAny(got.Reason, "Layer 2") {
		t.Errorf("Reason = %q, want Layer 2 failure marker", got.Reason)
	}
}

func TestBrokerSeed_Verify_FailWhenMgmtSecretAbsent(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{RolloutSettleTimeout: time.Millisecond, Ctx: context.Background(), Kube: NewKubeClient(c)}
	got := NewBrokerSeed().Verify(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestBrokerSeed_Verify_FailWhenCanonicalKeyMissing(t *testing.T) {
	// Secret present with only the legacy `token` key: neither LOOP_RUNTIME_BROKER_TOKEN nor
	// LOOP_RUNTIME_BROKER_TOKENS reaches loop-runtime-api.
	legacy := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: brokerSecretName},
		Data:       map[string][]byte{brokerTokenKey: []byte("legacy-token-no-canonical")},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(legacy).Build()
	deps := Deps{RolloutSettleTimeout: time.Millisecond, Ctx: context.Background(), Kube: NewKubeClient(c)}
	got := NewBrokerSeed().Verify(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !stringContainsAny(got.Reason, "LOOP_RUNTIME_BROKER_TOKEN") {
		t.Errorf("Reason = %q, want LOOP_RUNTIME_BROKER_TOKEN marker", got.Reason)
	}
}

// stringContainsAny returns true when any of subs is present in s.
func stringContainsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// The broker's release is the cell helmfile's (release loop-runtime-broker); the seed writes
// the bearer token into the two Secrets and restarts loop-runtime-api, and nothing else. The
// Host is left zero on purpose: it no longer carries a docker or helm hook, so a seed
// that reached for either would not compile, and this run proves the remaining layers
// converge without them (principle_violations#67).
func TestBrokerSeed_Run_SeedsTheTokenWithoutInstallingTheRelease(t *testing.T) {
	mgmt := fake.NewClientBuilder().WithScheme(cellScheme(t)).
		WithObjects(makeLoopRuntimeAPIDeploymentWithEnv(nil), clusterCredential("cell-0", "cell-0")).Build()
	workload := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(mgmt),
		CtrlClient:           mgmt,
		ClientCtor:           func(string) (KubeClient, error) { return NewKubeClient(workload), nil },
	}

	got := NewBrokerSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if _, err := NewKubeClient(workload).GetSecret(deps.Ctx, "external-secrets", "loop-runtime-broker-token"); err != nil {
		t.Errorf("the cell's token Secret was not written: %v", err)
	}
	if _, err := NewKubeClient(mgmt).GetSecret(deps.Ctx, "loop-runtime-system", "loop-runtime-broker-token"); err != nil {
		t.Errorf("the core's token Secret was not written: %v", err)
	}
}
