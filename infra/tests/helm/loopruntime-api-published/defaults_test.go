//go:build helm_render

// Package loopruntimeapipublished_test — render-time tests for the PUBLISHED
// variant of loopruntime-api (Plan 2026-05-20 v1.1, Task #128 follow-up).
//
// There is one loopruntime-api chart: ArgoCD consumes it with these defaults, and the
// Helmfile bootstrap installs it with charts/loopruntime-api/values-bootstrap.yaml
// (bootstrap_install_test.go). The bootstrap copy under infra/helm/loopruntime-api and
// its render suite were deleted when the pair became one chart.
//
// Published shape (charts/loopruntime-api, uses loopruntime-common library):
//   - Full app.kubernetes.io/* labels via loopruntime-common library
//   - Separate Service, ConfigMap, Secret, NetworkPolicy, PDB,
//     ServiceAccount, ServiceMonitor templates
//   - Container-level securityContext FULLY hardened
//   - priorityClassName: system-cluster-critical
//   - podAntiAffinity (HA scheduling)
//   - checksum/config + checksum/secret annotations (config drift
//     triggers automatic pod restart)
//   - HA default (replicas >= 2)
package loopruntimeapipublished_test

import (
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

const (
	chartName   = "loopruntime-api"
	releaseName = "tap-pub"
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

func TestPublished_FullStandardLabels(t *testing.T) {
	t.Parallel()
	// Drift detector: published variant MUST use loopruntime-common library
	// labels (5 standard app.kubernetes.io/* keys). If chart drifts back
	// to minimal-label style (regression to bootstrap pattern), Prometheus
	// servicemonitor selectors + many K8s native tools stop matching.
	d := renderDeploy(t)
	for _, key := range []string{
		"app.kubernetes.io/name",
		"app.kubernetes.io/instance",
		"app.kubernetes.io/managed-by",
		"app.kubernetes.io/version",
		"app.kubernetes.io/part-of",
	} {
		require.NotEmptyf(t, d.Labels[key],
			"published variant must emit standard label %q "+
				"(missing = drift toward bootstrap minimal-label style)", key)
	}
}

func TestPublished_PodAntiAffinityForHA(t *testing.T) {
	t.Parallel()
	// Published variant declares podAntiAffinity (preferred). Bootstrap
	// does NOT. This is a real HA correctness invariant for production.
	d := renderDeploy(t)
	require.NotNil(t, d.Spec.Template.Spec.Affinity,
		"published variant must declare affinity (HA scheduling)")
	require.NotNil(t, d.Spec.Template.Spec.Affinity.PodAntiAffinity,
		"published variant must declare podAntiAffinity to spread "+
			"replicas across nodes (single-node failure tolerance)")
}

func TestPublished_PriorityClassSetSystemClusterCritical(t *testing.T) {
	t.Parallel()
	// Production loopruntime-api is control-plane critical — must use
	// system-cluster-critical so it preempts tenant workloads under
	// resource pressure.
	d := renderDeploy(t)
	require.Equal(t, "system-cluster-critical",
		d.Spec.Template.Spec.PriorityClassName,
		"published variant must declare priorityClassName=system-cluster-critical")
}

func TestPublished_ContainerFullyHardened(t *testing.T) {
	t.Parallel()
	// Published variant has container-level securityContext fully
	// hardened via loopruntime-common. Bootstrap relies on pod cascade. The
	// container-level explicit is stronger because it can't be
	// overridden by malicious child containers.
	d := renderDeploy(t)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	for i, c := range d.Spec.Template.Spec.Containers {
		sc := c.SecurityContext
		require.NotNilf(t, sc, "container[%d] container-level securityContext", i)
		require.NotNilf(t, sc.AllowPrivilegeEscalation, "container[%d] APE", i)
		require.Falsef(t, *sc.AllowPrivilegeEscalation, "container[%d]", i)
		require.NotNilf(t, sc.ReadOnlyRootFilesystem, "container[%d] roRoot", i)
		require.Truef(t, *sc.ReadOnlyRootFilesystem, "container[%d] roRoot true", i)
		require.NotNilf(t, sc.Capabilities, "container[%d] caps", i)
		require.Containsf(t, sc.Capabilities.Drop, coreV1.Capability("ALL"),
			"container[%d] must drop ALL caps (production hardening)", i)
		require.NotNilf(t, sc.SeccompProfile, "container[%d] seccomp", i)
		require.Equalf(t, coreV1.SeccompProfileTypeRuntimeDefault,
			sc.SeccompProfile.Type, "container[%d] seccomp RuntimeDefault", i)
	}
}

func TestPublished_HARequiresMultipleReplicas(t *testing.T) {
	t.Parallel()
	d := renderDeploy(t)
	require.NotNil(t, d.Spec.Replicas)
	require.GreaterOrEqualf(t, int(*d.Spec.Replicas), 2,
		"published loopruntime-api defaults to HA (>= 2 replicas per CTO mandate "+
			"2026-05-14 + chaos-test C01)")
}

func TestPublished_ConfigChecksumAnnotation(t *testing.T) {
	t.Parallel()
	// Pod template annotations include checksum/config + checksum/secret
	// so any change to ConfigMap or Secret triggers a rolling pod restart
	// (otherwise pods would silently run with stale config until manual
	// delete). Bootstrap variant lacks these annotations.
	d := renderDeploy(t)
	ann := d.Spec.Template.Annotations
	require.NotEmpty(t, ann, "pod template MUST have annotations")
	require.NotEmptyf(t, ann["checksum/config"],
		"published variant must annotate pod with checksum/config — "+
			"ConfigMap changes would NOT roll pods otherwise (stale config "+
			"silently runs in prod)")
	require.NotEmptyf(t, ann["checksum/secret"],
		"published variant must annotate pod with checksum/secret — "+
			"Secret rotation would NOT roll pods otherwise")
}

func TestPublished_HasSeparateServiceTemplate(t *testing.T) {
	t.Parallel()
	// Sanity: published variant has Service in its own template
	// (templates/service.yaml), not multiplexed into deployment.yaml.
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/service.yaml"})
	var svc coreV1.Service
	chartrender.MustUnmarshalK8SYamlByKind(t, out, "Service", &svc)
	require.NotEmpty(t, svc.Spec.Ports,
		"published variant Service must declare at least one port")
}

func TestPublished_ServicePortMatchesContainerPort(t *testing.T) {
	t.Parallel()
	// Port mapping validation — Service targetPort must resolve to a
	// container port. Same B50 class as bootstrap test.
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)

	d := renderDeploy(t)

	svcOut := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/service.yaml"})
	var svc coreV1.Service
	chartrender.MustUnmarshalK8SYamlByKind(t, svcOut, "Service", &svc)

	containerPorts := make(map[int32]string)
	for _, p := range d.Spec.Template.Spec.Containers[0].Ports {
		containerPorts[p.ContainerPort] = p.Name
	}
	mapped := 0
	for _, sp := range svc.Spec.Ports {
		switch sp.TargetPort.Type {
		case 0:
			if _, ok := containerPorts[sp.TargetPort.IntVal]; ok {
				mapped++
			}
		case 1:
			for _, name := range containerPorts {
				if name == sp.TargetPort.StrVal {
					mapped++
				}
			}
		}
	}
	require.GreaterOrEqualf(t, mapped, 1,
		"at least one Service port must map to a container port "+
			"(B50 class) — container=%v, service=%+v",
		containerPorts, svc.Spec.Ports)
}

func TestPublished_HasNetworkPolicy(t *testing.T) {
	t.Parallel()
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/networkpolicy.yaml"})
	docs := chartrender.SplitYAMLByKind(out, "NetworkPolicy")
	require.NotEmpty(t, docs,
		"published variant must declare NetworkPolicy (production "+
			"zero-trust posture)")
}

func TestPublished_HasPodDisruptionBudget(t *testing.T) {
	t.Parallel()
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, chartPath, releaseName,
		[]string{"templates/pdb.yaml"})
	docs := chartrender.SplitYAMLByKind(out, "PodDisruptionBudget")
	require.NotEmpty(t, docs,
		"published variant must declare PDB (prevents drain-time outage)")
}

func TestPublished_DedicatedServiceAccount(t *testing.T) {
	t.Parallel()
	// Published variant uses a dedicated SA (not "default"). PV-7 of
	// dogfood Prova de Vida asserts the same at runtime.
	d := renderDeploy(t)
	require.NotEqualf(t, "default", d.Spec.Template.Spec.ServiceAccountName,
		"published variant must use dedicated ServiceAccount, not 'default'")
	require.NotEmpty(t, d.Spec.Template.Spec.ServiceAccountName)
}
