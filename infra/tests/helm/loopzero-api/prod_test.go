//go:build helm_render

package loopzeroapi_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/getloopscale/loopzero/infra/tests/helm/helpers"
	appsV1 "k8s.io/api/apps/v1"
)

const prodEnvStub = "infra/tests/helm/values/prod/loopzero-api.yaml"

func renderProdDeployment(t *testing.T) appsV1.Deployment {
	t.Helper()

	chartPath, err := helpers.ChartPath(chartName)
	require.NoError(t, err)

	opts, err := helpers.NewOptions(chartPath, prodEnvStub)
	require.NoError(t, err)

	output := helpers.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/deployment.yaml"})
	var deploy appsV1.Deployment
	helpers.MustUnmarshalK8SYamlByKind(t, output, "Deployment", &deploy)
	return deploy
}

// TestLoopZeroAPI_Prod_DOCR — prod uses DOCR loopzero-system namespace.
func TestLoopZeroAPI_Prod_DOCR(t *testing.T) {
	t.Parallel()

	deploy := renderProdDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	image := deploy.Spec.Template.Spec.Containers[0].Image
	require.True(t, strings.HasPrefix(image, "registry.digitalocean.com/loopzero-system/"),
		"prod image must use DOCR loopzero-system; got %q", image)
}

// TestLoopZeroAPI_Prod_SemverTag — prod uses immutable semver release.
func TestLoopZeroAPI_Prod_SemverTag(t *testing.T) {
	t.Parallel()

	deploy := renderProdDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	image := deploy.Spec.Template.Spec.Containers[0].Image
	// Tag must match v<digits>.<digits>.<digits>[-suffix]
	require.Regexp(t, `:v\d+\.\d+\.\d+(-[a-z0-9.-]+)?$`, image,
		"prod must use semver tag (immutable release); got %q", image)
}

// TestLoopZeroAPI_Prod_HigherReplicas — prod scales higher than dev/drill.
func TestLoopZeroAPI_Prod_HigherReplicas(t *testing.T) {
	t.Parallel()

	deploy := renderProdDeployment(t)
	require.NotNil(t, deploy.Spec.Replicas)
	require.GreaterOrEqual(t, int(*deploy.Spec.Replicas), 3,
		"prod scales >= 3 replicas (higher than dev=1, drill=2)")
}

// TestLoopZeroAPI_Prod_PublicHTTPSIssuer — prod JWT issuer is public HTTPS.
func TestLoopZeroAPI_Prod_PublicHTTPSIssuer(t *testing.T) {
	t.Parallel()

	deploy := renderProdDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	envVars := deploy.Spec.Template.Spec.Containers[0].Env
	var jwtIssuer string
	for _, e := range envVars {
		if e.Name == "JWT_ISSUER" {
			jwtIssuer = e.Value
			break
		}
	}
	require.NotEmpty(t, jwtIssuer)
	require.True(t, strings.HasPrefix(jwtIssuer, "https://"),
		"prod JWT_ISSUER must be HTTPS; got %q", jwtIssuer)
	require.False(t, strings.Contains(jwtIssuer, "cluster.local"),
		"prod JWT_ISSUER must NOT be cluster-local DNS (must be public); got %q", jwtIssuer)
}
