// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

func TestBrokerAcquireToken_ReusesExisting(t *testing.T) {
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: brokerSecretName},
		Data:       map[string][]byte{brokerTokenKey: []byte("canonical-token")},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(existing).Build()
	deps := Deps{RolloutSettleTimeout: time.Millisecond, Ctx: context.Background(), Kube: NewKubeClient(c)}

	tok, reused, err := brokerAcquireToken(deps)

	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reused || tok != "canonical-token" {
		t.Errorf("reused=%v tok=%q, want reused=true canonical-token", reused, tok)
	}
}

func TestBrokerAcquireToken_GeneratesWhenAbsent(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{RolloutSettleTimeout: time.Millisecond, Ctx: context.Background(), Kube: NewKubeClient(c)}

	tok, reused, err := brokerAcquireToken(deps)

	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if reused || len(tok) != 64 {
		t.Errorf("reused=%v len(tok)=%d, want reused=false len=64", reused, len(tok))
	}
}

func TestBrokerMgmtSecret_HasBothKeys(t *testing.T) {
	got := brokerMgmtSecret([]byte("t"))
	if string(got.Data["token"]) != "t" || string(got.Data["LOOPZERO_BROKER_TOKEN"]) != "t" {
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
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(c),
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
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{RolloutSettleTimeout: time.Millisecond, Ctx: context.Background(), Kube: NewKubeClient(c)}
	got := NewBrokerSeed().Run(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	// Must fail no later than Layer 2 (workload resolve).
	if !stringContainsAny(got.Reason, "Layer 2", "resolve workload") {
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
	// Secret present but missing LOOPZERO_BROKER_TOKEN key (legacy-only shape).
	legacy := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: brokerSecretName},
		Data:       map[string][]byte{brokerTokenKey: []byte("legacy-token-no-canonical")},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(legacy).Build()
	deps := Deps{RolloutSettleTimeout: time.Millisecond, Ctx: context.Background(), Kube: NewKubeClient(c)}
	got := NewBrokerSeed().Verify(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !stringContainsAny(got.Reason, "LOOPZERO_BROKER_TOKEN") {
		t.Errorf("Reason = %q, want LOOPZERO_BROKER_TOKEN marker", got.Reason)
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

// The broker's release is the cell helmfile's (release loopzero-broker); the seed writes
// the bearer token into the two Secrets and restarts loopzero-api, and nothing else. The
// Host is left zero on purpose: it no longer carries a docker or helm hook, so a seed
// that reached for either would not compile, and this run proves the remaining layers
// converge without them (principle_violations#67).
func TestBrokerSeed_Run_SeedsTheTokenWithoutInstallingTheRelease(t *testing.T) {
	mgmt := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(makeLoopZeroAPIDeploymentWithEnv(nil)).Build()
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
	if _, err := NewKubeClient(workload).GetSecret(deps.Ctx, "external-secrets", "loopzero-broker-token"); err != nil {
		t.Errorf("the cell's token Secret was not written: %v", err)
	}
	if _, err := NewKubeClient(mgmt).GetSecret(deps.Ctx, "loopzero-system", "loopzero-broker-token"); err != nil {
		t.Errorf("the core's token Secret was not written: %v", err)
	}
}
