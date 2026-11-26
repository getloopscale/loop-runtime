// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T8.5b regression test — text-level invariants for the loopruntime-system egress
// allow-list NetworkPolicy and the default-deny-egress fallback.
//
// Why text-level: helm template + kubeconform are the LIVE smoke tests
// run by infra-deploy.yml. This unit test guards the source against
// accidental edits that would weaken the gates (e.g., flipping
// loopruntimeSystemAllowEgress to default-true without the staging soak).

package networkpolicies_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readEgressTemplate(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	templatePath := filepath.Join(
		filepath.Dir(thisFile),
		"..", "..", "..", "..",
		"infra", "helm", "network-policies", "templates", "loopruntime-system-egress.yaml",
	)
	data, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("template not found: %v", err)
	}
	return string(data)
}

func readNetworkPoliciesValues(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	valuesPath := filepath.Join(
		filepath.Dir(thisFile),
		"..", "..", "..", "..",
		"infra", "helm", "network-policies", "values.yaml",
	)
	data, err := os.ReadFile(valuesPath)
	if err != nil {
		t.Fatalf("values not found: %v", err)
	}
	return string(data)
}

func TestNetworkPolicy_EgressTemplateExists(t *testing.T) {
	tmpl := readEgressTemplate(t)
	if !strings.Contains(tmpl, "loopruntime-system-allow-egress") {
		t.Error("template must define loopruntime-system-allow-egress NetworkPolicy")
	}
	if !strings.Contains(tmpl, "loopruntime-system-default-deny-egress") {
		t.Error("template must define loopruntime-system-default-deny-egress NetworkPolicy")
	}
}

func TestNetworkPolicy_DefaultDenyOptInOnly(t *testing.T) {
	values := readNetworkPoliciesValues(t)

	// Per EC-5, defaultDenyEgress MUST default OFF until staging soak
	// validates the allow-list. Flipping default-on without the inventory
	// breaks DNS, kube-apiserver, registry pulls — every pod in loopruntime-system.
	if !strings.Contains(values, "defaultDenyEgress: false") {
		t.Error("values.yaml MUST default defaultDenyEgress to false (T8.5b/c gate)")
	}
	if !strings.Contains(values, "loopruntimeSystemAllowEgress: false") {
		t.Error("values.yaml MUST default loopruntimeSystemAllowEgress to false until cell-by-cell rollout")
	}
}

func TestNetworkPolicy_AllowsDNSAndKubeAPI(t *testing.T) {
	tmpl := readEgressTemplate(t)
	// DNS to kube-system is the most common breakage class. Guard it.
	if !strings.Contains(tmpl, "kubernetes.io/metadata.name: kube-system") {
		t.Error("template must allow egress to kube-system (DNS)")
	}
	if !strings.Contains(tmpl, "port: 53") {
		t.Error("template must allow port 53 for DNS")
	}
	// kube-apiserver lives in default namespace (kubernetes.default.svc).
	if !strings.Contains(tmpl, "kubernetes.io/metadata.name: default") {
		t.Error("template must allow egress to default namespace (kube-apiserver)")
	}
}
