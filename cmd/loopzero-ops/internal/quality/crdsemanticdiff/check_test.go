// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package crdsemanticdiff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/scanresult"
)

func setup(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}
	return root
}

// =============================================================================
// shortNameFromPath
// =============================================================================

func TestShortNameFromPath(t *testing.T) {
	assert.Equal(t, "tenants", shortNameFromPath("operators/config/crd/bases/loopzero.io_tenants.yaml"))
	assert.Equal(t, "applications", shortNameFromPath("/abs/loopzero.io_applications.yaml"))
	assert.Equal(t, "previews", shortNameFromPath("loopzero.io_previews.yaml"))
}

// =============================================================================
// parseSourceCRD
// =============================================================================

func TestParseSourceCRD(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "loopzero.io_tenants.yaml")
	body := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tenants.loopzero.io
spec:
  group: loopzero.io
  scope: Namespaced
  names:
    kind: Tenant
    plural: tenants
`
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	crd, err := parseSourceCRD(p)
	require.NoError(t, err)
	assert.Equal(t, "CustomResourceDefinition", crd.Kind)
	assert.Equal(t, "tenants.loopzero.io", crd.Metadata.Name)
	assert.Equal(t, "loopzero.io", crd.Spec["group"])
}

func TestParseSourceCRD_MissingFile(t *testing.T) {
	_, err := parseSourceCRD("/tmp/zzz-no-crd-file.yaml")
	require.Error(t, err)
}

// =============================================================================
// parseHelmRender
// =============================================================================

func TestParseHelmRender_MultiDoc(t *testing.T) {
	raw := `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tenants.loopzero.io
spec:
  group: loopzero.io
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: not-a-crd
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: applications.loopzero.io
spec:
  group: loopzero.io
`
	out, err := parseHelmRender(raw)
	require.NoError(t, err)
	assert.Len(t, out, 2, "non-CRD doc must be filtered out")
	assert.Contains(t, out, "tenants.loopzero.io")
	assert.Contains(t, out, "applications.loopzero.io")
}

func TestParseHelmRender_Empty(t *testing.T) {
	out, err := parseHelmRender("")
	require.NoError(t, err)
	assert.Empty(t, out)
}

// =============================================================================
// normalizeSpec — controller-gen annotation stripping
// =============================================================================

func TestNormalizeSpec_StripsControllerGenAnnotation(t *testing.T) {
	spec1 := map[string]interface{}{
		"group": "loopzero.io",
		"versions": []interface{}{
			map[string]interface{}{
				"schema": map[string]interface{}{
					"openAPIV3Schema": map[string]interface{}{
						"controller-gen.kubebuilder.io/version": "v0.13.0",
						"someOtherField":                        "x",
					},
				},
			},
		},
	}
	spec2 := map[string]interface{}{
		"group": "loopzero.io",
		"versions": []interface{}{
			map[string]interface{}{
				"schema": map[string]interface{}{
					"openAPIV3Schema": map[string]interface{}{
						"controller-gen.kubebuilder.io/version": "v0.14.0",
						"someOtherField":                        "x",
					},
				},
			},
		},
	}
	n1 := normalizeSpec(spec1)
	n2 := normalizeSpec(spec2)
	// The controller-gen-noise child gets wiped → equal results.
	assert.Equal(t, n1, n2, "version-only annotation difference must normalize away")
}

func TestNormalizeSpec_PreservesRealDifferences(t *testing.T) {
	spec1 := map[string]interface{}{"group": "loopzero.io"}
	spec2 := map[string]interface{}{"group": "other.io"}
	assert.NotEqual(t, normalizeSpec(spec1), normalizeSpec(spec2))
}

// =============================================================================
// listSourceFiles
// =============================================================================

func TestListSourceFiles_FiltersPrefixAndSuffix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "loopzero.io_tenants.yaml"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "loopzero.io_apps.yaml"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other_prefix.yaml"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "loopzero.io_tenants.yml"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte(""), 0o600))

	files, err := listSourceFiles(dir)
	require.NoError(t, err)
	assert.Len(t, files, 2)
	assert.Contains(t, files[0], "loopzero.io_apps.yaml")
	assert.Contains(t, files[1], "loopzero.io_tenants.yaml")
}

// =============================================================================
// Check — end-to-end with HelmRenderOverride bypass
// =============================================================================

func TestCheck_AllInSync(t *testing.T) {
	root := setup(t, map[string]string{
		"operators/config/crd/bases/loopzero.io_tenants.yaml": `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tenants.loopzero.io
spec:
  group: loopzero.io
  scope: Namespaced
`,
	})
	override := `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tenants.loopzero.io
spec:
  group: loopzero.io
  scope: Namespaced
`
	out, err := Check(Config{
		Root:               root,
		HelmRenderOverride: override,
	})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Equal(t, 1, out.Scanned)
	assert.Empty(t, out.Drifts)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "semantic sync")
}

func TestCheck_SpecMismatchDetected(t *testing.T) {
	root := setup(t, map[string]string{
		"operators/config/crd/bases/loopzero.io_tenants.yaml": `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tenants.loopzero.io
spec:
  group: loopzero.io
  scope: Namespaced
`,
	})
	override := `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tenants.loopzero.io
spec:
  group: loopzero.io
  scope: Cluster
`
	out, err := Check(Config{
		Root:               root,
		HelmRenderOverride: override,
	})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	require.Len(t, out.Drifts, 1)
	assert.Equal(t, "tenants", out.Drifts[0].ShortName)
	assert.Equal(t, "spec mismatch", out.Drifts[0].Reason)
}

func TestCheck_ChartMissingCRD(t *testing.T) {
	root := setup(t, map[string]string{
		"operators/config/crd/bases/loopzero.io_tenants.yaml": `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tenants.loopzero.io
spec:
  group: loopzero.io
`,
	})
	// Chart render has no tenant CRD.
	override := `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: irrelevant
`
	out, err := Check(Config{
		Root:               root,
		HelmRenderOverride: override,
	})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	require.Len(t, out.Drifts, 1)
	assert.Equal(t, "chart template missing", out.Drifts[0].Reason)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "missing CRD")
}

func TestCheck_CRDFilter(t *testing.T) {
	root := setup(t, map[string]string{
		"operators/config/crd/bases/loopzero.io_tenants.yaml":      "apiVersion: x\nkind: CustomResourceDefinition\nmetadata:\n  name: tenants.loopzero.io\nspec:\n  group: loopzero.io\n",
		"operators/config/crd/bases/loopzero.io_applications.yaml": "apiVersion: x\nkind: CustomResourceDefinition\nmetadata:\n  name: applications.loopzero.io\nspec:\n  group: loopzero.io\n",
	})
	override := `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tenants.loopzero.io
spec:
  group: loopzero.io
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: applications.loopzero.io
spec:
  group: loopzero.io
`
	out, err := Check(Config{
		Root:               root,
		HelmRenderOverride: override,
		CRDFilter:          "tenants",
	})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Equal(t, 1, out.Scanned, "only the filtered CRD is scanned")
}

// A root without the operator CRD sources is a misconfigured run: the gate
// compared nothing and must not print PASS.
func TestCheck_MissingSourceDirIsAnError(t *testing.T) {
	_, err := Check(Config{Root: t.TempDir(), HelmRenderOverride: "---\n"})
	require.ErrorIs(t, err, scanresult.ErrNothingScanned)
}

// A --crd filter that matches no source CRD compared nothing; reporting "All 0
// CRDs are in semantic sync" would read as a pass for a typo.
func TestCheck_FilterMatchingNothingIsAnError(t *testing.T) {
	root := setup(t, map[string]string{
		"operators/config/crd/bases/loopzero.io_tenants.yaml": "apiVersion: x\nkind: CustomResourceDefinition\nmetadata:\n  name: tenants.loopzero.io\nspec:\n  group: loopzero.io\n",
	})
	_, err := Check(Config{Root: root, HelmRenderOverride: "---\n", CRDFilter: "tenant"})
	require.ErrorIs(t, err, scanresult.ErrNothingScanned)
}

// A stream cut mid-document is a decode error, not the end of the stream.
func TestParseHelmRender_TruncatedDocumentIsAnError(t *testing.T) {
	_, err := parseHelmRender("kind: CustomResourceDefinition\nmetadata:\n  name: \"tenants.loopzero.io")
	require.Error(t, err)
}

func TestCheck_VerboseAppendsDiff(t *testing.T) {
	root := setup(t, map[string]string{
		"operators/config/crd/bases/loopzero.io_tenants.yaml": `apiVersion: x
kind: CustomResourceDefinition
metadata:
  name: tenants.loopzero.io
spec:
  group: loopzero.io
  scope: Namespaced
`,
	})
	override := `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tenants.loopzero.io
spec:
  group: loopzero.io
  scope: Cluster
`
	out, err := Check(Config{
		Root:               root,
		HelmRenderOverride: override,
		Verbose:            true,
	})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	joined := strings.Join(out.Messages, "\n")
	assert.Contains(t, joined, "--- source")
	assert.Contains(t, joined, "--- chart")
}

// =============================================================================
// indent helper
// =============================================================================

func TestIndent(t *testing.T) {
	got := indent("a\nb\nc", "  ")
	assert.Equal(t, "  a\n  b\n  c", got)
}

// =============================================================================
// resolvePath
// =============================================================================

func TestResolvePath(t *testing.T) {
	assert.Equal(t, "/repo/operators/config/crd/bases",
		resolvePath("/repo", "operators/config/crd/bases"))
	assert.Equal(t, "/abs/path", resolvePath("/repo", "/abs/path"))
}

// =============================================================================
// Constants
// =============================================================================

func TestDefaults(t *testing.T) {
	assert.Equal(t, "operators/config/crd/bases", DefaultSourceDir)
	assert.Equal(t, "charts/loopzero-crds", DefaultChartPath)
	assert.Equal(t, "loopzero.io_", SourcePrefix)
}

// =============================================================================
// Extra fault-tolerance + coverage uplift
// =============================================================================

func TestRenderChart_OverridePath(t *testing.T) {
	got, err := renderChart("/any/path", "explicit-render")
	require.NoError(t, err)
	assert.Equal(t, "explicit-render", got)
}

func TestParseHelmRender_MalformedYAMLPropagates(t *testing.T) {
	_, err := parseHelmRender("not: : valid : yaml\n  ::: :")
	require.Error(t, err)
}

func TestResolvePath_EmptyRoot(t *testing.T) {
	assert.Equal(t, "operators/config/crd/bases",
		resolvePath("", "operators/config/crd/bases"))
}

func TestNormalizeSpec_NonMapKeys(t *testing.T) {
	// scalar input — walkAndStrip must not panic.
	spec := map[string]interface{}{
		"group": "loopzero.io",
		"list":  []interface{}{"a", "b", 42},
	}
	out := normalizeSpec(spec)
	assert.Contains(t, out, "group: loopzero.io")
	assert.Contains(t, out, "list")
}

func TestParseSourceCRD_MalformedYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "loopzero.io_broken.yaml")
	require.NoError(t, os.WriteFile(p, []byte("not: : valid"), 0o600))
	_, err := parseSourceCRD(p)
	require.Error(t, err)
}

func TestListSourceFiles_MissingDir(t *testing.T) {
	_, err := listSourceFiles("/tmp/zzz-no-crd-bases")
	require.Error(t, err)
}

// Note: renderChart's override hook is covered by TestRenderChart_OverridePath
// above. The real `helm template` exec path runs in the helm-charts.yml CI gate
// (helm present); unit tests stay hermetic via the override.
