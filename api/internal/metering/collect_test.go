// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

// collect_test.go — pure Σ-request logic for the metering worker (plan
// metering-compute-loop T3.1). No cluster, no DB — corev1.Pod fixtures only.

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// podWith builds a scheduled Running pod with one container requesting cpuReq
// (e.g. "500m") + memReq (e.g. "1Gi").
func podWith(cpuReq, memReq string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: corev1.PodSpec{
			NodeName: "node-1",
			Containers: []corev1.Container{{
				Name: "app",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(cpuReq),
						corev1.ResourceMemory: resource.MustParse(memReq),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

// TestSumComputeRequests_Basic — one running scheduled pod contributes its
// container CPU (millicores) + memory (bytes) request to the totals.
func TestSumComputeRequests_Basic(t *testing.T) {
	cpu, mem := sumComputeRequests([]corev1.Pod{podWith("500m", "1Gi")})
	if cpu != 500 {
		t.Errorf("cpuMillicores=%d, want 500", cpu)
	}
	if mem != 1<<30 {
		t.Errorf("memBytes=%d, want %d (1Gi)", mem, int64(1<<30))
	}
}

// TestSumComputeRequests_SkipsTerminalAndUnscheduled — EC-2: Succeeded/Failed
// (terminal) pods and unscheduled (no NodeName) pods reserve nothing and are
// skipped; only scheduled non-terminal pods count.
func TestSumComputeRequests_SkipsTerminalAndUnscheduled(t *testing.T) {
	succeeded := podWith("500m", "1Gi")
	succeeded.Status.Phase = corev1.PodSucceeded
	failed := podWith("500m", "1Gi")
	failed.Status.Phase = corev1.PodFailed
	unscheduled := podWith("500m", "1Gi")
	unscheduled.Spec.NodeName = "" // never placed → reserves nothing
	running := podWith("250m", "512Mi")

	cpu, mem := sumComputeRequests([]corev1.Pod{succeeded, failed, unscheduled, running})
	if cpu != 250 {
		t.Errorf("cpuMillicores=%d, want 250 (only the running scheduled pod counts)", cpu)
	}
	if mem != 512<<20 {
		t.Errorf("memBytes=%d, want %d (512Mi)", mem, int64(512<<20))
	}
}

// TestSumComputeRequests_InitAndSidecar — EC-3: the canonical K8s computation is
// max(max(init), Σregular + Σsidecar). A plain init container (1000m) that exceeds
// the regular sum (500m) sets the pod request to 1000m; a native sidecar (init
// container with restartPolicy=Always, 200m) is added to the regular sum instead.
func TestSumComputeRequests_InitAndSidecar(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways

	// Case A: a big plain init container dominates the regular sum.
	initHeavy := podWith("500m", "512Mi")
	initHeavy.Spec.InitContainers = []corev1.Container{{
		Name: "migrate",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1000m"),
		}},
	}}
	cpuA, _ := sumComputeRequests([]corev1.Pod{initHeavy})
	if cpuA != 1000 {
		t.Errorf("init-heavy cpu=%d, want 1000 (max(init 1000, regular 500))", cpuA)
	}

	// Case B: a native sidecar (restartPolicy=Always init) adds to the regular sum.
	sidecar := podWith("500m", "512Mi")
	sidecar.Spec.InitContainers = []corev1.Container{{
		Name:          "proxy",
		RestartPolicy: &always,
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("200m"),
		}},
	}}
	cpuB, _ := sumComputeRequests([]corev1.Pod{sidecar})
	if cpuB != 700 {
		t.Errorf("sidecar cpu=%d, want 700 (regular 500 + sidecar 200)", cpuB)
	}
}
