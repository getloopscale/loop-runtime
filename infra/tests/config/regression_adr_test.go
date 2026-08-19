// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package config — regression suite for B1-B18 (CHG-2026-04-28-LAYERED-BOOTSTRAP T8.1).
//
// Each test pre-condition mirrors a documented L3 bug. The suite exists so
// CI fails early if any of those bugs reappear via configuration drift.
//
// Run:
//
//	LOOPZERO_REGRESSION=1 go test ./infra/tests/performance/load/ -run TestRegression -v
package config

import (
	"os"
	"strings"
	"testing"
)

// runRegressionScript invokes regression-checks.sh and returns combined output.
func TestADR0018C1_NoEnvVarOverride(t *testing.T) {
	data, err := os.ReadFile("../../../api/internal/k8s/tenant_cr_manager.go")
	if err != nil {
		t.Fatalf("read tenant_cr_manager.go: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `const tenantNamespace = "loopzero-system"`) {
		t.Error("ADR-0018 C1 regression: tenant_cr_manager.go must declare `const tenantNamespace = \"loopzero-system\"`")
	}
	if strings.Contains(s, `os.Getenv("TENANT_NAMESPACE")`) {
		t.Error("ADR-0018 C1 regression: tenant_cr_manager.go must NOT read TENANT_NAMESPACE env")
	}
}

func TestADR0018C2C5_StartupMisplacementScan(t *testing.T) {
	t.Skip("monolith operators/cmd/main.go deleted T1.4 — ADR-0018 C2/C5 startup scan now per-binary (each split binary owns its CR types' scan)")
	mainData, err := os.ReadFile("../../../operators/cmd/main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(mainData), "runMisplacedTenantScan") {
		t.Error("ADR-0018 C2 regression: main.go must invoke runMisplacedTenantScan at startup")
	}
	if !strings.Contains(string(mainData), "func runMisplacedTenantScan") {
		t.Error("ADR-0018 C2 regression: runMisplacedTenantScan function must be defined")
	}
	metricsData, err := os.ReadFile("../../../operators/internal/metrics/metrics.go")
	if err != nil {
		t.Fatalf("read metrics.go: %v", err)
	}
	if !strings.Contains(string(metricsData), "MisplacedTenantTotal") {
		t.Error("ADR-0018 C5 regression: metrics package must export MisplacedTenantTotal")
	}
	if !strings.Contains(string(metricsData), "loopzero_misplaced_tenant_total") {
		t.Error("ADR-0018 C5 regression: metric name must be loopzero_misplaced_tenant_total")
	}
}

func TestADR0018T32_VAPTemplateExists(t *testing.T) {
	const path = "../../helm/admission-policies/templates/vap-tenant-namespace.yaml"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ADR-0018 T3.2 regression: VAP template missing: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "{{- if .Values.vaps.tenantNamespace }}") {
		t.Error("ADR-0018 T3.2 regression: VAP must be opt-in via .Values.vaps.tenantNamespace")
	}
	// C6 — status subresource exclusion via matchConditions.
	if !strings.Contains(s, "matchConditions:") {
		t.Error("ADR-0018 C6 regression: VAP must declare matchConditions to exclude subresources")
	}
	if !strings.Contains(s, `request.subResource == ""`) {
		t.Error("ADR-0018 C6 regression: matchConditions must skip when subResource != \"\" (status subresource bypass)")
	}
	// Validation expression check loopzero-system namespace.
	if !strings.Contains(s, `object.metadata.namespace == "loopzero-system"`) {
		t.Error("ADR-0018 T3.2 regression: validation expression must enforce namespace=loopzero-system")
	}
	// Default values.yaml: opt-out.
	values, err := os.ReadFile("../../helm/admission-policies/values.yaml")
	if err != nil {
		t.Fatalf("read values: %v", err)
	}
	if !strings.Contains(string(values), "tenantNamespace: false") {
		t.Error("ADR-0018 C4 regression: vaps.tenantNamespace default must be `false` (mandatory deployment order — VAP last)")
	}
}

// B-095: a cerca `loopzero-tenant-namespace-policy` casa CREATE **e UPDATE**, e remover finalizer e'
// um UPDATE. Sem isentar os proprios controllers, um CR cujo namespace nao satisfaca a validacao
// fica IMPOSSIVEL DE APAGAR — o operator nunca conclui a delecao e o requeue nunca converge. E' a
// mesma armadilha que o cabecalho da irma `vap-tenant-cr-shape` descreve.
//
// A lista precisa ser NAO-VAZIA: a forma `!(x in [])` e' sempre verdadeira, entao uma lista vazia
// faz a politica valer para toda identidade e negar tudo — armadilha de CEL registrada no
// `CLAUDE.md`. Por isso este teste exige os nomes, e nao so' a presenca do matchCondition.
func TestB095_VAPTenantNamespaceIsentaOsProprioControllers(t *testing.T) {
	const path = "../../helm/admission-policies/templates/vap-tenant-namespace.yaml"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("VAP template missing: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "exclude-system-actors") {
		t.Fatal("a politica nao isenta os atores de sistema: ligada, ela trava a remocao de " +
			"finalizer e torna o CR indeletavel")
	}
	for _, sa := range []string{
		"system:serviceaccount:loopzero-system:loopzero-api",
		"system:serviceaccount:loopzero-system:loopzero-tenant-operator",
		"system:serviceaccount:loopzero-system:tenant-operator",
		"system:kube-controller-manager",
		"system:masters",
	} {
		if !strings.Contains(s, sa) {
			t.Errorf("a lista de isencao nao nomeia %q; uma lista incompleta nega escritas "+
				"legitimas, e uma lista VAZIA (`!(x in [])`) nega todas", sa)
		}
	}
}
