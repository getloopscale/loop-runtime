//go:build helm_render

// Package harbor_test — Plan 2026-05-20 v1.1 T3.2 (7/8).
//
// harbor is LoopZero's container registry (per MEET-2026-04-11-048 Harbor
// migration, currently in dual-registry transition with DOCR). The
// wrapper chart pulls in upstream Harbor + adds LoopZero customizations.
package harbor_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/getloopscale/loopzero/infra/tests/helm/chartrender"
)

const (
	chartName   = "harbor"
	releaseName = "harbor-test"
)

func renderAll(t *testing.T) string {
	t.Helper()
	cp, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(cp, "")
	require.NoError(t, err)
	return chartrender.RenderTemplate(t, opts, cp, releaseName, nil)
}

func TestHarbor_HasCoreDeployments(t *testing.T) {
	t.Parallel()
	deploys := chartrender.SplitYAMLByKind(renderAll(t), "Deployment")
	require.GreaterOrEqualf(t, len(deploys), 5,
		"Harbor requires >=5 Deployments (core, portal, jobservice, "+
			"registry, registryctl); got %d", len(deploys))
}

func TestHarbor_HasStatefulSet(t *testing.T) {
	t.Parallel()
	// Database (PostgreSQL) runs as StatefulSet for stable identity.
	ss := chartrender.SplitYAMLByKind(renderAll(t), "StatefulSet")
	require.NotEmpty(t, ss,
		"Harbor must declare StatefulSet (DB needs stable identity + "+
			"persistent volume)")
}

func TestHarbor_HasPersistentVolumeClaim(t *testing.T) {
	t.Parallel()
	pvc := chartrender.SplitYAMLByKind(renderAll(t), "PersistentVolumeClaim")
	require.NotEmpty(t, pvc,
		"Harbor needs persistent storage for image blobs + scan results")
}

func TestHarbor_HasNetworkPolicy(t *testing.T) {
	t.Parallel()
	np := chartrender.SplitYAMLByKind(renderAll(t), "NetworkPolicy")
	require.NotEmpty(t, np,
		"Harbor must enforce NetworkPolicy (registry attack surface = "+
			"image supply chain compromise risk)")
}

func TestHarbor_HasPodDisruptionBudget(t *testing.T) {
	t.Parallel()
	pdb := chartrender.SplitYAMLByKind(renderAll(t), "PodDisruptionBudget")
	require.NotEmpty(t, pdb,
		"Harbor PDB required (prevents drain-time registry outage)")
}

func TestHarbor_HasSecretsForCredentials(t *testing.T) {
	t.Parallel()
	secrets := chartrender.SplitYAMLByKind(renderAll(t), "Secret")
	require.NotEmpty(t, secrets,
		"Harbor must declare Secrets (admin password, DB password, "+
			"registry storage keys)")
}

func TestHarbor_ServiceCountSufficient(t *testing.T) {
	t.Parallel()
	svcs := chartrender.SplitYAMLByKind(renderAll(t), "Service")
	require.GreaterOrEqualf(t, len(svcs), 5,
		"Harbor needs services for each core component; got %d only",
		len(svcs))
}
