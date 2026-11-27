// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metering

// pod_lister_test.go — the concrete PodListerFactory adapter (plan
// metering-compute-loop T3.2, slice S3b). Unit-tests the client→[]corev1.Pod
// mapping with client-go's fake clientset (a real boundary fake, no live
// cluster). Cell-client resolution itself is not exercised here.

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func podInNS(name, ns string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
}

// fakeCellClientProvider stands in for the production CellClientProvider —
// returns a canned client (or an error) so the factory is testable without a
// live provider.
type fakeCellClientProvider struct {
	client      kubernetes.Interface
	err         error
	invalidated []string
}

func (f *fakeCellClientProvider) ClientFor(ctx context.Context, cellID string) (kubernetes.Interface, error) {
	return f.client, f.err
}

func (f *fakeCellClientProvider) Invalidate(cellID string) {
	f.invalidated = append(f.invalidated, cellID)
}

func TestCellPodLister_SatisfiesInterfaces(t *testing.T) {
	var _ PodLister = (*cellPodLister)(nil)
	var _ PodListerFactory = (*CellPodListerFactory)(nil)
	var _ CellClientProvider = (*fakeCellClientProvider)(nil)
}

// TestCellPodLister_ListPods_ReturnsOnlyNamespacePods — ListPods(ns) returns the
// pods in THAT namespace as []corev1.Pod (the namespace scope is the K8s server's,
// not a client-side filter).
func TestCellPodLister_ListPods_ReturnsOnlyNamespacePods(t *testing.T) {
	cs := fake.NewSimpleClientset(
		podInNS("a1", "ns-a"), podInNS("a2", "ns-a"), podInNS("b1", "ns-b"),
	)
	lister := &cellPodLister{client: cs, invalidate: func() {}}

	pods, err := lister.ListPods(context.Background(), "ns-a")
	if err != nil {
		t.Fatalf("ListPods: %v", err)
	}
	if len(pods) != 2 {
		t.Fatalf("got %d pods, want 2 (only ns-a)", len(pods))
	}
	for _, p := range pods {
		if p.Namespace != "ns-a" {
			t.Errorf("pod %s in ns %q, want ns-a", p.Name, p.Namespace)
		}
	}
}

// TestCellPodLister_ListPods_EmptyNamespace_EmptyNotError — a genuinely empty
// namespace returns an empty slice + nil error (EC-1 scale-to-zero: zero pods is
// a valid Σreq=0, NOT a read failure).
func TestCellPodLister_ListPods_EmptyNamespace_EmptyNotError(t *testing.T) {
	cs := fake.NewSimpleClientset()
	lister := &cellPodLister{client: cs, invalidate: func() {}}

	pods, err := lister.ListPods(context.Background(), "ns-empty")
	if err != nil {
		t.Fatalf("empty namespace must not error: %v", err)
	}
	if len(pods) != 0 {
		t.Errorf("got %d pods, want 0", len(pods))
	}
}

// TestCellPodLister_ListPods_ListError_Propagated_NotSwallowed — SC-3: a real List
// failure propagates a wrapped error; it is NEVER swallowed to ([]Pod{}, nil)
// (that would advance the cursor over an unmeasured interval → silent under-bill).
func TestCellPodLister_ListPods_ListError_Propagated_NotSwallowed(t *testing.T) {
	boom := errors.New("apiserver unreachable")
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, boom
	})
	lister := &cellPodLister{client: cs, invalidate: func() {}}

	pods, err := lister.ListPods(context.Background(), "ns-a")
	if err == nil {
		t.Fatal("ListPods swallowed a list failure — SC-3 violated (must never return empty+nil on a read error)")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error must wrap the underlying list failure, got %v", err)
	}
	if len(pods) != 0 {
		t.Errorf("got %d pods on error, want 0 (no partial leak)", len(pods))
	}
}

// TestCellPodListerFactory_PodListerForCell — the factory resolves a cell's client
// via the provider and returns a working PodLister over it.
func TestCellPodListerFactory_PodListerForCell(t *testing.T) {
	cs := fake.NewSimpleClientset(podInNS("p", "ns"))
	factory := NewCellPodListerFactory(&fakeCellClientProvider{client: cs})

	lister, err := factory.PodListerForCell(context.Background(), "cell-0")
	if err != nil {
		t.Fatalf("PodListerForCell: %v", err)
	}
	pods, err := lister.ListPods(context.Background(), "ns")
	if err != nil {
		t.Fatalf("ListPods: %v", err)
	}
	if len(pods) != 1 {
		t.Errorf("got %d pods, want 1", len(pods))
	}
}

// TestCellPodListerFactory_PodListerForCell_ProviderError — a cell-client
// resolution failure propagates (a nonexistent/unreachable cell is a typed error,
// not a silent empty lister).
func TestCellPodListerFactory_PodListerForCell_ProviderError(t *testing.T) {
	boom := errors.New("cell not connected")
	factory := NewCellPodListerFactory(&fakeCellClientProvider{err: boom})

	_, err := factory.PodListerForCell(context.Background(), "cell-x")
	if err == nil {
		t.Fatal("PodListerForCell must propagate a cell-client resolution error")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error must wrap the provider failure, got %v", err)
	}
}

// A cell answering 401/403 means the cached credential died (rotated token,
// recreated service account): the lister must drop it so the next pass
// reads the current kubeconfig instead of failing forever.
func TestCellPodLister_AuthFailureInvalidatesTheCellClient(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewUnauthorized("token expired")
	})
	provider := &fakeCellClientProvider{client: cs}
	lister, err := NewCellPodListerFactory(provider).PodListerForCell(context.Background(), "cell-0")
	if err != nil {
		t.Fatalf("PodListerForCell: %v", err)
	}

	_, err = lister.ListPods(context.Background(), "ns")

	if err == nil {
		t.Fatal("ListPods swallowed the auth failure")
	}
	if len(provider.invalidated) != 1 || provider.invalidated[0] != "cell-0" {
		t.Errorf("invalidated = %v, want [cell-0]", provider.invalidated)
	}
}
