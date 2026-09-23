// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"strings"
	"testing"
)

// TestAppOperatorCanReadTenant — the ApplicationController reads the Tenant CR to
// resolve the cellID before querying the workload (reconcileWorkloadStatus). The
// chart's ClusterRole declared only loopzeroapplications and exposedroutes, so the Get
// failed and the function returned — without counting replicas, leaving every
// LoopZeroApplication in Progressing.
//
// Measured in app-dev 2026-08-09, in the operator's own log:
//
//	Failed to watch *v1.Tenant: tenants.loopzero.io is forbidden:
//	User "system:serviceaccount:loopzero-system:loopzero-app-operator" cannot list resource "tenants"
//
// Third layer of issue #274, after the namespace without a project and the nil
// RuntimeFactory.
func TestAppOperatorCanReadTenant(t *testing.T) {
	out := renderChart(t, uniqueChart(t, "loopzero-app-operator"), "rbac-check", nil)

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
