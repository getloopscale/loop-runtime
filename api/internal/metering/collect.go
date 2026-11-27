// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metering

// collect.go — the pure Σ effective-request computation for the metering worker
// (plan metering-compute-loop T3.1). Allocation-based: we bill the RESERVATION
// (requests), not live usage. The per-pod effective request uses the canonical
// K8s computation (component-helpers/resource.PodRequests) so the subtle
// init/sidecar/regular math — max(max(init), Σregular + Σnative-sidecar) — is
// correct rather than hand-rolled (a wrong formula is a money bug).

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/component-helpers/resource"
)

// PodLister lists the pods in a namespace on a cell — implemented over the cell
// client's .CoreV1().Pods(ns).List, fakeable in tests. Kept narrow (DIP) so the
// worker's collect logic is unit-testable without a live cluster.
type PodLister interface {
	ListPods(ctx context.Context, namespace string) ([]corev1.Pod, error)
}

// collectNamespaces lists the pods across all the given namespaces (a tenant's
// (project,env) namespaces on a cell) and returns the Σ effective request.
// SC-3: a list failure on ANY namespace short-circuits with a non-nil error and
// ZERO totals — the caller MUST NOT meter. A read failure is NEVER swallowed to
// Σreq=0 (that would advance the cursor over an unmeasured interval → silent
// under-bill, the MEET-003 hole; Rule 8: a typed error, never a zero).
func collectNamespaces(ctx context.Context, lister PodLister, namespaces []string) (cpuMillicores, memBytes int64, err error) {
	var pods []corev1.Pod
	for _, ns := range namespaces {
		got, listErr := lister.ListPods(ctx, ns)
		if listErr != nil {
			return 0, 0, fmt.Errorf("metering: list pods in %q: %w", ns, listErr)
		}
		pods = append(pods, got...)
	}
	cpuMillicores, memBytes = sumComputeRequests(pods)
	return cpuMillicores, memBytes, nil
}

// sumComputeRequests returns the Σ effective CPU (millicores) + memory (bytes)
// reservation across the given pods. EC-2: a terminal pod (Succeeded/Failed) or
// an unscheduled pod (no NodeName) reserves nothing and is skipped.
func sumComputeRequests(pods []corev1.Pod) (cpuMillicores, memBytes int64) {
	for i := range pods {
		p := &pods[i]
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		if p.Spec.NodeName == "" {
			continue
		}
		req := resource.PodRequests(p, resource.PodResourcesOptions{})
		cpuMillicores += req.Cpu().MilliValue()
		memBytes += req.Memory().Value()
	}
	return cpuMillicores, memBytes
}
