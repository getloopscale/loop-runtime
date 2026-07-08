// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"testing"

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
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

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
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

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
