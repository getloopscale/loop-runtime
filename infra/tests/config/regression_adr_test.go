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
	data, err := os.ReadFile("../../../api/internal/k8s/tenancy/tenant_cr_manager.go")
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

// TestADR0018C5_MisplacedTenantMetricDeclared asserts the ADR-0018 C5 metric
// surface: the operators metrics package exports MisplacedTenantTotal under the
// name `loopzero_misplaced_tenant_total`, which is the series the
// MisplacedTenantDetected alert queries.
//
// B-184 -- WHAT THIS TEST USED TO BE, AND WHY ONLY HALF OF IT CAME BACK. It was
// TestADR0018C2C5_StartupMisplacementScan, and it opened with a BARE t.Skip
// carrying no guard at all:
//
//	t.Skip("monolith operators/cmd/main.go deleted T1.4 -- ... now per-binary")
//	mainData, err := os.ReadFile("../../../operators/cmd/main.go")
//	if err != nil { t.Fatalf("read main.go: %v", err) }
//
// A skip marks the WHOLE test skipped, so those eight statements had not run
// since T1.4 while the package went on reporting ok -- the same damage as the
// path-guarded skips B-180 removed, in a shape the test-skip-absent-path gate
// deliberately does not see (it looks for a filesystem probe in the guard, and
// there is no guard here).
//
// The C5 half asserted something that is still in this tree, and it is restored
// below. The C2 half asserted that main.go invokes and defines
// runMisplacedTenantScan. MEASURED 2026-09-05: operators/cmd/main.go is absent
// from this tree, so removing the skip alone would only have moved the failure
// to the os.ReadFile -- and the identifier runMisplacedTenantScan appears in no
// Go source here, in any of the four split binaries, so the skip's own claim
// that the scan is "now per-binary" was not true either.
//
// WHAT COVERS THE MISPLACEMENT VECTOR IN C2'S PLACE, and what does not:
//
//   - the C1 constant, no TENANT_NAMESPACE override -- TestADR0018C1_NoEnvVarOverride
//     above, live;
//   - the C9 cache scoping, which makes a Tenant outside loopzero-system invisible to
//     the operator -- infra/tests/unit/cache_scoping_test.go and
//     operators/cmd/cache_scoping_test.go, both live;
//   - the C2 startup SCAN itself is covered by NOTHING, because it is not
//     implemented: MisplacedTenantTotal is declared and registered but has no
//     writer anywhere in operators/, api/ or pkg/, so the two alerts that query
//     loopzero_misplaced_tenant_total can never fire. Tracked as B-189. This test
//     asserts the metric is DECLARED and says so in its name; it deliberately
//     does not assert something weaker under C2's name and call that coverage.
func TestADR0018C5_MisplacedTenantMetricDeclared(t *testing.T) {
	metricsData, err := os.ReadFile("../../../operators/internal/metrics/metrics.go")
	if err != nil {
		t.Fatalf("read metrics.go: %v", err)
	}
	s := string(metricsData)
	// Each want is DECLARATION-SHAPED on purpose. The assertions this test
	// inherited were bare substring checks for "MisplacedTenantTotal" and
	// "loopzero_misplaced_tenant_total", and a mutation run during B-184 measured the
	// first one surviving: renaming the exported variable left the identifier in
	// its own doc comment and in the MustRegister list, so the check passed over a
	// package that no longer declared it. A test that its own subject's prose can
	// satisfy is not asserting the subject.
	for _, w := range []struct{ want, why string }{
		{"MisplacedTenantTotal = prometheus.NewGaugeVec(", "metrics package must DECLARE exported MisplacedTenantTotal as a GaugeVec"},
		{`Name: "loopzero_misplaced_tenant_total",`, "the GaugeVec opts must carry metric name loopzero_misplaced_tenant_total"},
		{"\t\tMisplacedTenantTotal,\n", "MisplacedTenantTotal must be registered in the controller-runtime registry"},
	} {
		if !strings.Contains(s, w.want) {
			t.Errorf("ADR-0018 C5 regression: %s (want %q in metrics.go)", w.why, w.want)
		}
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
func TestVAPTenantNamespaceExemptsItsOwnControllers(t *testing.T) {
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
