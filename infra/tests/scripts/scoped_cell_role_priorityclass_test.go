// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

type scopedRule struct {
	APIGroups []string `json:"apiGroups"`
	Resources []string `json:"resources"`
	Verbs     []string `json:"verbs"`
}

// scopedCellRoleRules parses the ClusterRole scoped-kubeconfig.sh applies on a cell for
// the loop-runtime-ctrl-runtime identity the operators use through ClusterCredential.
func scopedCellRoleRules(t *testing.T) []scopedRule {
	t.Helper()
	raw, err := os.ReadFile("../../helmfile/scripts/lib/scoped-kubeconfig.sh")
	if err != nil {
		t.Fatalf("read scoped-kubeconfig.sh: %v", err)
	}
	m := regexp.MustCompile(`(?s)cat <<EOF \| kubectl apply -f - >/dev/null\n(apiVersion: rbac\.authorization\.k8s\.io/v1\nkind: ClusterRole\n.*?)\nEOF`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("scoped-kubeconfig.sh applies no ClusterRole heredoc")
	}
	var role struct {
		Rules []scopedRule `json:"rules"`
	}
	if err := yaml.Unmarshal([]byte(strings.ReplaceAll(string(m[1]), "${CLUSTER_ROLE}", "x")), &role); err != nil {
		t.Fatalf("parse the ClusterRole: %v", err)
	}
	return role.Rules
}

func grants(rules []scopedRule, group, resource, verb string) bool {
	for _, r := range rules {
		if (slices.Contains(r.APIGroups, group) || slices.Contains(r.APIGroups, "*")) &&
			(slices.Contains(r.Resources, resource) || slices.Contains(r.Resources, "*")) &&
			(slices.Contains(r.Verbs, verb) || slices.Contains(r.Verbs, "*")) {
			return true
		}
	}
	return false
}

// The tenant reconcile creates the plan's PriorityClass on the tenant's cell through
// the scoped identity (operators/internal/tenant/tenant_priority.go). The identity could
// only read PriorityClasses, so on a freshly created cell every tenant stopped at
// `priorityclasses.scheduling.k8s.io is forbidden` — measured on a cold cleanroom (#408).
func TestTheScopedCellIdentityMayWritePriorityClasses(t *testing.T) {
	rules := scopedCellRoleRules(t)

	for _, verb := range []string{"create", "update", "patch"} {
		if !grants(rules, "scheduling.k8s.io", "priorityclasses", verb) {
			t.Errorf("the scoped cell ClusterRole does not grant %s on scheduling.k8s.io/priorityclasses", verb)
		}
	}
}
