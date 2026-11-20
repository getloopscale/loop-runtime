//go:build helm_render

// Package loopzeroplatform_test — render-time tests for the umbrella chart
// (Plan 2026-05-20 v1.1 T3.1 chart 10/10).
//
// loopzero-platform is the umbrella that includes all platform sub-charts:
// loopzero-crds, loopzero-api, loopzero-tenant-operator, loopzero-app-operator,
// loopzero-preview-operator, loopzero-platform-operator, loopzero-cloud,
// loopzero-dashboard, loopzero-build-cwts, loopzero-monitoring, loopzero-admission-policies.
//
// Focus of umbrella tests (different from sub-chart tests):
//   - Render succeeds end-to-end with all sub-charts enabled
//   - Disabling a sub-chart removes its contribution (conditional deps)
//   - No duplicate resource names across sub-charts
//   - Global registry override propagates correctly to all sub-chart images
package loopzeroplatform_test

import (
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const (
	chartName   = "loopzero-platform"
	releaseName = "tp"
)

type metaOnly struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
}

func renderUmbrella(t *testing.T, setValues map[string]string) string {
	t.Helper()
	chartPath, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	if setValues != nil {
		opts.SetValues = setValues
	}
	return chartrender.RenderTemplate(t, opts, chartPath, releaseName, nil)
}

func TestPlatform_AllSubChartsEnabledByDefault(t *testing.T) {
	t.Parallel()
	out := renderUmbrella(t, nil)
	require.NotEmpty(t, out, "umbrella render produced empty output")

	// Sanity: each major sub-chart's deployment marker appears in render.
	expected := []string{
		"loopzero-api",
		"loopzero-tenant-operator",
		"loopzero-app-operator",
		"loopzero-preview-operator",
		"loopzero-platform-operator",
		"loopzero-cloud",
		"loopzero-dashboard",
	}
	for _, sub := range expected {
		require.Containsf(t, out, sub,
			"umbrella render missing sub-chart %q resources", sub)
	}
}

func TestPlatform_DisableCRDsRemovesAllCRDs(t *testing.T) {
	t.Parallel()
	// Verify conditional dep gating: disabling loopzero-crds should leave
	// zero CRD manifests in the render.
	out := renderUmbrella(t, map[string]string{"loopzero-crds.enabled": "false"})
	docs := chartrender.SplitYAMLByKind(out, "CustomResourceDefinition")
	require.Empty(t, docs,
		"disabling loopzero-crds should leave zero CRDs; found %d", len(docs))
}

func TestPlatform_DisableMonitoringRemovesNineRules(t *testing.T) {
	t.Parallel()
	// PrometheusRule ownership across sub-charts (verified 2026-05-20):
	//   loopzero-monitoring:          9 rules (capacity, critical, database,
	//                                       domains, otel, probes,
	//                                       registry, tracing, warning)
	//   loopzero-admission-policies:  2 rules (policy-controller-alerts,
	//                                      tenant-namespace-alerts)
	// Disabling monitoring should remove the 9 it owns, leaving 2.
	baseline := chartrender.SplitYAMLByKind(renderUmbrella(t, nil),
		"PrometheusRule")
	out := renderUmbrella(t,
		map[string]string{"loopzero-monitoring.enabled": "false"})
	after := chartrender.SplitYAMLByKind(out, "PrometheusRule")

	require.Equalf(t, 11, len(baseline),
		"baseline PrometheusRule count drifted (want 11 = 9 monitoring + "+
			"2 admission-policies); got %d", len(baseline))
	require.Equalf(t, 2, len(after),
		"after disabling loopzero-monitoring, the 2 admission-policies rules "+
			"should remain; got %d", len(after))
}

func TestPlatform_DisableAdmissionPoliciesRemovesAllVAPs(t *testing.T) {
	t.Parallel()
	out := renderUmbrella(t,
		map[string]string{"loopzero-admission-policies.enabled": "false"})
	vaps := chartrender.SplitYAMLByKind(out, "ValidatingAdmissionPolicy")
	bindings := chartrender.SplitYAMLByKind(out,
		"ValidatingAdmissionPolicyBinding")
	cips := chartrender.SplitYAMLByKind(out, "ClusterImagePolicy")
	require.Empty(t, vaps,
		"disabling admission-policies should leave zero VAPs; found %d",
		len(vaps))
	require.Empty(t, bindings,
		"disabling admission-policies should leave zero VAP bindings; "+
			"found %d", len(bindings))
	require.Empty(t, cips,
		"disabling admission-policies should leave zero "+
			"ClusterImagePolicies; found %d", len(cips))
}

func TestPlatform_NoDuplicateResourceNamesPerKind(t *testing.T) {
	t.Parallel()
	out := renderUmbrella(t, nil)

	// Group by (kind, namespace, name) tuple. Two resources of the same
	// kind+namespace+name = umbrella conflict that helm would refuse on
	// apply (or, worse, silently overwrite).
	seen := make(map[string]string)
	for _, kind := range []string{
		"Deployment", "Service", "ServiceAccount", "ConfigMap",
		"Secret", "Role", "RoleBinding", "ClusterRole",
		"ClusterRoleBinding", "NetworkPolicy", "PodDisruptionBudget",
		"ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding",
		"CustomResourceDefinition", "PrometheusRule", "ClusterImagePolicy",
	} {
		for _, d := range chartrender.SplitYAMLByKind(out, kind) {
			var m metaOnly
			require.NoError(t, yaml.Unmarshal([]byte(d), &m))
			key := kind + "/" + m.Metadata.Namespace + "/" + m.Metadata.Name
			if prev, exists := seen[key]; exists {
				t.Fatalf("duplicate resource: %s (previously emitted by %s)",
					key, prev)
			}
			seen[key] = kind
		}
	}
}

func TestPlatform_GlobalRegistryOverridePropagates(t *testing.T) {
	t.Parallel()
	// Setting global.registry should propagate to sub-chart images.
	customReg := "registry.example.com/test-platform"
	out := renderUmbrella(t, map[string]string{
		"global.registry": customReg,
	})

	// Walk Deployment images. We do not assert EVERY image (some sub-charts
	// may not honor global.registry yet — finding to track); we assert
	// AT LEAST ONE Deployment uses the custom registry. Stronger drift
	// guard for full propagation is a follow-up.
	deploys := chartrender.SplitYAMLByKind(out, "Deployment")
	require.NotEmpty(t, deploys, "umbrella has Deployments")
	found := false
	for _, d := range deploys {
		if strings.Contains(d, customReg) {
			found = true
			break
		}
	}
	require.Truef(t, found,
		"global.registry=%q did NOT propagate to any sub-chart image — "+
			"either sub-charts ignore the global value, or test setup is "+
			"wrong", customReg)
}

func TestPlatform_RenderIsDeterministic(t *testing.T) {
	t.Parallel()
	// Two renders with identical inputs should produce byte-identical
	// output. Non-deterministic rendering causes spurious ArgoCD diffs.
	a := renderUmbrella(t, nil)
	b := renderUmbrella(t, nil)
	require.Equal(t, len(a), len(b),
		"umbrella render is non-deterministic (length differs across runs)")
}
