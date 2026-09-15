//go:build helm_render

// Package loopzerocloud_test — render-time tests for loopzero-cloud BOOTSTRAP chart.
//
// Plan 2026-05-20-helm-render-time-testing-terratest v1.1 — T3.1 (chart 1/10).
//
// CRITICAL: Per ADR D1 lookup order, `chartrender.ChartPath("loopzero-cloud")`
// resolves to `infra/helm/loopzero-cloud/` (bootstrap variant, used by Helmfile
// L2 at install time). The `charts/loopzero-cloud/` published variant (used by
// ArgoCD L3 Day-2) is structurally different (full Service template +
// loopzero-common library labels + container-level securityContext) and is
// NOT covered by this test file.
//
// FOLLOW-UP (Phase 3 amendment): published variant test pkg needed —
// see task #126 follow-up. CLAUDE.md "3 overlapping charts" rule means
// these two variants must stay in sync; render tests for BOTH would catch
// drift between the install-time chart and the GitOps chart.
//
// Bootstrap-variant shape (per `helm template` on infra/helm/loopzero-cloud):
//   - templates/deployment.yaml emits BOTH Service + Deployment (multi-doc)
//   - replicas: 1 (single replica is intentional)
//   - Pod-level securityContext: runAsNonRoot, runAsUser=1000, seccomp=RuntimeDefault
//   - NO container-level securityContext (relies on pod cascade)
//   - imagePullSecrets: loopzero-registry-pull (DOCR pull)
//   - Default image tag :latest (bug class B16; per-env stubs override)
package loopzerocloud_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/getloopscale/loopzero/infra/tests/helm/chartrender"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

const (
	chartName     = "loopzero-cloud"
	releaseName   = "loopzero-cloud-test"
	containerPort = 13002 // loopzero-cloud binary bind addr
)

func renderDefaultDeployment(t *testing.T) appsV1.Deployment {
	t.Helper()

	chartPath, err := chartrender.BootstrapChartPath(chartName)
	require.NoError(t, err)

	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)

	output := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/deployment.yaml"})
	require.NotEmpty(t, output)

	var deploy appsV1.Deployment
	chartrender.MustUnmarshalK8SYamlByKind(t, output, "Deployment", &deploy)
	return deploy
}

func renderDefaultService(t *testing.T) coreV1.Service {
	t.Helper()

	chartPath, err := chartrender.BootstrapChartPath(chartName)
	require.NoError(t, err)

	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)

	// Service is in the SAME multi-doc file (deployment.yaml), not service.yaml
	output := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/deployment.yaml"})
	require.NotEmpty(t, output)

	var svc coreV1.Service
	chartrender.MustUnmarshalK8SYamlByKind(t, output, "Service", &svc)
	return svc
}

// =============================================================================
// Category 1: Identity & Labels (bootstrap = minimal label set)
// =============================================================================

func TestLoopZeroCloud_Defaults_DeploymentName(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.Equal(t, "loopzero-cloud", deploy.Name)
}

func TestLoopZeroCloud_Defaults_BootstrapLabels(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	labels := deploy.Labels

	require.Equal(t, "loopzero-cloud", labels["app"])
	require.Equal(t, "loopzero-cloud", labels["loopzero.io/component"])
	require.Contains(t, []string{"Helm", "helm"},
		labels["app.kubernetes.io/managed-by"])
}

// TestLoopZeroCloud_Defaults_SelectorMatchesPodLabels — orphan ReplicaSet guard.
func TestLoopZeroCloud_Defaults_SelectorMatchesPodLabels(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	selector := deploy.Spec.Selector.MatchLabels
	podLabels := deploy.Spec.Template.Labels

	require.NotEmpty(t, selector)
	for k, v := range selector {
		require.Equalf(t, v, podLabels[k],
			"selector[%q] diverges from pod[%q]", k, k)
	}
}

// =============================================================================
// Category 3: Container
// =============================================================================

func TestLoopZeroCloud_Defaults_ImageRegistryFormat(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	image := deploy.Spec.Template.Spec.Containers[0].Image
	require.Regexp(t, `^[a-z0-9][a-z0-9.-]*(/[a-z0-9._-]+)*:[a-zA-Z0-9._-]+$`,
		image)
}

func TestLoopZeroCloud_Defaults_ContainerPort(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	container := deploy.Spec.Template.Spec.Containers[0]
	found := false
	for _, p := range container.Ports {
		if p.ContainerPort == containerPort {
			found = true
			break
		}
	}
	require.True(t, found,
		"container must expose port %d; got %+v", containerPort, container.Ports)
}

// TestLoopZeroCloud_Defaults_ServicePortMatchesContainerPort — Service routes to
// containerPort. Mismatch = "Connection refused" at readiness (B50 class).
func TestLoopZeroCloud_Defaults_ServicePortMatchesContainerPort(t *testing.T) {
	t.Parallel()
	svc := renderDefaultService(t)
	require.NotEmpty(t, svc.Spec.Ports)

	port := svc.Spec.Ports[0]
	require.Equal(t, int32(containerPort), port.Port,
		"service port must be %d", containerPort)
}

// =============================================================================
// Category 4: Security (pod-level only in bootstrap variant)
// =============================================================================

func TestLoopZeroCloud_Defaults_PodRunsAsNonRoot(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	podSec := deploy.Spec.Template.Spec.SecurityContext

	require.NotNil(t, podSec)
	require.NotNil(t, podSec.RunAsNonRoot)
	require.True(t, *podSec.RunAsNonRoot)
}

func TestLoopZeroCloud_Defaults_PodSeccompRuntimeDefault(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	podSec := deploy.Spec.Template.Spec.SecurityContext

	require.NotNil(t, podSec)
	require.NotNil(t, podSec.SeccompProfile,
		"pod seccompProfile must be set")
	require.Equal(t, coreV1.SeccompProfileTypeRuntimeDefault,
		podSec.SeccompProfile.Type)
}

func TestLoopZeroCloud_Defaults_HasImagePullSecret(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	pullSecrets := deploy.Spec.Template.Spec.ImagePullSecrets

	require.NotEmpty(t, pullSecrets,
		"loopzero-cloud needs DOCR imagePullSecrets")

	found := false
	for _, s := range pullSecrets {
		if s.Name == "loopzero-registry-pull" {
			found = true
			break
		}
	}
	require.True(t, found, "must reference loopzero-registry-pull")
}

// =============================================================================
// Category 6: Networking & Scaling
// =============================================================================

func TestLoopZeroCloud_Defaults_ReplicaCountExplicit(t *testing.T) {
	t.Parallel()
	deploy := renderDefaultDeployment(t)
	require.NotNil(t, deploy.Spec.Replicas, "replicas must be explicitly set")
	require.GreaterOrEqual(t, int(*deploy.Spec.Replicas), 1)
}
