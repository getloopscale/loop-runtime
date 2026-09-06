//go:build helm_render

// Package admissionpolicies_test — Plan 2026-05-20 v1.1 T3.2 (5/8).
//
// This is the BOOTSTRAP variant of loopzero-admission-policies (in infra/helm/,
// consumed by helmfile L2). Charts/loopzero-admission-policies (published)
// has its own test pkg (T3.1 chart 9/10).
//
// Both variants are deployed in different lifecycles (bootstrap-time vs
// Day-2 GitOps); render-time tests cover both to catch drift.
package admissionpolicies_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/getloopscale/loopzero/infra/tests/helm/chartrender"
	"sigs.k8s.io/yaml"
)

const (
	chartName        = "admission-policies"
	releaseName      = "admission-policies-test"
	expectedVAPs     = 6
	expectedBindings = 6
	expectedCIPs     = 1
	expectedPRs      = 2
)

type vapDoc struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		FailurePolicy    string `json:"failurePolicy"`
		MatchConstraints struct {
			ResourceRules []struct {
				Resources []string `json:"resources"`
			} `json:"resourceRules"`
		} `json:"matchConstraints"`
		MatchConditions []struct {
			Expression string `json:"expression"`
		} `json:"matchConditions,omitempty"`
	} `json:"spec"`
}

func renderAll(t *testing.T) string {
	t.Helper()
	cp, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(cp, "")
	require.NoError(t, err)
	return chartrender.RenderTemplate(t, opts, cp, releaseName, nil)
}

func renderVAPs(t *testing.T) []vapDoc {
	t.Helper()
	out := chartrender.SplitYAMLByKind(renderAll(t), "ValidatingAdmissionPolicy")
	vaps := make([]vapDoc, 0, len(out))
	for _, d := range out {
		var v vapDoc
		require.NoError(t, yaml.Unmarshal([]byte(d), &v))
		vaps = append(vaps, v)
	}
	return vaps
}

func TestBootstrapAdmission_VAPCount(t *testing.T) {
	t.Parallel()
	require.Equalf(t, expectedVAPs, len(renderVAPs(t)),
		"bootstrap VAP count must match published variant (%d) — drift "+
			"between infra/helm and charts/ creates lifecycle inconsistency",
		expectedVAPs)
}

func TestBootstrapAdmission_VAPAndBindingCountsMatch(t *testing.T) {
	t.Parallel()
	out := renderAll(t)
	vaps := chartrender.SplitYAMLByKind(out, "ValidatingAdmissionPolicy")
	bindings := chartrender.SplitYAMLByKind(out, "ValidatingAdmissionPolicyBinding")
	require.Equal(t, len(vaps), len(bindings),
		"every VAP MUST have a binding (1:1)")
}

func TestBootstrapAdmission_AllVAPsHaveExplicitFailurePolicy(t *testing.T) {
	t.Parallel()
	for _, v := range renderVAPs(t) {
		require.NotEmptyf(t, v.Spec.FailurePolicy,
			"VAP %q missing failurePolicy (Bug #33 class)", v.Metadata.Name)
	}
}

func TestBootstrapAdmission_PodTargetingVAPsExemptSystemActors(t *testing.T) {
	t.Parallel()
	// Bug #33 protection identical to charts/ variant.
	for _, v := range renderVAPs(t) {
		targetsPods := false
		for _, rr := range v.Spec.MatchConstraints.ResourceRules {
			for _, r := range rr.Resources {
				if r == "pods" {
					targetsPods = true
				}
			}
		}
		if !targetsPods || v.Spec.FailurePolicy == "Ignore" {
			continue
		}
		require.NotEmptyf(t, v.Spec.MatchConditions,
			"VAP %q targets pods with failurePolicy=Fail but has no "+
				"matchConditions — NS deletion deadlock risk",
			v.Metadata.Name)
		hasSystemExempt := false
		for _, mc := range v.Spec.MatchConditions {
			if stringsContainsAny(mc.Expression, "system:", "userInfo", "kube-system") {
				hasSystemExempt = true
			}
		}
		require.Truef(t, hasSystemExempt,
			"VAP %q matchConditions do not exempt system actors",
			v.Metadata.Name)
	}
}

func TestBootstrapAdmission_ClusterImagePolicyCount(t *testing.T) {
	t.Parallel()
	docs := chartrender.SplitYAMLByKind(renderAll(t), "ClusterImagePolicy")
	require.Equalf(t, expectedCIPs, len(docs),
		"bootstrap ClusterImagePolicy count must match published variant (%d)",
		expectedCIPs)
}

func TestBootstrapAdmission_PrometheusRuleCount(t *testing.T) {
	t.Parallel()
	docs := chartrender.SplitYAMLByKind(renderAll(t), "PrometheusRule")
	require.Equalf(t, expectedPRs, len(docs),
		"bootstrap PrometheusRule count must match published variant (%d)",
		expectedPRs)
}

// stringsContainsAny is a local helper (avoids cross-package import).
func stringsContainsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if len(s) >= len(n) {
			for i := 0; i+len(n) <= len(s); i++ {
				if s[i:i+len(n)] == n {
					return true
				}
			}
		}
	}
	return false
}
