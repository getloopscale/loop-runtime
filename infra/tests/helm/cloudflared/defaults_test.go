//go:build helm_render

// Package cloudflared_test — Plan 2026-05-20 v1.1 T3.2 (3/8).
//
// cloudflared is the edge tunnel that fronts production. Critical
// invariants: HA (>= 2 replicas), runs as non-root, image is pinned to
// digest or semver (not :latest — registry rejection risk).
package cloudflared_test

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	appsV1 "k8s.io/api/apps/v1"
)

const (
	chartName   = "cloudflared"
	releaseName = "cf"
)

func renderDeploy(t *testing.T) appsV1.Deployment {
	t.Helper()
	cp, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(cp, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, cp, releaseName,
		[]string{"templates/deployment.yaml"})
	var d appsV1.Deployment
	chartrender.MustUnmarshalK8SYamlByKind(t, out, "Deployment", &d)
	return d
}

func TestCloudflared_IsHighlyAvailable(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	require.NotNil(t, d.Spec.Replicas, "replicas must be set explicitly")
	require.GreaterOrEqual(t, int(*d.Spec.Replicas), 2,
		"cloudflared MUST be HA (>= 2) — single replica = edge outage on "+
			"pod restart; prod hard-down")
}

func TestCloudflared_RunsAsNonRoot(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	require.NotNil(t, d.Spec.Template.Spec.SecurityContext)
	require.NotNil(t, d.Spec.Template.Spec.SecurityContext.RunAsNonRoot)
	require.True(t, *d.Spec.Template.Spec.SecurityContext.RunAsNonRoot,
		"cloudflared must run as non-root")
}

func TestCloudflared_ImageTagPinned(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	img := d.Spec.Template.Spec.Containers[0].Image
	require.NotEmpty(t, img)
	require.Falsef(t, strings.HasSuffix(img, ":latest"),
		"cloudflared image must NOT use :latest tag — "+
			"unpredictable rollouts + DOCR rejection class (Bug #16); "+
			"got %q", img)
	require.Contains(t, img, ":",
		"cloudflared image must specify a tag, not bare repo")
}

func TestCloudflared_HasResourceLimits(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	c := d.Spec.Template.Spec.Containers[0]
	require.NotEmpty(t, c.Resources.Limits,
		"cloudflared must have resource limits (VAP enforces in prod)")
}

func TestCloudflared_AppLabelMatches(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	require.Equal(t, "cloudflared",
		d.Spec.Selector.MatchLabels["app"],
		"selector must target app=cloudflared")
	require.Equal(t, "cloudflared",
		d.Spec.Template.Labels["app"],
		"pod template label must match selector")
}
