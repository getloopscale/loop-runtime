// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"strings"
	"testing"
)

// Three defaults the loop-runtime-api chart ships that an infrastructure design audit
// (2026-09-07, system-design-output/final_report.md) found to be wrong in the
// same direction: the mechanism is present in the chart and switched off, so a
// reader sees the feature and the cluster does not get it.
//
// Each assertion is about the RENDERED output, not about the values file,
// because a values key that no template consumes is exactly the failure mode
// the audit kept finding.

func renderLoopRuntimeAPI(t *testing.T) string {
	t.Helper()
	return renderChart(t, publishedChart(t, "loop-runtime-api"), "api", nil)
}

// SD-12.2. loop-runtime-api serves long-lived SSE streams (deploy.*, rollout.step)
// from the same Deployment as the request/response API. With no
// terminationGracePeriodSeconds the Kubernetes default of 30s applies, so a
// rolling update severs a stream the CLI may legitimately watch for minutes —
// the renderer gives tenant canaries a 300s progressDeadlineSeconds.
//
// The grace period must therefore cover the canary budget, not the default.
func TestLoopRuntimeAPI_DeclaresGracePeriodCoveringACanary(t *testing.T) {
	rendered := renderLoopRuntimeAPI(t)

	if !strings.Contains(rendered, "terminationGracePeriodSeconds:") {
		t.Fatal("loop-runtime-api Deployment renders no terminationGracePeriodSeconds; " +
			"SSE streams are cut at the 30s default while a watched canary runs to 300s")
	}

	// 300s is the renderer's progressDeadlineSeconds for tenant Rollouts. A
	// grace period below it cannot outlive the thing the stream is reporting on.
	var found string
	for _, line := range strings.Split(rendered, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "terminationGracePeriodSeconds:") {
			found = strings.TrimSpace(strings.TrimPrefix(l, "terminationGracePeriodSeconds:"))
			break
		}
	}
	if found != "300" {
		t.Errorf("terminationGracePeriodSeconds = %q, want 300 (the canary progress deadline)", found)
	}
}

// SD-05.4. The chart ships topologySpreadConstraints.enabled=false with
// affinity={} while running replicaCount 2. Nothing keeps the two replicas off
// one node, so the pair survives a pod failure and not a node failure — the
// mode a 99.9%/24h availability target is most exposed to.
//
// The helper and the template wiring already exist; only the default was off.
func TestLoopRuntimeAPI_SpreadsReplicasAcrossNodes(t *testing.T) {
	rendered := renderLoopRuntimeAPI(t)

	if !strings.Contains(rendered, "topologySpreadConstraints:") {
		t.Fatal("loop-runtime-api Deployment renders no topologySpreadConstraints; " +
			"2 replicas may be scheduled onto one node")
	}
	if !strings.Contains(rendered, "kubernetes.io/hostname") {
		t.Error("topologySpreadConstraints does not spread on kubernetes.io/hostname; " +
			"spreading on anything else does not survive a node loss")
	}
}

// SD-01.13. Both loop-runtime-api charts ship serviceMonitor.enabled=false and no
// helmfile release overrides it, so the chart-native scrape path is off and the
// control-plane API is scraped only by a standalone manifest applied through a
// Taskfile — outside any reconciler.
//
// Turning the chart-native path on is what lets the same mechanism that owns
// the workload own its monitoring.
func TestLoopRuntimeAPI_ShipsItsOwnServiceMonitor(t *testing.T) {
	rendered := renderLoopRuntimeAPI(t)

	if !strings.Contains(rendered, "kind: ServiceMonitor") {
		t.Fatal("loop-runtime-api chart renders no ServiceMonitor; metrics collection for the " +
			"control-plane API depends on a kubectl apply nothing reconciles")
	}
}
