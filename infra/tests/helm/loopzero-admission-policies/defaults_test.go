//go:build helm_render

// Package loopzeroadmissionpolicies_test — render-time tests for the
// loopzero-admission-policies chart (Plan 2026-05-20 v1.1 T3.1 chart 9/10).
//
// The chart emits:
//   - 6 ValidatingAdmissionPolicy (VAP)
//   - 6 ValidatingAdmissionPolicyBinding (one per VAP)
//   - 1 ClusterImagePolicy (cosign signature gate)
//   - 2 PrometheusRule (policy-controller-alerts + tenant-namespace-alerts)
//
// Critical structural invariants:
//   - Every VAP has exactly one matching VAPBinding
//   - VAP names referenced by bindings actually exist
//   - VAPs use admissionregistration.k8s.io/v1 (NOT v1beta1)
//   - VAPs failurePolicy is explicit
//   - Bug #33 protection: pod-targeting VAPs MUST exempt system actors
//     via matchConditions (else namespace-controller deadlocks on delete)
package loopzeroadmissionpolicies_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/getloopscale/loopzero/infra/tests/helm/chartrender"
	"sigs.k8s.io/yaml"
)

// The default render — what every cell install receives. The counts were 6/6/2
// and had gone stale: loopzero-require-image-digest (T2.3, default ON in warn mode)
// is the seventh policy and binding, and loopzero-harbor-mirror-alerts the third
// PrometheusRule (with loopzero-policy-controller-alerts and
// loopzero-tenant-namespace-alerts). Measured 2026-09-25 by rendering the chart.
const (
	chartName        = "loopzero-admission-policies"
	releaseName      = "loopzero-admission-policies-test"
	expectedVAPs     = 7
	expectedBindings = 7
	expectedCIPs     = 1
	expectedPRs      = 3
)

// VAP minimal schema — full type is in k8s.io/api/admissionregistration/v1
// (vendored via terratest's deps).
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
				APIGroups   []string `json:"apiGroups"`
				APIVersions []string `json:"apiVersions"`
				Operations  []string `json:"operations"`
				Resources   []string `json:"resources"`
			} `json:"resourceRules"`
		} `json:"matchConstraints"`
		MatchConditions []struct {
			Name       string `json:"name"`
			Expression string `json:"expression"`
		} `json:"matchConditions,omitempty"`
		Validations []struct {
			Expression string `json:"expression"`
			Message    string `json:"message"`
		} `json:"validations"`
	} `json:"spec"`
}

type vapBindingDoc struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		PolicyName        string   `json:"policyName"`
		ValidationActions []string `json:"validationActions"`
	} `json:"spec"`
}

func renderAll(t *testing.T) string {
	t.Helper()
	chartPath, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	return chartrender.RenderTemplate(t, opts, chartPath, releaseName, nil)
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

func renderBindings(t *testing.T) []vapBindingDoc {
	t.Helper()
	out := chartrender.SplitYAMLByKind(renderAll(t), "ValidatingAdmissionPolicyBinding")
	bs := make([]vapBindingDoc, 0, len(out))
	for _, d := range out {
		var b vapBindingDoc
		require.NoError(t, yaml.Unmarshal([]byte(d), &b))
		bs = append(bs, b)
	}
	return bs
}

func TestAdmission_VAPCount(t *testing.T) {
	t.Parallel()
	require.Equalf(t, expectedVAPs, len(renderVAPs(t)),
		"VAP count drifted — want %d, got %d", expectedVAPs, len(renderVAPs(t)))
}

func TestAdmission_VAPBindingCount(t *testing.T) {
	t.Parallel()
	require.Equalf(t, expectedBindings, len(renderBindings(t)),
		"VAPBinding count drifted — want %d, got %d",
		expectedBindings, len(renderBindings(t)))
}

func TestAdmission_VAPandBindingCountsMatch(t *testing.T) {
	t.Parallel()
	require.Equal(t, len(renderVAPs(t)), len(renderBindings(t)),
		"Each VAP MUST have exactly one VAPBinding — counts diverged")
}

func TestAdmission_BindingsReferenceExistingVAPs(t *testing.T) {
	t.Parallel()
	vapNames := make(map[string]bool)
	for _, v := range renderVAPs(t) {
		vapNames[v.Metadata.Name] = true
	}
	for _, b := range renderBindings(t) {
		require.Truef(t, vapNames[b.Spec.PolicyName],
			"VAPBinding %q references nonexistent VAP %q — binding "+
				"would be inert and policy NOT enforced",
			b.Metadata.Name, b.Spec.PolicyName)
	}
}

func TestAdmission_AllVAPsUseV1(t *testing.T) {
	t.Parallel()
	for _, v := range renderVAPs(t) {
		require.Equalf(t, "admissionregistration.k8s.io/v1", v.APIVersion,
			"VAP %q must use admissionregistration.k8s.io/v1 "+
				"(v1beta1 is deprecated, v1alpha1 removed)", v.Metadata.Name)
	}
}

func TestAdmission_AllBindingsUseV1(t *testing.T) {
	t.Parallel()
	for _, b := range renderBindings(t) {
		require.Equalf(t, "admissionregistration.k8s.io/v1", b.APIVersion,
			"VAPBinding %q must use admissionregistration.k8s.io/v1",
			b.Metadata.Name)
	}
}

func TestAdmission_AllVAPsHaveExplicitFailurePolicy(t *testing.T) {
	t.Parallel()
	for _, v := range renderVAPs(t) {
		require.NotEmptyf(t, v.Spec.FailurePolicy,
			"VAP %q missing failurePolicy — K8s defaults to Fail which "+
				"can deadlock namespace-controller (Bug #33 class). "+
				"Set to Ignore explicitly or guard with matchConditions.",
			v.Metadata.Name)
		require.Containsf(t, []string{"Fail", "Ignore"}, v.Spec.FailurePolicy,
			"VAP %q failurePolicy %q invalid", v.Metadata.Name, v.Spec.FailurePolicy)
	}
}

func TestAdmission_AllBindingsHaveValidationActions(t *testing.T) {
	t.Parallel()
	for _, b := range renderBindings(t) {
		require.NotEmptyf(t, b.Spec.ValidationActions,
			"VAPBinding %q missing validationActions — defaults to "+
				"empty which means policy is silently NOT enforced",
			b.Metadata.Name)
		for _, a := range b.Spec.ValidationActions {
			require.Containsf(t,
				[]string{"Deny", "Warn", "Audit"}, a,
				"VAPBinding %q has invalid action %q",
				b.Metadata.Name, a)
		}
	}
}

func TestAdmission_PodTargetingVAPsExemptSystemActors(t *testing.T) {
	t.Parallel()
	// Bug #33 protection: VAPs that match Pods MUST have matchConditions
	// excluding the system service accounts that perform GC during
	// namespace deletion (kube-controller-manager, namespace-controller,
	// or use system:masters group). Without this, deleting a namespace
	// can deadlock because the system tries to GC pods but VAP blocks.
	//
	// We allow the exemption to take two forms:
	//   1. failurePolicy: Ignore (system actions never fail)
	//   2. matchConditions with system: prefix references
	for _, v := range renderVAPs(t) {
		targetsPods := false
		for _, rr := range v.Spec.MatchConstraints.ResourceRules {
			for _, r := range rr.Resources {
				if r == "pods" {
					targetsPods = true
				}
			}
		}
		if !targetsPods {
			continue
		}
		if v.Spec.FailurePolicy == "Ignore" {
			continue // exempt — system actions never deny
		}
		// failurePolicy: Fail — require explicit matchConditions exempting system
		require.NotEmptyf(t, v.Spec.MatchConditions,
			"VAP %q targets pods with failurePolicy=Fail but has no "+
				"matchConditions — namespace deletion can deadlock "+
				"(Bug #33 class)", v.Metadata.Name)
		hasSystemExempt := false
		for _, mc := range v.Spec.MatchConditions {
			// CEL expression should reference request.userInfo.username
			// or system: prefix
			if containsAny(mc.Expression, "system:", "userInfo", "kube-system") {
				hasSystemExempt = true
				break
			}
		}
		require.Truef(t, hasSystemExempt,
			"VAP %q targets pods with failurePolicy=Fail but matchConditions "+
				"do not exempt system actors (Bug #33 class)",
			v.Metadata.Name)
	}
}

func TestAdmission_ClusterImagePolicyPresent(t *testing.T) {
	t.Parallel()
	docs := chartrender.SplitYAMLByKind(renderAll(t), "ClusterImagePolicy")
	require.Equalf(t, expectedCIPs, len(docs),
		"ClusterImagePolicy count drifted — want %d (cosign signature "+
			"gate), got %d", expectedCIPs, len(docs))
}

func TestAdmission_PrometheusRuleCount(t *testing.T) {
	t.Parallel()
	docs := chartrender.SplitYAMLByKind(renderAll(t), "PrometheusRule")
	require.Equalf(t, expectedPRs, len(docs),
		"PrometheusRule count drifted — want %d (policy-controller-alerts "+
			"+ tenant-namespace-alerts), got %d", expectedPRs, len(docs))
}

func containsAny(s string, needles ...string) bool {
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
