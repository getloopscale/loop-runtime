//go:build helm_render

package loopzeroapipublished_test

// The Helmfile bootstrap installs this chart with charts/loopzero-api/values-bootstrap.yaml.
// These tests render THAT install and assert what the deleted bootstrap chart
// (infra/helm/loopzero-api) used to guarantee, now expressed through typed values.

import (
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	rbacV1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

const bootstrapValues = "charts/loopzero-api/values-bootstrap.yaml"

// renderInstall renders the whole chart with the given values file ("" for the
// published defaults) and optional --set overrides layered on top.
func renderInstall(t *testing.T, valuesFile string, set map[string]string) string {
	t.Helper()
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, valuesFile)
	require.NoError(t, err)
	opts.SetValues = set
	return chartrender.RenderTemplate(t, opts, chartPath, "loopzero-api", nil)
}

func renderBootstrap(t *testing.T, set map[string]string) string {
	t.Helper()
	return renderInstall(t, bootstrapValues, set)
}

func apiContainer(t *testing.T, out string) (appsV1.Deployment, coreV1.Container) {
	t.Helper()
	var d appsV1.Deployment
	chartrender.MustUnmarshalK8SYamlByKind(t, out, "Deployment", &d)
	require.NotEmpty(t, d.Spec.Template.Spec.Containers)
	return d, d.Spec.Template.Spec.Containers[0]
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

func TestAPIBootstrapInstall_PullsTheCISentinelFromTheRegistry(t *testing.T) {
	t.Parallel()
	_, c := apiContainer(t, renderBootstrap(t, nil))
	require.Equal(t, "registry.digitalocean.com/loopzero-system/loopzero-api:SET-BY-CI", c.Image,
		"a forgotten tag override must fail with ImagePullBackOff naming SET-BY-CI (issue #232)")
}

func TestAPIBootstrapInstall_RequiresTheDatabaseCredentialsSecret(t *testing.T) {
	t.Parallel()
	_, c := apiContainer(t, renderBootstrap(t, nil))
	src := secretSource(t, c, "loopzero-db-credentials")
	require.NotNil(t, src.SecretRef.Optional)
	require.False(t, *src.SecretRef.Optional,
		"an API up without a database answers 200 on the shallow probe with 27 of 36 surfaces off (B-116)")
	ref := envVar(t, c, "DATABASE_URL").ValueFrom.SecretKeyRef
	require.NotNil(t, ref, "DATABASE_URL must come from a Secret key")
	require.Equal(t, "loopzero-db-credentials", ref.Name)
	require.Equal(t, "uri", ref.Key, "the credentials Secret stores the URL under `uri` (B35)")
}

func TestAPIBootstrapInstall_TakesTheAppRoleSecretIDOnlyFromAnOptionalSecret(t *testing.T) {
	t.Parallel()
	_, c := apiContainer(t, renderBootstrap(t, nil))
	src := secretSource(t, c, "loopzero-openbao-approle")
	require.NotNil(t, src.SecretRef.Optional)
	require.True(t, *src.SecretRef.Optional, "installs without OpenBao boot without it")
	for _, e := range c.Env {
		require.NotEqual(t, "OPENBAO_VAULT_SECRET_ID", e.Name,
			"the secret_id must never be an inline variable (plan 2026-06-10-j22-openbao-transit-eso-sync T6.1)")
	}
}

func TestAPIBootstrapInstall_TakesTheGitOpsTokensFromTheirSecrets(t *testing.T) {
	t.Parallel()
	_, c := apiContainer(t, renderBootstrap(t, nil))
	for name, want := range map[string][2]string{
		"GITEA_TOKEN":  {"gitea-admin", "token"},                // B78
		"ARGOCD_TOKEN": {"loopzero-api-tokens", "argocd-token"}, // F-12
	} {
		ref := envVar(t, c, name).ValueFrom.SecretKeyRef
		require.NotNil(t, ref, "%s must come from a Secret key", name)
		require.Equal(t, want[0], ref.Name, name)
		require.Equal(t, want[1], ref.Key, name)
	}
}

func TestAPIBootstrapInstall_MountsTheClusterKubeconfigsAndPointsTheBinaryAtThem(t *testing.T) {
	t.Parallel()
	d, c := apiContainer(t, renderBootstrap(t, nil))
	var vol *coreV1.Volume
	for i := range d.Spec.Template.Spec.Volumes {
		if d.Spec.Template.Spec.Volumes[i].Name == "cluster-kubeconfigs" {
			vol = &d.Spec.Template.Spec.Volumes[i]
		}
	}
	require.NotNil(t, vol, "the bootstrap install reaches the build and cell clusters through these kubeconfigs (B36)")
	require.NotNil(t, vol.Secret)
	require.Equal(t, "loopzero-cluster-kubeconfigs", vol.Secret.SecretName)
	require.NotNil(t, vol.Secret.Optional)
	require.True(t, *vol.Secret.Optional, "the Secret is generated after the chart is installed")
	for name, file := range map[string]string{
		"CORE_KUBECONFIG":   "core.kubeconfig",
		"BUILD_KUBECONFIG":  "build.kubeconfig",
		"CELL_0_KUBECONFIG": "cell-0.kubeconfig",
	} {
		require.Equal(t, "/etc/loopzero/kubeconfigs/"+file, envVar(t, c, name).Value, name)
	}
}

func TestAPIBootstrapInstall_PodsKeepTheLegacyAppLabelOutsideTheSelector(t *testing.T) {
	t.Parallel()
	d, _ := apiContainer(t, renderBootstrap(t, nil))
	require.Equal(t, "loopzero-api", d.Spec.Template.Labels["app"],
		"`-l app=loopzero-api` selectors in network policies, monitoring, scripts and tests must keep matching")
	_, inSelector := d.Spec.Selector.MatchLabels["app"]
	require.False(t, inSelector, "the selector is the chart's app.kubernetes.io pair; `app` is an extra pod label only")
}

func TestAPIBootstrapInstall_MountsAWritableTmpForTheGitOpsPusher(t *testing.T) {
	t.Parallel()
	_, c := apiContainer(t, renderBootstrap(t, nil))
	found := false
	for _, m := range c.VolumeMounts {
		if m.MountPath == "/tmp" {
			found = true
		}
	}
	require.True(t, found, "the root filesystem is read-only and the pusher clones under /tmp (B79)")
}

func TestAPIBootstrapInstall_RoutesTheAPIHostThroughTheEdgeLimitsWithACertificate(t *testing.T) {
	t.Parallel()
	out := renderBootstrap(t, nil)
	routes := chartrender.SplitYAMLByKind(out, "IngressRoute")
	require.Len(t, routes, 1)
	require.Contains(t, routes[0], "Host(`api.dev.loopzero.example`)")
	var route struct {
		Spec struct {
			Routes []struct {
				Middlewares []struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
				} `json:"middlewares"`
			} `json:"routes"`
			TLS struct {
				SecretName string `json:"secretName"`
			} `json:"tls"`
		} `json:"spec"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(routes[0]), &route))
	require.Len(t, route.Spec.Routes, 1)
	var names []string
	for _, m := range route.Spec.Routes[0].Middlewares {
		names = append(names, m.Namespace+"/"+m.Name)
	}
	require.ElementsMatch(t, []string{"traefik/rate-limit", "traefik/body-limit"}, names,
		"the edge is the single instance of the rate limit and the body bound (SD-04.6, SD-14.1)")
	require.NotEmpty(t, route.Spec.TLS.SecretName, "TLS terminates at Traefik with the cert-manager certificate")
	certs := chartrender.SplitYAMLByKind(out, "Certificate")
	require.Len(t, certs, 1)
	require.Contains(t, certs[0], "letsencrypt-prod")
}

func TestAPIBootstrapInstall_PinsTheNodePortTheServerlbPublishes(t *testing.T) {
	t.Parallel()
	out := renderBootstrap(t, map[string]string{"service.type": "NodePort", "service.nodePort": "30001"})
	var svc coreV1.Service
	chartrender.MustUnmarshalK8SYamlByKind(t, out, "Service", &svc)
	require.Equal(t, coreV1.ServiceTypeNodePort, svc.Spec.Type)
	require.Equal(t, int32(30001), svc.Spec.Ports[0].NodePort, "k3d maps 30001 to host port 13001 (F-01)")
}

// The two grants the bootstrap install needs beyond the published RBAC are behind
// values that default off, so an ArgoCD install keeps the narrower grant.
func TestAPIInstall_ExtraGrantsAreOffByDefaultAndOnForTheBootstrap(t *testing.T) {
	t.Parallel()
	hasApplications := func(out string) bool {
		var role rbacV1.ClusterRole
		chartrender.MustUnmarshalK8SYamlByKind(t, out, "ClusterRole", &role)
		for _, r := range role.Rules {
			for _, res := range r.Resources {
				if res == "applications" {
					return true
				}
			}
		}
		return false
	}

	published := renderInstall(t, "", nil)
	require.False(t, hasApplications(published), "rbac.argocdApplications must default off")
	require.Empty(t, chartrender.SplitYAMLByKind(published, "Role"), "rbac.controlPlaneSecrets must default off")

	bootstrap := renderBootstrap(t, nil)
	require.True(t, hasApplications(bootstrap), "the bootstrap API patches and deletes ArgoCD Applications")
	roles := chartrender.SplitYAMLByKind(bootstrap, "Role")
	require.Len(t, roles, 1)
	var role rbacV1.Role
	require.NoError(t, yaml.Unmarshal([]byte(roles[0]), &role))
	require.Len(t, role.Rules, 1)
	require.Equal(t, []string{"secrets"}, role.Rules[0].Resources)
	require.NotContains(t, role.Rules[0].Verbs, "list", "the namespaced grant names what it needs, not a listing")
	require.False(t, strings.Contains(roles[0], "ClusterRole"), "the secrets grant is namespaced, never cluster-wide")
}

// `optional: true` is what lets a dependency vanish without startup saying anything
// (B-104: six of ten envFrom sources were absent in dev-public while the API
// served). The bootstrap chart asserted a comment above each one by scanning its
// template text; the schema now refuses an optional source, key or kubeconfig
// Secret that carries no reason, so an undecided exemption cannot render at all.
func TestAPIInstall_RefusesAnOptionalSecretWithoutAReason(t *testing.T) {
	t.Parallel()
	chartPath, err := chartrender.PublishedChartPath(chartName)
	require.NoError(t, err)
	for name, set := range map[string]map[string]string{
		"envFrom source": {
			"secrets.additionalSources[0].name":     "some-secret",
			"secrets.additionalSources[0].optional": "true",
		},
		"secret key": {
			"secrets.keyRefs[0].name":       "SOME_VAR",
			"secrets.keyRefs[0].secretName": "some-secret",
			"secrets.keyRefs[0].key":        "value",
			"secrets.keyRefs[0].optional":   "true",
		},
		"kubeconfig Secret": {
			"kubeconfig.clusterSecret.name":     "some-secret",
			"kubeconfig.clusterSecret.optional": "true",
		},
	} {
		t.Run(name, func(t *testing.T) {
			opts, err := chartrender.NewOptions(chartPath, "")
			require.NoError(t, err)
			opts.SetValues = set
			_, err = chartrender.RenderTemplateE(t, opts, chartPath, "loopzero-api", nil)
			require.Error(t, err, "an optional Secret must carry a reason (B-104)")
			require.Contains(t, err.Error(), "reason", "the refusal must name the missing field")
		})
	}
}
