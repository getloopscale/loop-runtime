//go:build helm_render

// Package loopzeroapi_test — render-time tests for loopzero-api chart defaults.
//
// Plan 2026-05-20-helm-render-time-testing-terratest v1.1 — T2.1.
//
// Per ADR D7: 6 categories validated:
//   1. Identity & Labels
//   2. Configuration
//   3. Container
//   4. Security
//   5. Reliability
//   6. Networking & Scaling
//
// These tests render the chart with defaults (no env override) and
// assert the resulting K8s manifests structurally. Catches bug classes
// schemas (input validation) don't: label selector drift, conditional
// branches, security control removal, image registry typos.
package loopzeroapi_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/loopzero/loopzero/infra/tests/helm/helpers"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

const (
	chartName   = "loopzero-api"
	releaseName = "loopzero-api-test"
)

// renderDefaultDeployment renders loopzero-api deployment with chart defaults.
// Shared setup used by category tests.
//
// Note: loopzero-api/templates/deployment.yaml emits MULTIPLE docs (Service +
// Deployment). MustUnmarshalK8SYamlByKind filters to Kind=Deployment.
// (Caught during T2.1 implementation — bug fix for test infrastructure.)
func renderDefaultDeployment(t *testing.T) appsV1.Deployment {
	t.Helper()

	chartPath, err := helpers.ChartPath(chartName)
	require.NoError(t, err, "resolve chart path")

	opts, err := helpers.NewOptions(chartPath, "")
	require.NoError(t, err, "build helm options")

	output := helpers.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/deployment.yaml"})
	require.NotEmpty(t, output, "render produced empty output")

	var deploy appsV1.Deployment
	helpers.MustUnmarshalK8SYamlByKind(t, output, "Deployment", &deploy)

	return deploy
}

// (renderDefaultService removed 2026-05-21 as unused — lint cleanup.
// The Service kind asserts live in env-specific tests where they're
// actually exercised; default tests focus on Deployment shape.)

// =============================================================================
// Category 1: Identity & Labels
// =============================================================================

// TestLoopZeroAPI_Defaults_DeploymentName confirms deployment uses chart name.
func TestLoopZeroAPI_Defaults_DeploymentName(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	require.Equal(t, "loopzero-api", deploy.Name,
		"deployment metadata.name must match chart name")
}

// TestLoopZeroAPI_Defaults_StandardHelmLabels confirms the labels loopzero-api
// actually emits. Note: loopzero-api chart uses minimal label set (NOT full
// 5-standard helm convention); this test reflects current chart posture.
//
// Finding: loopzero-api chart could benefit from full app.kubernetes.io/*
// label set per Microsoft 2026 pattern. Separate chart-improvement task.
func TestLoopZeroAPI_Defaults_StandardHelmLabels(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	labels := deploy.Labels

	require.NotEmpty(t, labels, "deployment must have labels")
	require.Equal(t, "loopzero-api", labels["app"],
		"app label must match chart name")
	require.Equal(t, "loopzero-api", labels["loopzero.io/component"],
		"loopzero.io/component label must identify chart")
	require.Contains(t, []string{"Helm", "helm"}, labels["app.kubernetes.io/managed-by"],
		"managed-by must be Helm")
}

// TestLoopZeroAPI_Defaults_SelectorMatchesPodLabels — Microsoft 2026 critical
// pattern: label selector drift causes orphaned ReplicaSets in LIVE.
func TestLoopZeroAPI_Defaults_SelectorMatchesPodLabels(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	selector := deploy.Spec.Selector.MatchLabels
	podLabels := deploy.Spec.Template.Labels

	require.NotEmpty(t, selector, "selector.matchLabels must not be empty")
	require.NotEmpty(t, podLabels, "pod template labels must not be empty")

	// Every selector key MUST exist + match in pod labels
	for k, selectorVal := range selector {
		podVal, ok := podLabels[k]
		require.Truef(t, ok,
			"selector key %q absent from pod labels — would cause orphaned ReplicaSet", k)
		require.Equalf(t, selectorVal, podVal,
			"selector[%q]=%q diverges from pod[%q]=%q",
			k, selectorVal, k, podVal)
	}
}

// =============================================================================
// Category 3: Container
// =============================================================================

// TestLoopZeroAPI_Defaults_ContainersNonEmpty — EC-4 guard. Containers slice
// must be non-empty before indexing. If template has `{{ if .X }}` around
// container def, this catches it.
func TestLoopZeroAPI_Defaults_ContainersNonEmpty(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers,
		"deployment must have at least one container")
}

// TestLoopZeroAPI_Defaults_ImageRegistryFormat — EC-6 pattern: assert
// STRUCTURE (OCI image ref format), not exact value.
func TestLoopZeroAPI_Defaults_ImageRegistryFormat(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	image := deploy.Spec.Template.Spec.Containers[0].Image
	require.NotEmpty(t, image, "container image must not be empty")
	require.Regexp(t, `^[a-z0-9][a-z0-9.-]*(/[a-z0-9._-]+)*:[a-zA-Z0-9._-]+$`,
		image,
		"image must be OCI ref format: registry/repo:tag")
}

// TestLoopZeroAPI_Defaults_ContainerPortMatchesAPIBind — B50 protection at
// render-time: loopzero-api binary binds :3001. Service + container port
// must match (else readiness probe connection refused).
func TestLoopZeroAPI_Defaults_ContainerPortMatchesAPIBind(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	container := deploy.Spec.Template.Spec.Containers[0]
	foundAPIPort := false
	for _, p := range container.Ports {
		if p.ContainerPort == 3001 {
			foundAPIPort = true
			break
		}
	}
	require.True(t, foundAPIPort,
		"container must expose port 3001 (loopzero-api binary bind addr per B50 fix); "+
			"got ports: %+v", container.Ports)
}

// =============================================================================
// Category 4: Security (INF-G08 render-time enforcement)
// =============================================================================

// TestLoopZeroAPI_Defaults_PodRunsAsNonRoot — INF-G08 invariant at template
// level. Schema (T2.5 ext) requires field present in values; this test
// confirms template actually USES the value in the rendered manifest.
func TestLoopZeroAPI_Defaults_PodRunsAsNonRoot(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	podSec := deploy.Spec.Template.Spec.SecurityContext

	require.NotNil(t, podSec, "podSecurityContext must be set")
	require.NotNil(t, podSec.RunAsNonRoot, "runAsNonRoot must be specified")
	require.True(t, *podSec.RunAsNonRoot,
		"pod must run as non-root per INF-G08")
}

// TestLoopZeroAPI_Defaults_ContainerSecurityHardened — verify each container
// has hardened securityContext fields that are CONTAINER-level (not
// inherited from pod). runAsNonRoot is asserted at pod level via separate
// test (TestLoopZeroAPI_Defaults_PodRunsAsNonRoot) since pod-level cascades.
func TestLoopZeroAPI_Defaults_ContainerSecurityHardened(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	for i, c := range deploy.Spec.Template.Spec.Containers {
		require.NotNilf(t, c.SecurityContext,
			"container[%d] (%s) must have securityContext", i, c.Name)

		sc := c.SecurityContext

		require.NotNilf(t, sc.AllowPrivilegeEscalation,
			"container[%d] allowPrivilegeEscalation must be specified", i)
		require.Falsef(t, *sc.AllowPrivilegeEscalation,
			"container[%d] must NOT allow privilege escalation", i)

		require.NotNilf(t, sc.ReadOnlyRootFilesystem,
			"container[%d] readOnlyRootFilesystem must be specified", i)
		require.Truef(t, *sc.ReadOnlyRootFilesystem,
			"container[%d] must have read-only root filesystem", i)

		// Capabilities drop must include "ALL" (INF-G08)
		require.NotNilf(t, sc.Capabilities,
			"container[%d] capabilities must be specified", i)
		require.Containsf(t, sc.Capabilities.Drop, coreV1.Capability("ALL"),
			"container[%d] must drop ALL capabilities", i)
	}
}

// =============================================================================
// Category 5: Reliability
// =============================================================================

// TestLoopZeroAPI_Defaults_HasLivenessProbe — operational readiness.
func TestLoopZeroAPI_Defaults_HasLivenessProbe(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	mainContainer := deploy.Spec.Template.Spec.Containers[0]
	require.NotNil(t, mainContainer.LivenessProbe,
		"main container must have livenessProbe (kubelet restart trigger)")
}

// TestLoopZeroAPI_Defaults_HasReadinessProbe — service ingress gate.
func TestLoopZeroAPI_Defaults_HasReadinessProbe(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	mainContainer := deploy.Spec.Template.Spec.Containers[0]
	require.NotNil(t, mainContainer.ReadinessProbe,
		"main container must have readinessProbe (service routing gate)")
}

// TestLoopZeroAPI_Defaults_HasResourceLimits — VAP requireResourceLimits
// enforce em K8s; schema requires field; this test confirms render
// emits limits in actual manifest (not silently dropped).
func TestLoopZeroAPI_Defaults_HasResourceLimits(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)

	mainContainer := deploy.Spec.Template.Spec.Containers[0]
	require.NotEmpty(t, mainContainer.Resources.Limits,
		"main container must have resources.limits (VAP requireResourceLimits)")
	require.NotEmpty(t, mainContainer.Resources.Requests,
		"main container must have resources.requests")
}

// =============================================================================
// Category 6: Networking & Scaling
// =============================================================================

// TestLoopZeroAPI_Defaults_HARequiresMultipleReplicas — chart values.yaml
// comment claims HA default. Render must reflect.
func TestLoopZeroAPI_Defaults_HARequiresMultipleReplicas(t *testing.T) {
	t.Parallel()

	deploy := renderDefaultDeployment(t)
	require.NotNil(t, deploy.Spec.Replicas, "replicas must be set")
	require.GreaterOrEqual(t, int(*deploy.Spec.Replicas), 2,
		"loopzero-api defaults to multi-replica HA per CTO mandate 2026-05-14 + chaos-test C01")
}
