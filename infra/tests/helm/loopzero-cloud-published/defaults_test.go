//go:build helm_render

// Package loopzerocloudpublished_test — render-time tests for the PUBLISHED
// variant of loopzero-cloud (Plan 2026-05-20 v1.1, Task #128 follow-up).
//
// There is one loopzero-cloud chart: ArgoCD consumes it with these defaults, and the
// Helmfile bootstrap installs it with charts/loopzero-cloud/values-bootstrap.yaml
// (bootstrap_install_test.go). The bootstrap copy under infra/helm/loopzero-cloud and
// its render suite were deleted when the pair became one chart.
//
// Published shape (charts/loopzero-cloud, uses loopzero-common library):
//   - 5 standard app.kubernetes.io/* labels (vs minimal in bootstrap)
//   - templates/{service,configmap,networkpolicy,pdb,serviceaccount,
//     servicemonitor,deployment}.yaml — separate template files
//   - Container-level securityContext fully hardened (NOT cascade-only)
//   - priorityClassName: system-cluster-critical
//   - automountServiceAccountToken explicitly false
//   - Replicas default 1 (loopzero-cloud is dev-tier app, NOT HA-required)
package loopzerocloudpublished_test

import (
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

const (
	chartName   = "loopzero-cloud"
	releaseName = "tcl-pub"
)

func renderDeploy(t *testing.T) appsV1.Deployment {
	t.Helper()
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/deployment.yaml"})
	var d appsV1.Deployment
	chartrender.MustUnmarshalK8SYamlByKind(t, out, "Deployment", &d)
	return d
}

func TestCloudPublished_FullStandardLabels(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	for _, key := range []string{
		"app.kubernetes.io/name",
		"app.kubernetes.io/instance",
		"app.kubernetes.io/managed-by",
		"app.kubernetes.io/version",
		"app.kubernetes.io/part-of",
	} {
		require.NotEmptyf(t, d.Labels[key],
			"published variant must emit %q label (loopzero-common library)",
			key)
	}
}

func TestCloudPublished_PriorityClassSetSystemClusterCritical(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	require.Equal(t, "system-cluster-critical",
		d.Spec.Template.Spec.PriorityClassName,
		"published loopzero-cloud must declare priorityClassName="+
			"system-cluster-critical (auth+billing = control-plane critical)")
}

func TestCloudPublished_AutomountTokenDisabled(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	amt := d.Spec.Template.Spec.AutomountServiceAccountToken
	require.NotNilf(t, amt, "automountServiceAccountToken must be explicit "+
		"(implicit default = true = unnecessary token mount)")
	require.False(t, *amt,
		"loopzero-cloud does not need to call the K8s API — token mount "+
			"is unnecessary attack surface")
}

func TestCloudPublished_ContainerFullyHardened(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	for i, c := range d.Spec.Template.Spec.Containers {
		sc := c.SecurityContext
		require.NotNilf(t, sc, "container[%d] securityContext", i)
		require.NotNilf(t, sc.AllowPrivilegeEscalation, "container[%d] APE", i)
		require.Falsef(t, *sc.AllowPrivilegeEscalation, "container[%d]", i)
		require.NotNilf(t, sc.ReadOnlyRootFilesystem, "container[%d] roRoot", i)
		require.Truef(t, *sc.ReadOnlyRootFilesystem, "container[%d] roRoot true", i)
		require.NotNilf(t, sc.Capabilities, "container[%d] caps", i)
		require.Containsf(t, sc.Capabilities.Drop, coreV1.Capability("ALL"),
			"container[%d] must drop ALL caps", i)
		require.NotNilf(t, sc.SeccompProfile, "container[%d] seccomp", i)
		require.Equalf(t, coreV1.SeccompProfileTypeRuntimeDefault,
			sc.SeccompProfile.Type, "container[%d] seccomp must be "+
				"RuntimeDefault", i)
	}
}

func TestCloudPublished_PodRunsAsNonRoot(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	ps := d.Spec.Template.Spec.SecurityContext
	require.NotNil(t, ps)
	require.NotNil(t, ps.RunAsNonRoot)
	require.True(t, *ps.RunAsNonRoot)
}

func TestCloudPublished_HasLivenessAndReadinessProbes(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	c := d.Spec.Template.Spec.Containers[0]
	require.NotNil(t, c.LivenessProbe, "must declare livenessProbe")
	require.NotNil(t, c.ReadinessProbe, "must declare readinessProbe")
}

func TestCloudPublished_HasResourceLimits(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	c := d.Spec.Template.Spec.Containers[0]
	require.NotEmpty(t, c.Resources.Limits, "must declare resources.limits")
	require.NotEmpty(t, c.Resources.Requests, "must declare resources.requests")
}

func TestCloudPublished_SeparateServiceTemplate(t *testing.T) {
	t.Parallel()
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/service.yaml"})
	var svc coreV1.Service
	chartrender.MustUnmarshalK8SYamlByKind(t, out, "Service", &svc)
	require.NotEmpty(t, svc.Spec.Ports,
		"published variant must declare Service with at least one port")
}

func TestCloudPublished_HasNetworkPolicy(t *testing.T) {
	t.Parallel()
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/networkpolicy.yaml"})
	docs := chartrender.SplitYAMLByKind(out, "NetworkPolicy")
	require.NotEmpty(t, docs,
		"published loopzero-cloud must declare NetworkPolicy")
}

func TestCloudPublished_ServiceMonitorConditionalToggle(t *testing.T) {
	t.Parallel()
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)

	// Default OFF — full-chart render must produce zero ServiceMonitor.
	// (Cannot use --show-only on a fully-conditional template — helm
	// errors out when the named template produces no output.)
	optsOff, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	outOff := chartrender.RenderTemplate(t, optsOff, chartPath, releaseName, nil)
	require.Empty(t, chartrender.SplitYAMLByKind(outOff, "ServiceMonitor"),
		"ServiceMonitor must be OFF by default (opt-in toggle)")

	// Toggled ON — full-chart render must produce exactly one ServiceMonitor.
	optsOn, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	optsOn.SetValues = map[string]string{
		"metrics.serviceMonitor.enabled": "true",
	}
	outOn := chartrender.RenderTemplate(t, optsOn, chartPath, releaseName, nil)
	docs := chartrender.SplitYAMLByKind(outOn, "ServiceMonitor")
	require.NotEmpty(t, docs,
		"with metrics.serviceMonitor.enabled=true, chart MUST emit "+
			"ServiceMonitor — silent no-op = broken Prometheus discovery")
	require.Lenf(t, docs, 1, "expected exactly 1 ServiceMonitor; got %d",
		len(docs))
}

func TestCloudPublished_DedicatedServiceAccount(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	require.NotEqualf(t, "default", d.Spec.Template.Spec.ServiceAccountName,
		"must use dedicated ServiceAccount")
	require.NotEmpty(t, d.Spec.Template.Spec.ServiceAccountName)
}
