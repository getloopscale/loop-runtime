// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"slices"
	"sort"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// operatorSecretAccess is what one operator chart grants on Secrets: the
// namespaces it may touch them in, and the verbs in each. Every entry is the
// measured access of the operator's code on the management cluster — the
// inventory lives in the commit that set it. Build and cell clusters are
// reached through kubeconfigs carrying their own identity, so nothing here
// covers them.
type operatorSecretAccess struct {
	chart      string
	namespaces map[string][]string
}

var operatorSecretAccessTable = []operatorSecretAccess{
	{
		chart: "loopzero-tenant-operator",
		// cell kubeconfig Secrets (CellAwareRuntimeClientFactory) and the
		// registry pull Secret (EnvironmentReconciler.ensurePullSecret), both
		// read through the manager cache.
		namespaces: map[string][]string{"loopzero-system": {"get", "list", "watch"}},
	},
	{
		chart: "loopzero-app-operator",
		// cell kubeconfig Secrets (CellAwareRuntimeClientFactory), read through
		// the manager cache. The TLS Secret the ExposureController checks is
		// read in the workload's cell, with the cell's own identity.
		namespaces: map[string][]string{"loopzero-system": {"get", "list", "watch"}},
	},
}

// TestOperatorCharts_GrantNoClusterWideSecretOrConfigMapAccess — a ClusterRole
// rule on Secrets or ConfigMaps lets the operator read every credential in the
// cluster, tenant workloads included (principle_violations#149/#150). Secret
// access is namespaced now; the ClusterRole carries none by default.
func TestOperatorCharts_GrantNoClusterWideSecretOrConfigMapAccess(t *testing.T) {
	for _, tc := range operatorSecretAccessTable {
		t.Run(tc.chart, func(t *testing.T) {
			docs := renderRBAC(t, tc.chart, nil)
			for _, cr := range docs.clusterRoles {
				for _, rule := range cr.Rules {
					if grantsCoreResource(rule, "secrets") || grantsCoreResource(rule, "configmaps") {
						t.Errorf("ClusterRole %s grants %v on %v cluster-wide", cr.Name, rule.Verbs, rule.Resources)
					}
				}
			}
		})
	}
}

// TestOperatorCharts_GrantSecretAccessOnlyWhereTheOperatorUsesIt pins the
// namespaced grants: a Role per namespace the operator reads or writes Secrets
// in, with exactly the verbs its code needs, bound to the operator's
// ServiceAccount — and no Secret grant in any other namespace.
func TestOperatorCharts_GrantSecretAccessOnlyWhereTheOperatorUsesIt(t *testing.T) {
	for _, tc := range operatorSecretAccessTable {
		t.Run(tc.chart, func(t *testing.T) {
			docs := renderRBAC(t, tc.chart, nil)
			granted := docs.secretVerbsByNamespace(t)

			var gotNamespaces, wantNamespaces []string
			for ns := range granted {
				gotNamespaces = append(gotNamespaces, ns)
			}
			for ns := range tc.namespaces {
				wantNamespaces = append(wantNamespaces, ns)
			}
			sort.Strings(gotNamespaces)
			sort.Strings(wantNamespaces)
			if !slices.Equal(gotNamespaces, wantNamespaces) {
				t.Fatalf("Secret access rendered in namespaces %v, want %v", gotNamespaces, wantNamespaces)
			}
			for ns, want := range tc.namespaces {
				want = slices.Clone(want)
				sort.Strings(want)
				if got := granted[ns]; !slices.Equal(got, want) {
					t.Errorf("Secret verbs in %s = %v, want %v", ns, got, want)
				}
			}
		})
	}
}

// TestOperatorCharts_LegacyClusterWideSecretsIsAnOptInReadGrant — the cutover
// escape hatch restores cluster-wide Secret READ for an operator image that
// predates the scoped Secret cache, and nothing more. It is off unless set.
func TestOperatorCharts_LegacyClusterWideSecretsIsAnOptInReadGrant(t *testing.T) {
	for _, tc := range operatorSecretAccessTable {
		t.Run(tc.chart, func(t *testing.T) {
			docs := renderRBAC(t, tc.chart, map[string]string{"rbac.legacyClusterWideSecrets": "true"})
			var verbs []string
			for _, cr := range docs.clusterRoles {
				for _, rule := range cr.Rules {
					if grantsCoreResource(rule, "configmaps") {
						t.Errorf("ClusterRole %s grants configmaps under the legacy flag; no operator image reads ConfigMaps", cr.Name)
					}
					if grantsCoreResource(rule, "secrets") {
						verbs = append(verbs, rule.Verbs...)
					}
				}
			}
			sort.Strings(verbs)
			if want := []string{"get", "list", "watch"}; !slices.Equal(verbs, want) {
				t.Errorf("legacy cluster-wide Secret verbs = %v, want %v", verbs, want)
			}
		})
	}
}

type renderedRBAC struct {
	serviceAccount string
	clusterRoles   []rbacv1.ClusterRole
	roles          []rbacv1.Role
	roleBindings   []rbacv1.RoleBinding
}

// renderRBAC renders chart and decodes its ServiceAccount and RBAC documents.
func renderRBAC(t *testing.T, chart string, set map[string]string) renderedRBAC {
	t.Helper()
	out := renderChart(t, uniqueChart(t, chart), "rbac-check", set)
	var r renderedRBAC
	for _, doc := range strings.Split(out, "\n---") {
		var meta struct {
			Kind string `json:"kind"`
		}
		if err := yaml.Unmarshal([]byte(doc), &meta); err != nil {
			t.Fatalf("decode rendered document: %v\n%s", err, doc)
		}
		switch meta.Kind {
		case "ServiceAccount":
			var sa corev1.ServiceAccount
			mustDecode(t, doc, &sa)
			r.serviceAccount = sa.Name
		case "ClusterRole":
			var cr rbacv1.ClusterRole
			mustDecode(t, doc, &cr)
			r.clusterRoles = append(r.clusterRoles, cr)
		case "Role":
			var role rbacv1.Role
			mustDecode(t, doc, &role)
			r.roles = append(r.roles, role)
		case "RoleBinding":
			var rb rbacv1.RoleBinding
			mustDecode(t, doc, &rb)
			r.roleBindings = append(r.roleBindings, rb)
		}
	}
	if r.serviceAccount == "" {
		t.Fatalf("%s renders no ServiceAccount", chart)
	}
	return r
}

func mustDecode(t *testing.T, doc string, into any) {
	t.Helper()
	if err := yaml.Unmarshal([]byte(doc), into); err != nil {
		t.Fatalf("decode %T: %v\n%s", into, err, doc)
	}
}

// secretVerbsByNamespace returns, per namespace, the sorted verbs the
// operator's ServiceAccount holds on every Secret there (rules restricted by
// resourceNames are excluded: they grant one named object, not the namespace).
// A Role that grants Secrets but is bound to no one, or to someone else, is a
// test failure — it is either dead or someone else's grant in this chart.
func (r renderedRBAC) secretVerbsByNamespace(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, role := range r.roles {
		var verbs []string
		for _, rule := range role.Rules {
			if grantsCoreResource(rule, "secrets") && len(rule.ResourceNames) == 0 {
				verbs = append(verbs, rule.Verbs...)
			}
		}
		if len(verbs) == 0 {
			continue
		}
		if !r.boundToOperator(role) {
			t.Errorf("Role %s/%s grants Secrets but is not bound to ServiceAccount %s", role.Namespace, role.Name, r.serviceAccount)
			continue
		}
		out[role.Namespace] = append(out[role.Namespace], verbs...)
	}
	for ns, verbs := range out {
		sort.Strings(verbs)
		out[ns] = slices.Compact(verbs)
	}
	return out
}

func (r renderedRBAC) boundToOperator(role rbacv1.Role) bool {
	for _, rb := range r.roleBindings {
		if rb.Namespace != role.Namespace || rb.RoleRef.Kind != "Role" || rb.RoleRef.Name != role.Name {
			continue
		}
		for _, s := range rb.Subjects {
			if s.Kind == rbacv1.ServiceAccountKind && s.Name == r.serviceAccount {
				return true
			}
		}
	}
	return false
}

func grantsCoreResource(rule rbacv1.PolicyRule, resource string) bool {
	if !slices.Contains(rule.APIGroups, "") && !slices.Contains(rule.APIGroups, "*") {
		return false
	}
	return slices.Contains(rule.Resources, resource) || slices.Contains(rule.Resources, "*")
}
