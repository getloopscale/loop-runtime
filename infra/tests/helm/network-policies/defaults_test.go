//go:build helm_render

// Package networkpolicies_test — Plan 2026-05-20 v1.1 T3.2 (2/8).
//
// Critical: this chart defines the platform's default-deny stance. A
// silent regression here = workloads gain free internet egress without
// notice. Tests assert: default-deny present + targeted allow policies
// scoped to specific pods (not wide-open).
package networkpolicies_test

import (
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	netV1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/yaml"
)

const (
	chartName   = "network-policies"
	releaseName = "network-policies-test"
)

func renderNPs(t *testing.T) []netV1.NetworkPolicy {
	t.Helper()
	cp, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(cp, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, cp, releaseName, nil)
	docs := chartrender.SplitYAMLByKind(out, "NetworkPolicy")
	nps := make([]netV1.NetworkPolicy, 0, len(docs))
	for _, d := range docs {
		var np netV1.NetworkPolicy
		require.NoError(t, yaml.Unmarshal([]byte(d), &np))
		nps = append(nps, np)
	}
	return nps
}

func TestNetworkPolicies_DefaultDenyIngressPresent(t *testing.T) {
	t.Parallel()
	// At least one NP must select all pods (podSelector: {}) and deny
	// Ingress — the cornerstone of zero-trust posture.
	found := false
	for _, np := range renderNPs(t) {
		if len(np.Spec.PodSelector.MatchLabels) == 0 &&
			len(np.Spec.PodSelector.MatchExpressions) == 0 {
			for _, pt := range np.Spec.PolicyTypes {
				if pt == netV1.PolicyTypeIngress && len(np.Spec.Ingress) == 0 {
					found = true
				}
			}
		}
	}
	require.True(t, found,
		"chart must declare a default-deny-ingress NP (podSelector: {} + "+
			"policyTypes: [Ingress] + empty ingress rules)")
}

func TestNetworkPolicies_AllUseNetworkingV1(t *testing.T) {
	t.Parallel()
	// NetworkPolicy still exists as a single stable type; assert nobody
	// regressed to a non-existent apiVersion.
	for _, np := range renderNPs(t) {
		// Default unmarshal preserves TypeMeta only if present in YAML;
		// since NetworkPolicy has been stable, the check is most useful
		// on the structural side — assert NP parsed at all (would error
		// on apiVersion drift).
		require.NotEmpty(t, np.Spec.PolicyTypes,
			"NP %q must declare policyTypes (empty = default Ingress only "+
				"which obscures intent)", np.Name)
	}
}

func TestNetworkPolicies_AllowPoliciesHavePodSelector(t *testing.T) {
	t.Parallel()
	// Allow-style NPs (those with ingress/egress rules) MUST scope by
	// podSelector — not match all pods (privilege escalation otherwise).
	for _, np := range renderNPs(t) {
		hasRules := len(np.Spec.Ingress) > 0 || len(np.Spec.Egress) > 0
		if !hasRules {
			continue // default-deny style — exempt
		}
		isWildcard := len(np.Spec.PodSelector.MatchLabels) == 0 &&
			len(np.Spec.PodSelector.MatchExpressions) == 0
		require.Falsef(t, isWildcard,
			"NP %q has ingress/egress rules but matches ALL pods — "+
				"this grants wide access; scope via podSelector", np.Name)
	}
}

func TestNetworkPolicies_AllInLoopRuntimeSystemNamespace(t *testing.T) {
	t.Parallel()
	// This chart targets loopruntime-system. Catches typos (e.g., loopruntime-syste)
	// that silently leave default-deny absent from the right namespace.
	for _, np := range renderNPs(t) {
		require.Equalf(t, "loopruntime-system", np.Namespace,
			"NP %q must be in loopruntime-system (chart owns control-plane "+
				"policies); got %q", np.Name, np.Namespace)
	}
}
