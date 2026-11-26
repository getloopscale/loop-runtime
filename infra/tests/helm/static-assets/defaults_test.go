//go:build helm_render

// Package staticassets_test — Plan 2026-05-20 v1.1 T3.2 (8/8).
//
// static-assets is the helper chart that wraps MinIO for static-app
// hosting (CHG-2026-10 Static Frontend Delivery sprint).
package staticassets_test

import (
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

const (
	chartName   = "static-assets"
	releaseName = "sa"
)

func TestStaticAssets_HasDeployment(t *testing.T) {
	t.Parallel()
	cp, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(cp, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, cp, releaseName, nil)

	docs := chartrender.SplitYAMLByKind(out, "Deployment")
	require.NotEmpty(t, docs, "must emit at least one Deployment")
}

func TestStaticAssets_HasService(t *testing.T) {
	t.Parallel()
	cp, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(cp, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, cp, releaseName, nil)

	docs := chartrender.SplitYAMLByKind(out, "Service")
	require.NotEmpty(t, docs, "must emit Service for the Deployment")
}

func TestStaticAssets_ServicePortMatchesContainerPort(t *testing.T) {
	t.Parallel()
	cp, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(cp, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, cp, releaseName, nil)

	var deploy appsV1.Deployment
	chartrender.MustUnmarshalK8SYamlByKind(t, out, "Deployment", &deploy)
	var svc coreV1.Service
	chartrender.MustUnmarshalK8SYamlByKind(t, out, "Service", &svc)

	require.NotEmpty(t, deploy.Spec.Template.Spec.Containers)
	require.NotEmpty(t, svc.Spec.Ports)

	// Service targetPort (int or name) must resolve to a port the
	// container actually exposes. We accept name-match OR port-number match.
	containerPorts := make(map[int32]string)
	for _, p := range deploy.Spec.Template.Spec.Containers[0].Ports {
		containerPorts[p.ContainerPort] = p.Name
	}

	mapped := false
	for _, sp := range svc.Spec.Ports {
		switch sp.TargetPort.Type {
		case 0: // int
			if _, ok := containerPorts[sp.TargetPort.IntVal]; ok {
				mapped = true
			}
		case 1: // string (port name)
			for _, name := range containerPorts {
				if name == sp.TargetPort.StrVal {
					mapped = true
				}
			}
		}
	}
	require.Truef(t, mapped,
		"Service.targetPort does not map to any container port — "+
			"traffic would be black-holed; container ports=%v, "+
			"service ports=%+v", containerPorts, svc.Spec.Ports)
}
