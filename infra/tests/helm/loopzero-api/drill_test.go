//go:build helm_render

package loopzeroapi_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/getloopscale/loopzero/infra/tests/helm/chartrender"
	appsV1 "k8s.io/api/apps/v1"
)

const drillEnvStub = "infra/tests/helm/values/drill/loopzero-api.yaml"

func renderDrillDeployment(t *testing.T) appsV1.Deployment {
	t.Helper()

	chartPath, err := chartrender.BootstrapChartPath(chartName)
	require.NoError(t, err)

	opts, err := chartrender.NewOptions(chartPath, drillEnvStub)
	require.NoError(t, err)

	output := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/deployment.yaml"})
	var deploy appsV1.Deployment
	chartrender.MustUnmarshalK8SYamlByKind(t, output, "Deployment", &deploy)
	return deploy
}

// TestLoopZeroAPI_Drill_DOCR — drill uses prod DOCR namespace.
func TestLoopZeroAPI_Drill_DOCR(t *testing.T) {
	t.Parallel()

	deploy := renderDrillDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	image := deploy.Spec.Template.Spec.Containers[0].Image
	require.True(t, strings.HasPrefix(image, "registry.digitalocean.com/loopzero-system/"),
		"drill image must use DOCR loopzero-system namespace; got %q", image)
}

// TestLoopZeroAPI_Drill_NoLatestTag — Bug #16 protection: DOCR rejects :latest
// tag pulls. Drill must use immutable tag.
func TestLoopZeroAPI_Drill_NoLatestTag(t *testing.T) {
	t.Parallel()

	deploy := renderDrillDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	image := deploy.Spec.Template.Spec.Containers[0].Image
	require.False(t, strings.HasSuffix(image, ":latest"),
		"drill image must NOT use :latest (Bug #16 — DOCR rejects); got %q", image)
}

// TestLoopZeroAPI_Drill_HA — drill uses multi-replica HA (Bug #18 validation).
func TestLoopZeroAPI_Drill_HA(t *testing.T) {
	t.Parallel()

	deploy := renderDrillDeployment(t)
	require.NotNil(t, deploy.Spec.Replicas)
	require.GreaterOrEqual(t, int(*deploy.Spec.Replicas), 2,
		"drill must use multi-replica HA (Bug #18 leader election validated iter 12)")
}

// TestLoopZeroAPI_Drill_PullSecret — drill needs DOCR pull credentials.
func TestLoopZeroAPI_Drill_PullSecret(t *testing.T) {
	t.Parallel()

	deploy := renderDrillDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.ImagePullSecrets,
		"drill must specify imagePullSecrets for DOCR auth")

	foundRegistryPull := false
	for _, s := range deploy.Spec.Template.Spec.ImagePullSecrets {
		if s.Name == "loopzero-registry-pull" {
			foundRegistryPull = true
			break
		}
	}
	require.True(t, foundRegistryPull,
		"drill must reference loopzero-registry-pull Secret")
}

// TestLoopZeroAPI_Drill_Domain — drill uses CF Zone domain.
func TestLoopZeroAPI_Drill_Domain(t *testing.T) {
	t.Parallel()

	// Domain typically appears in IngressRoute template, not Deployment.
	// Validate from IngressRoute render.
	chartPath, err := chartrender.BootstrapChartPath(chartName)
	require.NoError(t, err)

	opts, err := chartrender.NewOptions(chartPath, drillEnvStub)
	require.NoError(t, err)

	output := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/ingressroute.yaml"})
	require.Contains(t, output, "loopzero-drill.example",
		"drill ingressroute must use drill CF Zone domain")
}
