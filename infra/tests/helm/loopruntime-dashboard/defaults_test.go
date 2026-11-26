//go:build helm_render

// Package loopruntimedashboard_test — render-time tests for loopruntime-dashboard chart.
//
// Plan 2026-05-20-helm-render-time-testing-terratest v1.1 — T3.1 (chart 2/10).
//
// loopruntime-dashboard is the web UI served from charts/loopruntime-dashboard. Single
// deployment, listens on :8080.
package loopruntimedashboard_test

import (
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

const (
	chartName     = "loopruntime-dashboard"
	releaseName   = "loopruntime-dashboard-test"
	containerPort = 8080
)

func renderDefaultDeployment(t *testing.T) appsV1.Deployment {
	t.Helper()
	chartPath, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	output := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/deployment.yaml"})
	var deploy appsV1.Deployment
	chartrender.MustUnmarshalK8SYamlByKind(t, output, "Deployment", &deploy)
	return deploy
}

func TestLoopRuntimeDashboard_Defaults_DeploymentName(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.Contains(t, deploy.Name, "loopruntime-dashboard")
}

func TestLoopRuntimeDashboard_Defaults_StandardHelmLabels(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	labels := deploy.Labels
	require.Equal(t, "loopruntime-dashboard", labels["app.kubernetes.io/name"])
	require.Equal(t, releaseName, labels["app.kubernetes.io/instance"])
	require.Equal(t, "Helm", labels["app.kubernetes.io/managed-by"])
}

func TestLoopRuntimeDashboard_Defaults_SelectorMatchesPodLabels(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	selector := deploy.Spec.Selector.MatchLabels
	podLabels := deploy.Spec.Template.Labels
	require.NotEmpty(t, selector)
	for k, v := range selector {
		require.Equalf(t, v, podLabels[k], "selector[%q] vs pod[%q]", k, k)
	}
}

func TestLoopRuntimeDashboard_Defaults_ImageRegistryFormat(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)
	require.Regexp(t, `^[a-z0-9][a-z0-9.-]*(/[a-z0-9._-]+)*:[a-zA-Z0-9._-]+$`,
		deploy.Spec.Template.Spec.Containers[0].Image)
}

func TestLoopRuntimeDashboard_Defaults_ContainerPort(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)
	found := false
	for _, p := range deploy.Spec.Template.Spec.Containers[0].Ports {
		if p.ContainerPort == containerPort {
			found = true
			break
		}
	}
	require.Truef(t, found, "container must expose port %d", containerPort)
}

func TestLoopRuntimeDashboard_Defaults_PodRunsAsNonRoot(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	podSec := deploy.Spec.Template.Spec.SecurityContext
	require.NotNil(t, podSec)
	require.NotNil(t, podSec.RunAsNonRoot)
	require.True(t, *podSec.RunAsNonRoot)
}

func TestLoopRuntimeDashboard_Defaults_PodSeccompRuntimeDefault(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	podSec := deploy.Spec.Template.Spec.SecurityContext
	require.NotNil(t, podSec)
	require.NotNil(t, podSec.SeccompProfile)
	require.Equal(t, coreV1.SeccompProfileTypeRuntimeDefault,
		podSec.SeccompProfile.Type)
}

// TestLoopRuntimeDashboard_Defaults_ContainerSecurityHardened — adapted for
// nginx-backed images: readOnlyRootFilesystem is intentionally NOT
// asserted because nginx needs writable /tmp + /var/cache + /var/run
// (chart documents this with `# readOnlyRootFilesystem: false — nginx
// needs to write to /tmp and /var/cache`). All other hardening
// invariants still apply.
func TestLoopRuntimeDashboard_Defaults_ContainerSecurityHardened(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)
	for i, c := range deploy.Spec.Template.Spec.Containers {
		sc := c.SecurityContext
		require.NotNilf(t, sc, "container[%d] securityContext", i)
		require.NotNilf(t, sc.AllowPrivilegeEscalation, "container[%d] allowPrivilegeEscalation", i)
		require.Falsef(t, *sc.AllowPrivilegeEscalation,
			"container[%d] must NOT allow privilege escalation", i)
		require.NotNilf(t, sc.Capabilities, "container[%d] capabilities", i)
		require.Containsf(t, sc.Capabilities.Drop, coreV1.Capability("ALL"),
			"container[%d] must drop ALL capabilities", i)
		require.NotNilf(t, sc.SeccompProfile, "container[%d] seccompProfile", i)
		require.Equalf(t, coreV1.SeccompProfileTypeRuntimeDefault,
			sc.SeccompProfile.Type,
			"container[%d] seccomp must be RuntimeDefault", i)
	}
}

func TestLoopRuntimeDashboard_Defaults_AutomountTokenDisabled(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	amt := deploy.Spec.Template.Spec.AutomountServiceAccountToken
	require.NotNil(t, amt, "automountServiceAccountToken must be explicit")
	require.False(t, *amt, "dashboard needs no SA token")
}

func TestLoopRuntimeDashboard_Defaults_HasLivenessProbe(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)
	require.NotNil(t, deploy.Spec.Template.Spec.Containers[0].LivenessProbe)
}

func TestLoopRuntimeDashboard_Defaults_HasReadinessProbe(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)
	require.NotNil(t, deploy.Spec.Template.Spec.Containers[0].ReadinessProbe)
}

func TestLoopRuntimeDashboard_Defaults_HasResourceLimits(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)
	c := deploy.Spec.Template.Spec.Containers[0]
	require.NotEmpty(t, c.Resources.Limits)
	require.NotEmpty(t, c.Resources.Requests)
}

func TestLoopRuntimeDashboard_Defaults_ReplicasExplicit(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.NotNil(t, deploy.Spec.Replicas)
	require.GreaterOrEqual(t, int(*deploy.Spec.Replicas), 1)
}
