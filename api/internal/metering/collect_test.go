// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metering

// collect_test.go — pure Σ-request logic for the metering worker (plan
// metering-compute-loop T3.1). No cluster, no DB — corev1.Pod fixtures only.

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// fakePodLister returns canned pods per namespace, or a canned error, for the
// collectNamespaces tests (no cluster).
type fakePodLister struct {
	byNS  map[string][]corev1.Pod
	errNS map[string]error
}

func (f fakePodLister) ListPods(ctx context.Context, namespace string) ([]corev1.Pod, error) {
	if e, ok := f.errNS[namespace]; ok {
		return nil, e
	}
	return f.byNS[namespace], nil
}

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

// TestCollectNamespaces_SumsAcrossNamespaces — collect lists pods across all a
// tenant's (project,env) namespaces on a cell and sums the effective request.
func TestCollectNamespaces_SumsAcrossNamespaces(t *testing.T) {
	lister := fakePodLister{byNS: map[string][]corev1.Pod{
		"loopruntime-acme-web-prod":    {podWith("500m", "512Mi")},
		"loopruntime-acme-worker-prod": {podWith("250m", "256Mi"), podWith("250m", "256Mi")},
	}}
	cpu, mem, err := collectNamespaces(context.Background(), lister, []string{"loopruntime-acme-web-prod", "loopruntime-acme-worker-prod"})
	if err != nil {
		t.Fatalf("collectNamespaces: %v", err)
	}
	if cpu != 1000 { // 500 + 250 + 250
		t.Errorf("cpuMillicores=%d, want 1000", cpu)
	}
	if mem != 1<<30 { // 512Mi + 256Mi + 256Mi = 1Gi
		t.Errorf("memBytes=%d, want %d (1Gi)", mem, int64(1<<30))
	}
}

// TestCollectNamespaces_ListErrorShortCircuits — SC-3: a pod-list failure on ANY
// namespace short-circuits with a non-nil error and ZERO totals; the caller MUST
// NOT meter (never swallow a read failure to Σreq=0, which would advance the
// cursor over an unmeasured interval → silent under-bill).
func TestCollectNamespaces_ListErrorShortCircuits(t *testing.T) {
	boom := errors.New("cell unreachable")
	lister := fakePodLister{
		byNS:  map[string][]corev1.Pod{"loopruntime-acme-web-prod": {podWith("500m", "512Mi")}},
		errNS: map[string]error{"loopruntime-acme-worker-prod": boom},
	}
	cpu, mem, err := collectNamespaces(context.Background(), lister,
		[]string{"loopruntime-acme-web-prod", "loopruntime-acme-worker-prod"})
	if err == nil {
		t.Fatal("collectNamespaces returned nil error on a list failure — SC-3 violated (must short-circuit, never swallow to 0)")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error does not wrap the underlying list failure: %v", err)
	}
	if cpu != 0 || mem != 0 {
		t.Errorf("totals=(%d,%d) on a read failure, want (0,0) — no partial sum may leak to a meter write", cpu, mem)
	}
}
