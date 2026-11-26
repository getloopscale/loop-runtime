//go:build helm_render

// Package loopzerocrds_test — render-time tests for the loopzero-crds chart
// (Plan 2026-05-20 v1.1 T3.1 chart 7/10).
//
// loopzero-crds emits ONLY CustomResourceDefinition manifests for the 8 wired
// CRDs (per CLAUDE.md § CRDs and after the 2026-05-13 deletion of
// AppSecret + ReconciliationReport orphans). This test pkg validates:
//   - All 8 expected CRDs are present
//   - No orphan CRDs slipped back in (regression guard for 2026-05-13 ADR)
//   - All CRDs use apiVersion apiextensions.k8s.io/v1 (not v1beta1)
//   - All CRDs are scoped (Namespaced or Cluster, never empty)
//   - All CRDs use the loopzero.io API group
package loopzerocrds_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	apiextV1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

const (
	chartName   = "loopzero-crds"
	releaseName = "loopzero-crds-test"
	apiGroup    = "loopzero.io"
)

// expectedCRDs matches CLAUDE.md § Repository Structure (8 CRDs, all
// wired). 2 historical orphans (AppSecret + ReconciliationReport)
// deleted 2026-05-13 per ADR; if either reappears, this test FAILS.
var expectedCRDs = []string{
	"clustercredentials.loopzero.io",
	"environments.loopzero.io",
	"exposedroutes.loopzero.io",
	"gitrepositorybindings.loopzero.io",
	"platformbootstraps.loopzero.io",
	"previewenvironments.loopzero.io",
	"tenants.loopzero.io",
	"loopzeroapplications.loopzero.io",
}

// orphanCRDs were deleted 2026-05-13 (sponsor-direct-approval).
// Render output MUST NOT contain them.
var orphanCRDs = []string{
	"appsecrets.loopzero.io",
	"reconciliationreports.loopzero.io",
}

func renderAllCRDs(t *testing.T) []apiextV1.CustomResourceDefinition {
	t.Helper()
	chartPath, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	output := chartrender.RenderTemplate(t, opts, chartPath, releaseName, nil)

	docs := chartrender.SplitYAMLByKind(output, "CustomResourceDefinition")
	crds := make([]apiextV1.CustomResourceDefinition, 0, len(docs))
	for _, d := range docs {
		var crd apiextV1.CustomResourceDefinition
		require.NoError(t, yaml.Unmarshal([]byte(d), &crd))
		crds = append(crds, crd)
	}
	return crds
}

func TestLoopZeroCRDs_AllExpectedPresent(t *testing.T) {
	t.Parallel()
	got := make(map[string]bool)
	for _, c := range renderAllCRDs(t) {
		got[c.Name] = true
	}
	for _, want := range expectedCRDs {
		require.Truef(t, got[want], "expected CRD missing: %q", want)
	}
}

func TestLoopZeroCRDs_NoOrphansRegressed(t *testing.T) {
	t.Parallel()
	got := make(map[string]bool)
	for _, c := range renderAllCRDs(t) {
		got[c.Name] = true
	}
	for _, orphan := range orphanCRDs {
		require.Falsef(t, got[orphan],
			"orphan CRD %q regressed — was deleted 2026-05-13 per ADR "+
				"decisions/2026-05-13-orphan-crd-deletion.md", orphan)
	}
}

func TestLoopZeroCRDs_CountMatchesExpected(t *testing.T) {
	t.Parallel()
	crds := renderAllCRDs(t)
	require.Equalf(t, len(expectedCRDs), len(crds),
		"unexpected CRD count: want %d (canonical) got %d — if intentional, "+
			"update expectedCRDs slice + CLAUDE.md § CRDs count",
		len(expectedCRDs), len(crds))
}

func TestLoopZeroCRDs_AllUseAPIGroupLoopZeroIO(t *testing.T) {
	t.Parallel()
	for _, c := range renderAllCRDs(t) {
		require.Equalf(t, apiGroup, c.Spec.Group,
			"CRD %q must be in apiGroup %q, got %q",
			c.Name, apiGroup, c.Spec.Group)
	}
}

func TestLoopZeroCRDs_AllUseV1APIVersion(t *testing.T) {
	t.Parallel()
	for _, c := range renderAllCRDs(t) {
		require.Equalf(t, "apiextensions.k8s.io/v1", c.APIVersion,
			"CRD %q must use apiextensions.k8s.io/v1 (NOT deprecated "+
				"v1beta1 — removed in K8s 1.22)", c.Name)
	}
}

func TestLoopZeroCRDs_AllHaveScopeSet(t *testing.T) {
	t.Parallel()
	for _, c := range renderAllCRDs(t) {
		require.NotEmptyf(t, string(c.Spec.Scope),
			"CRD %q missing spec.scope (must be Namespaced or Cluster)",
			c.Name)
		require.Containsf(t,
			[]apiextV1.ResourceScope{apiextV1.NamespaceScoped, apiextV1.ClusterScoped},
			c.Spec.Scope,
			"CRD %q scope %q invalid", c.Name, c.Spec.Scope)
	}
}

func TestLoopZeroCRDs_AllHaveStoredVersion(t *testing.T) {
	t.Parallel()
	for _, c := range renderAllCRDs(t) {
		require.NotEmptyf(t, c.Spec.Versions,
			"CRD %q must declare at least one version", c.Name)
		storedCount := 0
		versionNames := make([]string, 0, len(c.Spec.Versions))
		for _, v := range c.Spec.Versions {
			versionNames = append(versionNames, v.Name)
			if v.Storage {
				storedCount++
			}
		}
		require.Equalf(t, 1, storedCount,
			"CRD %q must have exactly 1 storage version (got %d); "+
				"versions: %s", c.Name, storedCount,
			strings.Join(versionNames, ","))
	}
}

func TestLoopZeroCRDs_NamesMatchPlural(t *testing.T) {
	t.Parallel()
	// CRD metadata.name must equal `<plural>.<group>` per K8s spec
	for _, c := range renderAllCRDs(t) {
		want := c.Spec.Names.Plural + "." + c.Spec.Group
		require.Equalf(t, want, c.Name,
			"CRD name %q must equal <plural>.<group>=%q", c.Name, want)
	}
}

func TestLoopZeroCRDs_ListsSortedDeterministic(t *testing.T) {
	t.Parallel()
	// Template ordering should be deterministic across renders so
	// helmfile/ArgoCD don't see spurious diffs.
	crds1 := renderAllCRDs(t)
	crds2 := renderAllCRDs(t)
	require.Equal(t, len(crds1), len(crds2))
	names1 := make([]string, len(crds1))
	names2 := make([]string, len(crds2))
	for i, c := range crds1 {
		names1[i] = c.Name
	}
	for i, c := range crds2 {
		names2[i] = c.Name
	}
	sort.Strings(names1)
	sort.Strings(names2)
	require.Equal(t, names1, names2, "CRD list must be deterministic across renders")
}
