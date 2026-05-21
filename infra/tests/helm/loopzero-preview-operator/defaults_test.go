//go:build helm_render

// Package loopzeropreviewoperator_test — render-time tests for the tenant
// operator chart (Plan 2026-05-20 v1.1 T3.1 chart 3/10).
//
// Pattern reused across all 4 split operators (tenant/app/platform/preview):
// they share an identical Deployment shape (controller binary, health
// :8081, metrics :8443, --leader-elect=true, fully-hardened container
// securityContext via loopzero-common library, no service-facing port).
package loopzeropreviewoperator_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/loopzero/loopzero/infra/tests/helm/helpers"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

const (
	chartName        = "loopzero-preview-operator"
	releaseName      = "pre-op"
	healthPort       = 8081
	metricsPort      = 8443
	controllerName   = "controller"
	leaderElectFlag  = "--leader-elect=true"
)

func renderDeployment(t *testing.T) appsV1.Deployment {
	t.Helper()
	chartPath, err := helpers.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := helpers.NewOptions(chartPath, "")
	require.NoError(t, err)
	out := helpers.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/deployment.yaml"})
	var d appsV1.Deployment
	helpers.MustUnmarshalK8SYamlByKind(t, out, "Deployment", &d)
	return d
}

func TestDefaults_DeploymentName(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	require.Contains(t, d.Name, chartName)
}

func TestDefaults_StandardHelmLabels(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	l := d.Labels
	require.Equal(t, chartName, l["app.kubernetes.io/name"])
	require.Equal(t, releaseName, l["app.kubernetes.io/instance"])
	require.Equal(t, "Helm", l["app.kubernetes.io/managed-by"])
}

func TestDefaults_SelectorMatchesPodLabels(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	for k, v := range d.Spec.Selector.MatchLabels {
		require.Equalf(t, v, d.Spec.Template.Labels[k],
			"selector[%q] vs pod[%q] mismatch — orphan ReplicaSet risk", k, k)
	}
}

func TestDefaults_ControllerContainerExists(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	require.Equal(t, controllerName, d.Spec.Template.Spec.Containers[0].Name)
}

func TestDefaults_LeaderElectionEnabled(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	require.Contains(t, d.Spec.Template.Spec.Containers[0].Args, leaderElectFlag,
		"operator MUST run with leader election (HA + crash recovery)")
}

func TestDefaults_PortsHealthAndMetrics(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	gotPorts := map[int32]string{}
	for _, p := range d.Spec.Template.Spec.Containers[0].Ports {
		gotPorts[p.ContainerPort] = p.Name
	}
	require.Equal(t, "health", gotPorts[healthPort],
		"container must expose health probe on :%d", healthPort)
	require.Equal(t, "metrics", gotPorts[metricsPort],
		"container must expose metrics on :%d", metricsPort)
}

func TestDefaults_ImageRegistryFormat(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	require.Regexp(t, `^[a-z0-9][a-z0-9.-]*(/[a-z0-9._-]+)*:[a-zA-Z0-9._-]+$`,
		d.Spec.Template.Spec.Containers[0].Image)
}

func TestDefaults_PodRunsAsNonRoot(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	ps := d.Spec.Template.Spec.SecurityContext
	require.NotNil(t, ps)
	require.NotNil(t, ps.RunAsNonRoot)
	require.True(t, *ps.RunAsNonRoot)
}

func TestDefaults_PodSeccompRuntimeDefault(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	ps := d.Spec.Template.Spec.SecurityContext
	require.NotNil(t, ps)
	require.NotNil(t, ps.SeccompProfile)
	require.Equal(t, coreV1.SeccompProfileTypeRuntimeDefault, ps.SeccompProfile.Type)
}

func TestDefaults_ContainerSecurityFullyHardened(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	for i, c := range d.Spec.Template.Spec.Containers {
		sc := c.SecurityContext
		require.NotNilf(t, sc, "container[%d] securityContext", i)
		require.NotNilf(t, sc.AllowPrivilegeEscalation, "container[%d] APE", i)
		require.Falsef(t, *sc.AllowPrivilegeEscalation, "container[%d] APE must be false", i)
		require.NotNilf(t, sc.ReadOnlyRootFilesystem, "container[%d] roRoot", i)
		require.Truef(t, *sc.ReadOnlyRootFilesystem, "container[%d] roRoot true", i)
		require.NotNilf(t, sc.Capabilities, "container[%d] caps", i)
		require.Containsf(t, sc.Capabilities.Drop, coreV1.Capability("ALL"),
			"container[%d] must drop ALL caps", i)
	}
}

func TestDefaults_HasLivenessProbe(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	require.NotNil(t, d.Spec.Template.Spec.Containers[0].LivenessProbe,
		"operator needs livenessProbe (kubelet restart trigger)")
}

func TestDefaults_HasReadinessProbe(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	require.NotNil(t, d.Spec.Template.Spec.Containers[0].ReadinessProbe,
		"operator needs readinessProbe (service gate)")
}

func TestDefaults_HasResourceLimits(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	c := d.Spec.Template.Spec.Containers[0]
	require.NotEmpty(t, c.Resources.Limits)
	require.NotEmpty(t, c.Resources.Requests)
}

func TestDefaults_ReplicasExplicit(t *testing.T) {
	t.Parallel()
	d := renderDeployment(t)
	require.NotNil(t, d.Spec.Replicas)
	require.GreaterOrEqual(t, int(*d.Spec.Replicas), 1)
}
