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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// The broker serves every cell with its own token (#456): the seed gives each cell's
// External Secrets a token of its own, and lists every pair for loop-runtime-api. It used to write
// one token into cell-0 only, so no other cell's External Secrets could authenticate.

// clusterCredential is a ClusterCredential as the core cluster holds it.
func clusterCredential(name, cellID string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(clusterCredentialGVK)
	u.SetName(name)
	_ = unstructured.SetNestedField(u.Object, cellID, "spec", "cellID")
	return u
}

func cellScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := newScheme(t)
	s.AddKnownTypeWithName(clusterCredentialGVK, &unstructured.Unstructured{})
	list := clusterCredentialGVK
	list.Kind += "List"
	s.AddKnownTypeWithName(list, &unstructured.UnstructuredList{})
	return s
}

type cellHarness struct {
	deps  Deps
	mgmt  client.Client
	cells map[string]client.Client // kube context → client
}

func newCellHarness(t *testing.T, existing []client.Object, cellIDs ...string) cellHarness {
	t.Helper()
	objs := append([]client.Object{makeLoopRuntimeAPIDeploymentWithEnv(nil)}, existing...)
	for _, id := range cellIDs {
		objs = append(objs, clusterCredential(id, id))
	}
	mgmt := fake.NewClientBuilder().WithScheme(cellScheme(t)).WithObjects(objs...).Build()
	h := cellHarness{mgmt: mgmt, cells: map[string]client.Client{}}
	for _, id := range cellIDs {
		h.cells[brokerCellContext(id)] = fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	}
	h.deps = Deps{
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(mgmt),
		CtrlClient:           mgmt,
		ClientCtor: func(ctx string) (KubeClient, error) {
			c, ok := h.cells[ctx]
			if !ok {
				t.Fatalf("seed asked for context %q, which no cell maps to", ctx)
			}
			return NewKubeClient(c), nil
		},
	}
	return h
}

func (h cellHarness) cellToken(t *testing.T, cellID string) string {
	t.Helper()
	sec, err := NewKubeClient(h.cells[brokerCellContext(cellID)]).GetSecret(h.deps.Ctx, brokerESONamespace, brokerSecretName)
	if err != nil {
		t.Fatalf("%s has no broker Secret: %v", cellID, err)
	}
	return string(sec.Data[brokerTokenKey])
}

func (h cellHarness) coreSecret(t *testing.T) *corev1.Secret {
	t.Helper()
	sec, err := NewKubeClient(h.mgmt).GetSecret(h.deps.Ctx, "loop-runtime-system", brokerSecretName)
	if err != nil {
		t.Fatal(err)
	}
	return sec
}

func TestBrokerSeed_GivesEachCellItsOwnToken(t *testing.T) {
	h := newCellHarness(t, nil, "cell-0", "cell-1")

	got := NewBrokerSeed().Run(h.deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s)", got.Verdict, got.Reason)
	}
	t0, t1 := h.cellToken(t, "cell-0"), h.cellToken(t, "cell-1")
	if t0 == "" || t1 == "" || t0 == t1 {
		t.Fatalf("cell tokens must be present and distinct (equal=%v)", t0 == t1)
	}
	core := h.coreSecret(t)
	if want := "cell-0=" + t0 + ",cell-1=" + t1; string(core.Data[brokerCellTokensEnvKey]) != want {
		t.Errorf("LOOP_RUNTIME_BROKER_TOKENS does not list each cell with the token its External Secrets holds")
	}
	if string(core.Data[brokerCanonicalEnvKey]) != t0 {
		t.Errorf("LOOP_RUNTIME_BROKER_TOKEN is not the default cell's token")
	}
}

// Re-running never rotates a token a cell already holds, including the single token the
// seed wrote before tokens were per cell — that one stays the default cell's.
func TestBrokerSeed_KeepsTheTokensAlreadyIssued(t *testing.T) {
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: brokerSecretName},
		Data: map[string][]byte{
			brokerTokenKey:         []byte("legacy-cell-0"),
			brokerCellTokensEnvKey: []byte("cell-1=kept-cell-1"),
		},
	}
	h := newCellHarness(t, []client.Object{existing}, "cell-0", "cell-1")

	if got := NewBrokerSeed().Run(h.deps); got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s)", got.Verdict, got.Reason)
	}

	if h.cellToken(t, "cell-0") != "legacy-cell-0" || h.cellToken(t, "cell-1") != "kept-cell-1" {
		t.Error("a token already issued was rotated")
	}
}

// A core with no ClusterCredential has no cell to serve; writing a token nowhere would
// report success for a broker no cell can reach.
func TestBrokerSeed_FailsWhenNoCellIsRegistered(t *testing.T) {
	h := newCellHarness(t, nil)

	got := NewBrokerSeed().Run(h.deps)

	if got.Verdict != FAIL || !strings.Contains(got.Reason, "ClusterCredential") {
		t.Fatalf("Verdict = %s (%s), want FAIL naming ClusterCredential", got.Verdict, got.Reason)
	}
}

// Verify compares every cell's token with the pair loop-runtime-api holds for it: a cell whose
// External Secrets holds another cell's token, or a stale one, is refused.
func TestBrokerSeed_VerifyRefusesACellWhoseTokenDoesNotMatch(t *testing.T) {
	h := newCellHarness(t, nil, "cell-0", "cell-1")
	if got := NewBrokerSeed().Run(h.deps); got.Verdict != PASS {
		t.Fatalf("Run: %s (%s)", got.Verdict, got.Reason)
	}
	if got := NewBrokerSeed().Verify(h.deps); got.Verdict != PASS {
		t.Fatalf("Verify after Run = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	stale := brokerWorkloadSecret([]byte("stale"))
	if _, err := NewKubeClient(h.cells[brokerCellContext("cell-1")]).CreateOrUpdateSecret(h.deps.Ctx, stale); err != nil {
		t.Fatal(err)
	}

	got := NewBrokerSeed().Verify(h.deps)

	if got.Verdict != FAIL || !strings.Contains(got.Reason, "cell-1") {
		t.Errorf("Verify = %s (%s), want FAIL naming cell-1", got.Verdict, got.Reason)
	}
}

// On an environment whose kubeconfig names each cell's context after the cell id
// (staging's EKS clusters: cell-0, cell-1), the seed writes each cell's token through
// that context instead of the k3d one, which does not exist there.
func TestBrokerSeed_WritesThroughTheCellContextsTheEnvironmentNames(t *testing.T) {
	mgmt := fake.NewClientBuilder().WithScheme(cellScheme(t)).WithObjects(
		makeLoopRuntimeAPIDeploymentWithEnv(nil), clusterCredential("cell-0", "cell-0"),
	).Build()
	cell0 := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	noPrefix := ""
	deps := Deps{
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(mgmt),
		CtrlClient:           mgmt,
		CellContextPrefix:    &noPrefix,
		ClientCtor: func(ctx string) (KubeClient, error) {
			if ctx != "cell-0" {
				t.Fatalf("seed asked for context %q, want cell-0", ctx)
			}
			return NewKubeClient(cell0), nil
		},
	}

	if got := NewBrokerSeed().Run(deps); got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s)", got.Verdict, got.Reason)
	}

	sec, err := NewKubeClient(cell0).GetSecret(deps.Ctx, brokerESONamespace, brokerSecretName)
	if err != nil || len(sec.Data[brokerTokenKey]) == 0 {
		t.Fatalf("cell-0 holds no broker token (err=%v)", err)
	}
}
