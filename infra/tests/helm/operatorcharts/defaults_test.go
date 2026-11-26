//go:build helm_render

// Package operatorcharts holds the render-time defaults suite for the four
// split operator charts (tenant / app / platform / preview).
//
// They share one Deployment shape — controller binary, health :8081, metrics
// :8443, --leader-elect=true, a fully hardened container securityContext from
// the loop-runtime-common library, no service-facing port — so they share one suite,
// table-driven over {chart, release}. It replaces four per-chart copies that
// differed only in those two constants; a fifth operator is one table row, and
// a new invariant reaches all four at once instead of drifting across copies.
//
// Run one chart: go test -tags helm_render ./helm/operatorcharts/ -run 'TestOperatorChartDefaults/loop-runtime-app-operator'
package operatorcharts_test

import (
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

// operatorChart is one row of the table: the chart and the release it renders as.
type operatorChart struct {
	chart   string
	release string
}

var operatorCharts = []operatorChart{
	{chart: "loop-runtime-tenant-operator", release: "tnt-op"},
	{chart: "loop-runtime-app-operator", release: "app-op"},
	{chart: "loop-runtime-platform-operator", release: "pla-op"},
	{chart: "loop-runtime-preview-operator", release: "pre-op"},
}

const (
	healthPort      = 8081
	metricsPort     = 8443
	controllerName  = "controller"
	leaderElectFlag = "--leader-elect=true"
)

// defaultsCheck asserts one invariant of a rendered operator Deployment.
type defaultsCheck func(t *testing.T, oc operatorChart, d appsV1.Deployment)

func TestOperatorChartDefaults(t *testing.T) {
	checks := []struct {
		name  string
		check defaultsCheck
	}{
		{"DeploymentName", checkDeploymentName},
		{"StandardHelmLabels", checkStandardHelmLabels},
		{"SelectorMatchesPodLabels", checkSelectorMatchesPodLabels},
		{"ControllerContainerExists", checkControllerContainerExists},
		{"LeaderElectionEnabled", checkLeaderElectionEnabled},
		{"PortsHealthAndMetrics", checkPortsHealthAndMetrics},
		{"ImageRegistryFormat", checkImageRegistryFormat},
		{"PodRunsAsNonRoot", checkPodRunsAsNonRoot},
		{"PodSeccompRuntimeDefault", checkPodSeccompRuntimeDefault},
		{"ContainerSecurityFullyHardened", checkContainerSecurityFullyHardened},
		{"HasLivenessProbe", checkHasLivenessProbe},
		{"HasReadinessProbe", checkHasReadinessProbe},
		{"HasResourceLimits", checkHasResourceLimits},
		{"ReplicasExplicit", checkReplicasExplicit},
	}
	for _, c := range operatorCharts {
		c := c
		t.Run(c.chart, func(t *testing.T) {
			t.Parallel()
			d := renderDeployment(t, c)
			for _, ck := range checks {
				ck := ck
				t.Run(ck.name, func(t *testing.T) { ck.check(t, c, d) })
			}
		})
	}
}

func renderDeployment(t *testing.T, c operatorChart) appsV1.Deployment {
	t.Helper()
	chartPath, err := chartrender.ChartPath(c.chart)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, chartPath, c.release,
		[]string{"templates/deployment.yaml"})
	var d appsV1.Deployment
	chartrender.MustUnmarshalK8SYamlByKind(t, out, "Deployment", &d)
	return d
}

func checkDeploymentName(t *testing.T, oc operatorChart, d appsV1.Deployment) {
	require.Contains(t, d.Name, oc.chart)
}

func checkStandardHelmLabels(t *testing.T, oc operatorChart, d appsV1.Deployment) {
	l := d.Labels
	require.Equal(t, oc.chart, l["app.kubernetes.io/name"])
	require.Equal(t, oc.release, l["app.kubernetes.io/instance"])
	require.Equal(t, "Helm", l["app.kubernetes.io/managed-by"])
}

func checkSelectorMatchesPodLabels(t *testing.T, _ operatorChart, d appsV1.Deployment) {
	for k, v := range d.Spec.Selector.MatchLabels {
		require.Equalf(t, v, d.Spec.Template.Labels[k],
			"selector[%q] vs pod[%q] mismatch — orphan ReplicaSet risk", k, k)
	}
}

func checkControllerContainerExists(t *testing.T, _ operatorChart, d appsV1.Deployment) {
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	require.Equal(t, controllerName, d.Spec.Template.Spec.Containers[0].Name)
}

func checkLeaderElectionEnabled(t *testing.T, _ operatorChart, d appsV1.Deployment) {
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	require.Contains(t, d.Spec.Template.Spec.Containers[0].Args, leaderElectFlag,
		"operator MUST run with leader election (HA + crash recovery)")
}

func checkPortsHealthAndMetrics(t *testing.T, _ operatorChart, d appsV1.Deployment) {
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

func checkImageRegistryFormat(t *testing.T, _ operatorChart, d appsV1.Deployment) {
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	require.Regexp(t, `^[a-z0-9][a-z0-9.-]*(/[a-z0-9._-]+)*:[a-zA-Z0-9._-]+$`,
		d.Spec.Template.Spec.Containers[0].Image)
}

func checkPodRunsAsNonRoot(t *testing.T, _ operatorChart, d appsV1.Deployment) {
	ps := d.Spec.Template.Spec.SecurityContext
	require.NotNil(t, ps)
	require.NotNil(t, ps.RunAsNonRoot)
	require.True(t, *ps.RunAsNonRoot)
}

func checkPodSeccompRuntimeDefault(t *testing.T, _ operatorChart, d appsV1.Deployment) {
	ps := d.Spec.Template.Spec.SecurityContext
	require.NotNil(t, ps)
	require.NotNil(t, ps.SeccompProfile)
	require.Equal(t, coreV1.SeccompProfileTypeRuntimeDefault, ps.SeccompProfile.Type)
}

func checkContainerSecurityFullyHardened(t *testing.T, _ operatorChart, d appsV1.Deployment) {
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

func checkHasLivenessProbe(t *testing.T, _ operatorChart, d appsV1.Deployment) {
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	require.NotNil(t, d.Spec.Template.Spec.Containers[0].LivenessProbe,
		"operator needs livenessProbe (kubelet restart trigger)")
}

func checkHasReadinessProbe(t *testing.T, _ operatorChart, d appsV1.Deployment) {
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	require.NotNil(t, d.Spec.Template.Spec.Containers[0].ReadinessProbe,
		"operator needs readinessProbe (service gate)")
}

func checkHasResourceLimits(t *testing.T, _ operatorChart, d appsV1.Deployment) {
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	c := d.Spec.Template.Spec.Containers[0]
	require.NotEmpty(t, c.Resources.Limits)
	require.NotEmpty(t, c.Resources.Requests)
}

func checkReplicasExplicit(t *testing.T, _ operatorChart, d appsV1.Deployment) {
	require.NotNil(t, d.Spec.Replicas)
	require.GreaterOrEqual(t, int(*d.Spec.Replicas), 1)
}
