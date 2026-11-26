// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
)

// Two ways loopzero-api reaches secrets, locked on its one chart (charts/loopzero-api), in
// both of its installs — ArgoCD with the chart defaults, and the Helmfile bootstrap
// with values-bootstrap.yaml.
//
//   - rbac.legacyClusterWideSecrets (T5.13 of plan 2026-06-02-openbao-adoption)
//     keeps the cluster-wide secrets/configmaps grant for clusters that predate the
//     per-tenant Role, and turning it off must remove THAT grant and nothing else.
//     A cluster-wide Secret grant was deep review finding #1.
//   - vault.enabled (T5.11) projects a ServiceAccount token with the OpenBao
//     audience; with vault off, which is the default of both installs, it must not.

// TestLoopZeroAPIKeepsTheLegacySecretGrantByDefault — the flag defaults to true so a
// cluster without the per-tenant Role keeps working after an upgrade.
func TestLoopZeroAPIKeepsTheLegacySecretGrantByDefault(t *testing.T) {
	chart := uniqueChart(t, "loopzero-api")
	var values struct {
		RBAC struct {
			LegacyClusterWideSecrets *bool `json:"legacyClusterWideSecrets"`
		} `json:"rbac"`
	}
	b, err := os.ReadFile(filepath.Join(chart, "values.yaml"))
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	mustDecode(t, string(b), &values)
	if v := values.RBAC.LegacyClusterWideSecrets; v == nil || !*v {
		t.Errorf("values.yaml rbac.legacyClusterWideSecrets = %v, want true (backward compatibility)", v)
	}

	rules := loopzeroAPIClusterRoleRules(t, nil)
	if !anyRuleGrants(rules, "secrets", "configmaps") {
		t.Errorf("the default render has no cluster-wide secrets/configmaps grant; rules: %+v", rules)
	}
}

// TestLoopZeroAPIDropsOnlyTheSecretGrantWithTheLegacyFlagOff — removing the grant must
// not take the control plane's other permissions with it.
func TestLoopZeroAPIDropsOnlyTheSecretGrantWithTheLegacyFlagOff(t *testing.T) {
	rules := loopzeroAPIClusterRoleRules(t, map[string]string{"rbac.legacyClusterWideSecrets": "false"})

	for _, r := range rules {
		if grantsCoreResource(r, "secrets") || grantsCoreResource(r, "configmaps") {
			t.Errorf("with the flag off a rule still grants secrets/configmaps cluster-wide: %+v", r)
		}
	}
	for _, kept := range []struct{ group, why string }{
		{"loopzero.io", "the control plane's CRDs"},
		{"argoproj.io", "the build pipeline's Workflows"},
	} {
		if !slices.ContainsFunc(rules, func(r rbacv1.PolicyRule) bool { return slices.Contains(r.APIGroups, kept.group) }) {
			t.Errorf("with the flag off the %s grant is gone — %s break; the removal is over-scoped", kept.group, kept.why)
		}
	}
	if !anyRuleGrants(rules, "namespaces") {
		t.Error("with the flag off the namespaces grant is gone — tenant provisioning breaks")
	}
}

func TestLoopZeroAPIProjectsTheOpenBaoTokenOnlyWhenVaultIsEnabled(t *testing.T) {
	chart := uniqueChart(t, "loopzero-api")
	bootstrap := filepath.Join(chart, "values-bootstrap.yaml")
	on := map[string]string{"vault.enabled": "true"}

	for _, tc := range []struct {
		name   string
		set    map[string]string
		values []string
		want   bool
	}{
		{"ArgoCD install, vault on", on, nil, true},
		{"ArgoCD install, vault off (default)", nil, nil, false},
		{"bootstrap install, vault on", on, []string{bootstrap}, true},
		{"bootstrap install, vault off (default)", nil, []string{bootstrap}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deploy := loopzeroAPIDeployment(t, chart, tc.set, tc.values)
			token, mounted := vaultTokenProjection(deploy)
			if !tc.want {
				if token != nil || mounted != "" {
					t.Errorf("vault-sa-token is rendered while vault is off")
				}
				return
			}
			if token == nil {
				t.Fatal("no vault-sa-token projected volume while vault.enabled=true")
			}
			if token.Audience != "vault.loopzero.io" {
				t.Errorf("token audience = %q, want vault.loopzero.io", token.Audience)
			}
			if token.ExpirationSeconds == nil || *token.ExpirationSeconds != 3600 {
				t.Errorf("token expirationSeconds = %v, want 3600", token.ExpirationSeconds)
			}
			if mounted != "/var/run/secrets/vault.loopzero.io" {
				t.Errorf("vault-sa-token mounted at %q, want /var/run/secrets/vault.loopzero.io", mounted)
			}
		})
	}
}

func loopzeroAPIClusterRoleRules(t *testing.T, set map[string]string) []rbacv1.PolicyRule {
	t.Helper()
	var rules []rbacv1.PolicyRule
	for _, cr := range renderRBAC(t, "loopzero-api", set).clusterRoles {
		rules = append(rules, cr.Rules...)
	}
	if len(rules) == 0 {
		t.Fatal("loopzero-api rendered no ClusterRole rules")
	}
	return rules
}

// anyRuleGrants reports whether one rule grants every one of the core resources.
func anyRuleGrants(rules []rbacv1.PolicyRule, resources ...string) bool {
	return slices.ContainsFunc(rules, func(r rbacv1.PolicyRule) bool {
		for _, res := range resources {
			if !grantsCoreResource(r, res) {
				return false
			}
		}
		return true
	})
}

func loopzeroAPIDeployment(t *testing.T, chart string, set map[string]string, values []string) appsv1.Deployment {
	t.Helper()
	var deploy appsv1.Deployment
	out := renderChart(t, chart, "loopzero-api", set, values...)
	docs := chartrender.SplitYAMLByKind(out, "Deployment")
	if len(docs) != 1 {
		t.Fatalf("loopzero-api rendered %d Deployments, want 1", len(docs))
	}
	if err := yaml.Unmarshal([]byte(docs[0]), &deploy); err != nil {
		t.Fatalf("decode the loopzero-api Deployment: %v", err)
	}
	return deploy
}

// vaultTokenProjection returns the ServiceAccount token source of the vault-sa-token
// volume and where a container mounts it; nil and "" when it is not rendered.
func vaultTokenProjection(deploy appsv1.Deployment) (*corev1.ServiceAccountTokenProjection, string) {
	var token *corev1.ServiceAccountTokenProjection
	for _, v := range deploy.Spec.Template.Spec.Volumes {
		if v.Name != "vault-sa-token" || v.Projected == nil {
			continue
		}
		for _, s := range v.Projected.Sources {
			if s.ServiceAccountToken != nil {
				token = s.ServiceAccountToken
			}
		}
	}
	for _, c := range deploy.Spec.Template.Spec.Containers {
		for _, m := range c.VolumeMounts {
			if m.Name == "vault-sa-token" {
				return token, m.MountPath
			}
		}
	}
	return token, ""
}
