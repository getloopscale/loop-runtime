// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestAppOperatorCanReadTenant — the ApplicationController reads the Tenant CR to
// resolve the cellID before querying the workload (reconcileWorkloadStatus). The
// chart's ClusterRole declared only loopruntimeapplications and exposedroutes, so the Get
// failed and the function returned — without counting replicas, leaving every
// LoopRuntimeApplication in Progressing.
//
// Measured in app-dev 2026-08-09, in the operator's own log:
//
//	Failed to watch *v1.Tenant: tenants.runtime.getloopscale.com is forbidden:
//	User "system:serviceaccount:loop-runtime-system:loop-runtime-app-operator" cannot list resource "tenants"
//
// Third layer of issue #274, after the namespace without a project and the nil
// RuntimeFactory.
func TestAppOperatorCanReadTenant(t *testing.T) {
	out := renderChart(t, uniqueChart(t, "loop-runtime-app-operator"), "rbac-check", nil)

	// Isolate the operator's ClusterRole: the tenants rule must be in it, not in
	// any other document of the chart.
	var clusterRole string
	for _, doc := range strings.Split(out, "\n---") {
		if strings.Contains(doc, "kind: ClusterRole") && !strings.Contains(doc, "kind: ClusterRoleBinding") {
			clusterRole = doc
			break
		}
	}
	if clusterRole == "" {
		t.Fatal("the chart renders no ClusterRole")
	}

	if !strings.Contains(clusterRole, "tenants") {
		t.Errorf("the ClusterRole does not grant read on `tenants`, but reconcileWorkloadStatus "+
			"does a Get on a Tenant before anything else — without it the workload status is "+
			"never read (#274). Rendered ClusterRole:\n%s", clusterRole)
	}
}

// TestAppOperatorCanReadClusterCredentials — both controllers resolve a
// placed tenant's cell through CellAwareRuntimeClientFactory, which lists the
// ClusterCredentials to find the cell's kubeconfig Secret. The chart granted
// read on tenants but not on clustercredentials, so every lookup for a named
// cell was forbidden: the fast-path Healthy and the TLS certificate check never
// reached the workload. The Tilt manifest has carried the grant all along.
func TestAppOperatorCanReadClusterCredentials(t *testing.T) {
	docs := renderRBAC(t, "loop-runtime-app-operator", nil)
	var verbs []string
	for _, cr := range docs.clusterRoles {
		for _, rule := range cr.Rules {
			if slices.Contains(rule.APIGroups, "runtime.getloopscale.com") && slices.Contains(rule.Resources, "clustercredentials") {
				verbs = append(verbs, rule.Verbs...)
			}
		}
	}
	sort.Strings(verbs)
	if want := []string{"get", "list", "watch"}; !slices.Equal(verbs, want) {
		t.Errorf("app-operator ClusterRole verbs on clustercredentials = %v, want %v", verbs, want)
	}
}
