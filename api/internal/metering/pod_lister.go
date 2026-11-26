// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metering

// pod_lister.go — the concrete PodListerFactory (plan metering-compute-loop T3.2,
// slice S3b). Wraps a cell's read-scoped Kubernetes client (resolved in
// production by SecretCellClientProvider) and lists the pods in a namespace,
// mapping them to []corev1.Pod for the pure sumComputeRequests math. The cell
// client is resolved behind the narrow CellClientProvider interface (DIP) so
// the factory is unit-testable with a fake.

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CellClientProvider resolves a cell's read-scoped Kubernetes client. Satisfied
// in production by SecretCellClientProvider, which resolves the cell's
// ClusterCredential by spec.cellID; the interface is the DIP seam that lets the
// factory be tested without a live provider.
type CellClientProvider interface {
	ClientFor(ctx context.Context, cellID string) (kubernetes.Interface, error)
	// Invalidate drops a cached client whose credential the cell rejected.
	Invalidate(cellID string)
}

// cellPodLister lists pods on ONE cell via its Kubernetes client. It implements
// PodLister; sumComputeRequests owns all filtering (EC-2 pod-phase / scheduled),
// so this lister returns EVERY pod in the namespace — a client-side selector here
// would duplicate that logic (DRY) and phase field-selectors are unreliable.
type cellPodLister struct {
	client kubernetes.Interface
	// invalidate drops this cell's cached client when the cell rejects its
	// credential, so the next pass rebuilds it from the current Secret.
	invalidate func()
}

// ListPods returns every pod in the namespace as []corev1.Pod. A List failure is
// wrapped and propagated (SC-3: a read failure is NEVER swallowed to an empty
// slice — that would advance the cursor over an unmeasured interval). EC-9:
// single-page List; the tenant namespace pod count is small, so paging (limit /
// continue) is deferred to the ADR-11 informer migration keyed off the
// loopruntime_metering_pass_duration_seconds ceiling.
func (l *cellPodLister) ListPods(ctx context.Context, namespace string) ([]corev1.Pod, error) {
	list, err := l.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err) {
			l.invalidate()
		}
		return nil, fmt.Errorf("metering: list pods in namespace %q: %w", namespace, err)
	}
	return list.Items, nil
}

// CellPodListerFactory produces a PodLister bound to a specific cell's client.
type CellPodListerFactory struct {
	clients CellClientProvider
}

// NewCellPodListerFactory wraps the cell-client provider — in production,
// SecretCellClientProvider.
func NewCellPodListerFactory(clients CellClientProvider) *CellPodListerFactory {
	return &CellPodListerFactory{clients: clients}
}

// PodListerForCell resolves the cell's read-scoped client and returns a PodLister
// over it. A cell-client resolution failure (nonexistent / unreachable cell) is a
// typed error — never a silent empty lister that would meter zero and advance the
// cursor.
func (f *CellPodListerFactory) PodListerForCell(ctx context.Context, cellID string) (PodLister, error) {
	client, err := f.clients.ClientFor(ctx, cellID)
	if err != nil {
		return nil, fmt.Errorf("metering: resolve client for cell %q: %w", cellID, err)
	}
	return &cellPodLister{client: client, invalidate: func() { f.clients.Invalidate(cellID) }}, nil
}
