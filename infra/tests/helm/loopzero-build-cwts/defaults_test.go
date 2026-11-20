//go:build helm_render

// Package loopzerobuildcwts_test — Plan 2026-05-20 v1.1 T3.2 (4/8).
//
// loopzero-build-cwts emits the ClusterWorkflowTemplates used by Argo
// Workflows for build pipelines, plus RBAC + LimitRange + ResourceQuota
// for the build namespace.
package loopzerobuildcwts_test

import (
	"testing"

	"github.com/getloopscale/loopzero/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const (
	chartName      = "loopzero-build-cwts"
	releaseName    = "tbc"
	expectedCWTs   = 3
	expectedQuotas = 1
	expectedLimits = 1
)

type cwtDoc struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Templates []struct {
			Name string `json:"name"`
		} `json:"templates"`
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

func renderCWTs(t *testing.T) []cwtDoc {
	t.Helper()
	docs := chartrender.SplitYAMLByKind(renderAll(t), "ClusterWorkflowTemplate")
	cwts := make([]cwtDoc, 0, len(docs))
	for _, d := range docs {
		var c cwtDoc
		require.NoError(t, yaml.Unmarshal([]byte(d), &c))
		cwts = append(cwts, c)
	}
	return cwts
}

func TestBuildCWTs_CWTCount(t *testing.T) {
	t.Parallel()
	require.Equalf(t, expectedCWTs, len(renderCWTs(t)),
		"CWT count drifted — want %d, got %d", expectedCWTs, len(renderCWTs(t)))
}

func TestBuildCWTs_AllUseArgoprojV1(t *testing.T) {
	t.Parallel()
	for _, c := range renderCWTs(t) {
		require.Equalf(t, "argoproj.io/v1alpha1", c.APIVersion,
			"CWT %q must use argoproj.io/v1alpha1", c.Metadata.Name)
	}
}

func TestBuildCWTs_AllHaveTemplates(t *testing.T) {
	t.Parallel()
	for _, c := range renderCWTs(t) {
		require.NotEmptyf(t, c.Spec.Templates,
			"CWT %q must have at least one inner template", c.Metadata.Name)
	}
}

func TestBuildCWTs_ResourceQuotaPresent(t *testing.T) {
	t.Parallel()
	docs := chartrender.SplitYAMLByKind(renderAll(t), "ResourceQuota")
	require.Equalf(t, expectedQuotas, len(docs),
		"build namespace MUST have ResourceQuota (prevents runaway build "+
			"resource consumption); want %d got %d", expectedQuotas, len(docs))
}

func TestBuildCWTs_LimitRangePresent(t *testing.T) {
	t.Parallel()
	docs := chartrender.SplitYAMLByKind(renderAll(t), "LimitRange")
	require.Equalf(t, expectedLimits, len(docs),
		"build namespace MUST have LimitRange (default pod resource "+
			"limits); want %d got %d", expectedLimits, len(docs))
}

func TestBuildCWTs_RBACFullyDefined(t *testing.T) {
	t.Parallel()
	out := renderAll(t)
	for _, kind := range []string{"ServiceAccount", "Role", "RoleBinding",
		"ClusterRole", "ClusterRoleBinding"} {
		docs := chartrender.SplitYAMLByKind(out, kind)
		require.NotEmptyf(t, docs,
			"build-cwts must define %s (Argo Workflows requires "+
				"explicit RBAC; missing = pods can't start)", kind)
	}
}

func TestBuildCWTs_NetworkPolicyPresent(t *testing.T) {
	t.Parallel()
	docs := chartrender.SplitYAMLByKind(renderAll(t), "NetworkPolicy")
	require.NotEmpty(t, docs,
		"build namespace MUST have NetworkPolicy (build pods need scoped "+
			"egress: only DOCR + Gitea + DNS)")
}
