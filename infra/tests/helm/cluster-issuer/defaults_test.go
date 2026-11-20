//go:build helm_render

// Package clusterissuer_test — Plan 2026-05-20 v1.1 T3.2 (1/8).
package clusterissuer_test

import (
	"testing"

	"github.com/getloopscale/loopzero/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const (
	chartName   = "cluster-issuer"
	releaseName = "cluster-issuer-test"
)

type clusterIssuerDoc struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec map[string]interface{} `json:"spec"`
}

func render(t *testing.T) []clusterIssuerDoc {
	t.Helper()
	cp, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(cp, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, cp, releaseName, nil)
	docs := chartrender.SplitYAMLByKind(out, "ClusterIssuer")
	cis := make([]clusterIssuerDoc, 0, len(docs))
	for _, d := range docs {
		var c clusterIssuerDoc
		require.NoError(t, yaml.Unmarshal([]byte(d), &c))
		cis = append(cis, c)
	}
	return cis
}

func TestClusterIssuer_AtLeastOneExists(t *testing.T) {
	t.Parallel()
	cis := render(t)
	require.NotEmpty(t, cis, "must emit at least one ClusterIssuer")
}

func TestClusterIssuer_UsesCertManagerV1(t *testing.T) {
	t.Parallel()
	for _, c := range render(t) {
		require.Equalf(t, "cert-manager.io/v1", c.APIVersion,
			"ClusterIssuer %q must use cert-manager.io/v1", c.Metadata.Name)
	}
}

func TestClusterIssuer_HasIssuerType(t *testing.T) {
	t.Parallel()
	// Spec must have exactly one of: acme, selfSigned, ca, vault.
	validTypes := []string{"acme", "selfSigned", "ca", "vault"}
	for _, c := range render(t) {
		found := 0
		for _, k := range validTypes {
			if _, ok := c.Spec[k]; ok {
				found++
			}
		}
		require.Equalf(t, 1, found,
			"ClusterIssuer %q must declare exactly one issuer type "+
				"(%v); got %d", c.Metadata.Name, validTypes, found)
	}
}
