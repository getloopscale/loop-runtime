// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"os/exec"
	"strings"
	"testing"
)

// TestAppOperatorPodeLerTenant — o ApplicationController lê o Tenant CR para resolver o
// cellID antes de consultar o workload (reconcileWorkloadStatus). O ClusterRole do chart
// declarava apenas loopzeroapplications e exposedroutes, então o Get falhava e a função
// retornava — sem contar réplicas, deixando toda LoopZeroApplication em Progressing.
//
// Medido em app-dev 2026-08-09, no log do próprio operator:
//
//	Failed to watch *v1.Tenant: tenants.loopzero.io is forbidden:
//	User "system:serviceaccount:loopzero-system:loopzero-app-operator" cannot list resource "tenants"
//
// Terceira camada da issue #274, depois do namespace sem projeto e do RuntimeFactory nil.
func TestAppOperatorPodeLerTenant(t *testing.T) {
	dir := operatorChartDir(t, "loopzero-app-operator")
	out, err := exec.Command("helm", "template", "rbac-check", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}

	// Isola o ClusterRole do operator: a regra de tenants tem de estar nele, não em
	// qualquer outro documento do chart.
	var clusterRole string
	for _, doc := range strings.Split(string(out), "\n---") {
		if strings.Contains(doc, "kind: ClusterRole") && !strings.Contains(doc, "kind: ClusterRoleBinding") {
			clusterRole = doc
			break
		}
	}
	if clusterRole == "" {
		t.Fatal("o chart não renderiza ClusterRole nenhum")
	}

	if !strings.Contains(clusterRole, "tenants") {
		t.Errorf("o ClusterRole não concede leitura de `tenants`, mas reconcileWorkloadStatus "+
			"faz Get num Tenant antes de qualquer outra coisa — sem isso o status do workload "+
			"nunca é lido (#274). ClusterRole renderizado:\n%s", clusterRole)
	}
}
