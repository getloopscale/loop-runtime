//go:build helm_render

package loopzeroapi_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/loopzero/loopzero/infra/tests/helm/helpers"
	appsV1 "k8s.io/api/apps/v1"
)

// devEnvStub is the relative path (from repo root) to the dev plain-YAML
// env stub created in T1.3.
const devEnvStub = "infra/tests/helm/values/dev/loopzero-api.yaml"

func renderDevDeployment(t *testing.T) appsV1.Deployment {
	t.Helper()

	chartPath, err := helpers.ChartPath(chartName)
	require.NoError(t, err)

	opts, err := helpers.NewOptions(chartPath, devEnvStub)
	require.NoError(t, err)

	output := helpers.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/deployment.yaml"})
	var deploy appsV1.Deployment
	helpers.MustUnmarshalK8SYamlByKind(t, output, "Deployment", &deploy)
	return deploy
}

// TestLoopZeroAPI_Dev_LocalRegistry — dev k3d uses local registry, NOT DOCR.
// Catches env drift if someone copies prod registry into dev stub.
func TestLoopZeroAPI_Dev_LocalRegistry(t *testing.T) {
	t.Parallel()

	deploy := renderDevDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	image := deploy.Spec.Template.Spec.Containers[0].Image
	require.True(t, strings.HasPrefix(image, "k3d-loopzero-registry"),
		"dev image must use local k3d registry; got %q", image)
}

// TestLoopZeroAPI_Dev_SingleReplica — dev k3d does not need HA.
func TestLoopZeroAPI_Dev_SingleReplica(t *testing.T) {
	t.Parallel()

	deploy := renderDevDeployment(t)
	require.NotNil(t, deploy.Spec.Replicas)
	require.Equal(t, int32(1), *deploy.Spec.Replicas,
		"dev should use single replica (no HA on local k3d)")
}

// TestLoopZeroAPI_Dev_LocalHydra — dev JWT issuer uses cluster-local Hydra.
func TestLoopZeroAPI_Dev_LocalHydra(t *testing.T) {
	t.Parallel()

	deploy := renderDevDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	envVars := deploy.Spec.Template.Spec.Containers[0].Env
	var jwtIssuer string
	for _, e := range envVars {
		if e.Name == "JWT_ISSUER" {
			jwtIssuer = e.Value
			break
		}
	}
	require.NotEmpty(t, jwtIssuer, "JWT_ISSUER env var must be set in dev")
	require.True(t, strings.Contains(jwtIssuer, "cluster.local"),
		"dev JWT_ISSUER must use cluster-local DNS; got %q", jwtIssuer)
}
