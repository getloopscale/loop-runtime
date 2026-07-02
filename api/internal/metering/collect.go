// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

// collect.go — the pure Σ effective-request computation for the metering worker
// (plan metering-compute-loop T3.1). Allocation-based: we bill the RESERVATION
// (requests), not live usage. The per-pod effective request uses the canonical
// K8s computation (component-helpers/resource.PodRequests) so the subtle
// init/sidecar/regular math — max(max(init), Σregular + Σnative-sidecar) — is
// correct rather than hand-rolled (a wrong formula is a money bug).

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/component-helpers/resource"
)

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
