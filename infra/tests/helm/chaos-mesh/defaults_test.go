//go:build helm_render

// Package chaosmesh_test — Plan 2026-05-20 v1.1 T3.2 (6/8).
//
// chaos-mesh is the wrapper chart that pulls in upstream chaos-mesh
// (controller-manager + chaos-daemon + chaos-dashboard) for use in the
// drill cluster. CLAUDE.md flags an open issue: chaos-mesh in local k3d
// enters CrashLoopBackOff with informer cache sync timeout — render
// tests cannot catch this (runtime issue) but CAN catch the structural
// drift class: webhook config existence, RBAC completeness.
package chaosmesh_test

import (
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
)

const (
	chartName   = "chaos-mesh"
	releaseName = "cm"
)

func renderAll(t *testing.T) string {
	t.Helper()
	cp, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(cp, "")
	require.NoError(t, err)
	return chartrender.RenderTemplate(t, opts, cp, releaseName, nil)
}

func TestChaosMesh_HasControllerManagerDeployment(t *testing.T) {
	t.Parallel()
	deploys := chartrender.SplitYAMLByKind(renderAll(t), "Deployment")
	require.NotEmpty(t, deploys,
		"chaos-mesh must emit at least one Deployment (controller-manager)")
}

func TestChaosMesh_HasChaosDaemonDaemonSet(t *testing.T) {
	t.Parallel()
	ds := chartrender.SplitYAMLByKind(renderAll(t), "DaemonSet")
	require.NotEmpty(t, ds,
		"chaos-mesh must emit a DaemonSet (chaos-daemon runs per-node)")
}

func TestChaosMesh_HasMutatingWebhookConfig(t *testing.T) {
	t.Parallel()
	// MutatingWebhookConfiguration is how chaos-mesh injects into pods.
	// Missing = chaos experiments silently no-op.
	mw := chartrender.SplitYAMLByKind(renderAll(t),
		"MutatingWebhookConfiguration")
	require.NotEmpty(t, mw,
		"chaos-mesh MutatingWebhookConfiguration missing — chaos "+
			"experiments would silently no-op without it")
}

func TestChaosMesh_HasValidatingWebhookConfig(t *testing.T) {
	t.Parallel()
	vw := chartrender.SplitYAMLByKind(renderAll(t),
		"ValidatingWebhookConfiguration")
	require.NotEmpty(t, vw,
		"chaos-mesh ValidatingWebhookConfiguration missing — CR "+
			"validation skipped, garbage chaos manifests would apply")
}

func TestChaosMesh_HasRBACFullyDefined(t *testing.T) {
	t.Parallel()
	out := renderAll(t)
	for _, kind := range []string{"ServiceAccount", "ClusterRole",
		"ClusterRoleBinding"} {
		docs := chartrender.SplitYAMLByKind(out, kind)
		require.NotEmptyf(t, docs,
			"chaos-mesh must declare %s (operator pattern requirement)",
			kind)
	}
}

func TestChaosMesh_HasServicesForControllerAndWebhook(t *testing.T) {
	t.Parallel()
	svcs := chartrender.SplitYAMLByKind(renderAll(t), "Service")
	require.GreaterOrEqualf(t, len(svcs), 2,
		"chaos-mesh needs >=2 Services (controller-metrics + webhook "+
			"endpoint); got %d", len(svcs))
}
