//go:build helm_render

package loopruntimecloudpublished_test

// The Helmfile bootstrap installs this chart with charts/loopruntime-cloud/values-bootstrap.yaml.
// These tests render THAT install and assert what the deleted bootstrap chart
// (infra/helm/loopruntime-cloud) used to guarantee, now expressed through typed values.

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

const bootstrapValues = "charts/loopruntime-cloud/values-bootstrap.yaml"

// renderBootstrap renders the whole chart the way Helmfile installs it, with
// optional --set overrides layered on top.
func renderBootstrap(t *testing.T, set map[string]string) string {
	t.Helper()
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, bootstrapValues)
	require.NoError(t, err)
	opts.SetValues = set
	return chartrender.RenderTemplate(t, opts, chartPath, "loopruntime-cloud", nil)
}

func bootstrapDeployment(t *testing.T, set map[string]string) appsV1.Deployment {
	t.Helper()
	var d appsV1.Deployment
	chartrender.MustUnmarshalK8SYamlByKind(t, renderBootstrap(t, set), "Deployment", &d)
	return d
}

func envVar(t *testing.T, c coreV1.Container, name string) coreV1.EnvVar {
	t.Helper()
	for _, e := range c.Env {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("container %q declares no variable %s", c.Name, name)
	return coreV1.EnvVar{}
}

func secretSource(t *testing.T, c coreV1.Container, name string) coreV1.EnvFromSource {
	t.Helper()
	for _, s := range c.EnvFrom {
		if s.SecretRef != nil && s.SecretRef.Name == name {
			return s
		}
	}
	t.Fatalf("container %q mounts no Secret %s through envFrom", c.Name, name)
	return coreV1.EnvFromSource{}
}

func TestCloudBootstrapInstall_MapsDatabaseURLFromTheCredentialsSecretURIKey(t *testing.T) {
	t.Parallel()
	c := bootstrapDeployment(t, nil).Spec.Template.Spec.Containers[0]
	ref := envVar(t, c, "DATABASE_URL").ValueFrom.SecretKeyRef
	require.NotNil(t, ref, "DATABASE_URL must come from a Secret key")
	require.Equal(t, "loopruntime-db-credentials", ref.Name)
	require.Equal(t, "uri", ref.Key, "the credentials Secret stores the URL under `uri` (B35)")
}

func TestCloudBootstrapInstall_PodsKeepTheLegacyAppLabelOutsideTheSelector(t *testing.T) {
	t.Parallel()
	d := bootstrapDeployment(t, nil)
	require.Equal(t, "loopruntime-cloud", d.Spec.Template.Labels["app"],
		"`-l app=loopruntime-cloud` selectors in taskfiles, scripts and live tests must keep matching")
	_, inSelector := d.Spec.Selector.MatchLabels["app"]
	require.False(t, inSelector, "the selector is the chart's app.kubernetes.io pair; `app` is an extra pod label only")
}

func TestCloudBootstrapInstall_StripeAPIKeysAreOptionalUnlessBillingIsRequired(t *testing.T) {
	t.Parallel()
	c := bootstrapDeployment(t, nil).Spec.Template.Spec.Containers[0]
	src := secretSource(t, c, "loopruntime-stripe-api-keys")
	require.NotNil(t, src.SecretRef.Optional)
	require.True(t, *src.SecretRef.Optional, "an install that does not charge boots without Stripe")

	c = bootstrapDeployment(t, map[string]string{"billing.required": "true"}).Spec.Template.Spec.Containers[0]
	src = secretSource(t, c, "loopruntime-stripe-api-keys")
	require.NotNil(t, src.SecretRef.Optional)
	require.False(t, *src.SecretRef.Optional, "an install that declares it charges must not start without the keys")
}

func TestCloudBootstrapInstall_RoutesTheDashboardHostWithEdgeTLSAndNoCertificate(t *testing.T) {
	t.Parallel()
	out := renderBootstrap(t, nil)
	routes := chartrender.SplitYAMLByKind(out, "IngressRoute")
	require.Len(t, routes, 1)
	require.Contains(t, routes[0], "Host(`app.dev.getloopscale.com`)")
	var route struct {
		Spec struct {
			TLS map[string]interface{} `json:"tls"`
		} `json:"spec"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(routes[0]), &route))
	require.NotNil(t, route.Spec.TLS, "the route must declare tls: the entry point is websecure")
	require.Empty(t, route.Spec.TLS, "CFManaged terminates TLS at the Cloudflare edge: no secretName")
	require.Empty(t, chartrender.SplitYAMLByKind(out, "Certificate"),
		"a CFManaged route must not ask cert-manager for a certificate")
}

func TestCloudBootstrapInstall_PinsTheNodePortTheEdgeReaches(t *testing.T) {
	t.Parallel()
	out := renderBootstrap(t, map[string]string{"service.type": "NodePort", "service.nodePort": "30002"})
	var svc coreV1.Service
	chartrender.MustUnmarshalK8SYamlByKind(t, out, "Service", &svc)
	require.Equal(t, coreV1.ServiceTypeNodePort, svc.Spec.Type)
	require.Equal(t, int32(30002), svc.Spec.Ports[0].NodePort)
}

func TestCloudBootstrapInstall_MountsAWritableTmpForExports(t *testing.T) {
	t.Parallel()
	c := bootstrapDeployment(t, nil).Spec.Template.Spec.Containers[0]
	found := false
	for _, m := range c.VolumeMounts {
		if m.MountPath == "/tmp" {
			found = true
		}
	}
	require.True(t, found, "the root filesystem is read-only and exports are staged under /tmp/loopruntime-exports")
}

func TestCloudBootstrapInstall_RefusesAnOptionalSecretWithoutAReason(t *testing.T) {
	t.Parallel()
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	opts.SetValues = map[string]string{
		"secrets.additionalSources[0].name":     "some-secret",
		"secrets.additionalSources[0].optional": "true",
	}
	_, err = chartrender.RenderTemplateE(t, opts, chartPath, "loopruntime-cloud", nil)
	require.Error(t, err, "optional is how a dependency disappears silently (B-104); it must carry a reason")
	require.True(t, strings.Contains(err.Error(), "reason"), "the refusal must name the missing field, got: %v", err)
}

func TestCloudBootstrapInstall_RendersTheDataPlaneAdminKeyExternalSecretsOnlyWhenEnabled(t *testing.T) {
	t.Parallel()
	require.Empty(t, chartrender.SplitYAMLByKind(renderBootstrap(t, nil), "ExternalSecret"),
		"off by default: they need an ESO controller")
	out := renderBootstrap(t, map[string]string{"rag.externalSecretEnabled": "true", "lens.externalSecretEnabled": "true"})
	docs := chartrender.SplitYAMLByKind(out, "ExternalSecret")
	require.Len(t, docs, 2)
	joined := strings.Join(docs, "\n")
	require.Contains(t, joined, "RAG_PLATFORM_ADMIN_KEY")
	require.Contains(t, joined, "LENS_PLATFORM_ADMIN_KEY")
}
