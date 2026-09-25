// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// The values the post-Cleanroom seeds used to patch into live objects, proven to
// be rendered by `helmfile sync` instead.
//
// Until this file, five seeds edited what Helmfile L2 renders
// (principle_violations#67): `vault-cross-cluster-addr` and `vault-role-id-sync`
// set env on the loopzero-api Deployment, `loki-cross-cluster` rewrote the Promtail
// Secret, flipped the chart's `loki` Service to NodePort and set LOKI_URL,
// `patch-kratos` and `hydra-urls` rewrote the Ory ConfigMaps, and `broker` ran a
// `helm upgrade --install` of its own. Cluster state then depended on the order
// the seeds ran in rather than on the declared values, and the next sync could
// not tell a drift from a design.
//
// Each test below names the seed whose patch it replaces and asserts the value
// the patch used to write is now in the render. The seeds that patched are
// deleted; what a seed still does is write secret material into a Secret.
package charts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Where LOKI_URL points on the k3d topology: the build cluster's Loki, published
// on the build serverlb as host port 30120, reached from the core cluster through
// `host.k3d.internal` — the docker network gateway, which the setup networking
// step pins in the core CoreDNS (host_k3d_internal_canonical_test.go). The seed
// computed the same pair at runtime from `docker network inspect`.
const lokiCrossClusterURL = "http://host.k3d.internal:30120"

// The address the core cluster reaches the core serverlb at, from a cell: host port
// 13001 is published to loopzero-api's NodePort 30001 (infra/k3d/core-cluster.yaml).
// The broker seed recorded the core server-0 CONTAINER IP instead, which docker
// reassigns on every restart (loopzero-workspace#23).
const coreAPIFromCell = "http://host.k3d.internal:13001"

// The in-cluster push URL of a Promtail that runs next to its Loki.
const colocatedLokiPushURL = "http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push"

// helmfileCoreEnv is what the core helmfile's releases require to render at all.
var helmfileCoreEnv = []string{"LOOPZERO_DB_PASSWORD=render-test"}

func renderDevPublicLoopZeroAPI(t *testing.T) string {
	t.Helper()
	return helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "dev-public", "loopzero-api", "charts/loopzero-api", helmfileCoreEnv...)
}

func configMapData(t *testing.T, rendered, name string) map[string]any {
	t.Helper()
	cm := renderedObject(t, rendered, "ConfigMap", name)
	if cm == nil {
		t.Fatalf("no ConfigMap %q in the render", name)
	}
	data, _ := cm["data"].(map[string]any)
	return data
}

// Replaces the `loki-cross-cluster` seed's Layer 3 (setDeploymentContainerEnv LOKI_URL).
func TestDevPublicLoopZeroAPIReadsLokiAtTheBuildClusterPublishedPort(t *testing.T) {
	data := configMapData(t, renderDevPublicLoopZeroAPI(t), "loopzero-api")

	if got := data["LOKI_URL"]; got != lokiCrossClusterURL {
		t.Errorf("rendered LOKI_URL = %v, want %q — the address the loki-cross-cluster seed used to "+
			"patch onto the Deployment; without it in the render the next sync disagrees with the seed", got, lokiCrossClusterURL)
	}

	published := mustRead(t, filepath.Join(repoRoot(t), "infra/k3d/build-cluster.yaml"))
	if !strings.Contains(published, "- port: 30120:30120") {
		t.Error("infra/k3d/build-cluster.yaml no longer publishes 30120:30120 — LOKI_URL would name a port nothing listens on")
	}
}

// Replaces the `vault-cross-cluster-addr` seed. The seed wrote https://<serverlb IP>:<NodePort>,
// an address the OpenBao certificate carries no SAN for; the render keeps the Service
// name the certificate does carry, and the core CoreDNS alias resolves it.
func TestDevPublicLoopZeroAPIReachesOpenBaoByTheNameItsCertificateCarries(t *testing.T) {
	data := configMapData(t, renderDevPublicLoopZeroAPI(t), "loopzero-api")

	const want = "https://openbao.secrets-system.svc.cluster.local:8200"
	if got := data["OPENBAO_VAULT_ADDR"]; got != want {
		t.Errorf("rendered OPENBAO_VAULT_ADDR = %v, want %q", got, want)
	}
}

// Replaces the `vault-role-id-sync` seed's Deployment edit. The role id is read from
// OpenBao after every vault bootstrap and changes with it, so it cannot be a literal
// in a values file (the one committed here was stale, and the seed's env override
// is what hid that). It lives in the Secret that already carries the secret_id.
func TestDevPublicLoopZeroAPIReadsTheRoleIDOnlyFromTheAppRoleSecret(t *testing.T) {
	rendered := renderDevPublicLoopZeroAPI(t)

	secret := renderedObject(t, rendered, "Secret", "loopzero-api")
	if secret == nil {
		t.Fatal("no Secret loopzero-api in the render")
	}
	for _, field := range []string{"data", "stringData"} {
		if v := dig(secret, field, "OPENBAO_VAULT_ROLE_ID"); v != nil {
			t.Errorf("the chart's own Secret carries OPENBAO_VAULT_ROLE_ID (%s) — a committed role id goes stale "+
				"at the next vault bootstrap; it must arrive only from Secret loopzero-openbao-approle", field)
		}
	}

	deploy := renderedObject(t, rendered, "Deployment", "loopzero-api")
	if deploy == nil {
		t.Fatal("no Deployment loopzero-api in the render")
	}
	if !envFromNamesSecret(deploy, "loopzero-openbao-approle") {
		t.Error("the loopzero-api container does not read Secret loopzero-openbao-approle through envFrom — the role id would reach no one")
	}
	if envSetsName(deploy, "OPENBAO_VAULT_ROLE_ID") {
		t.Error("the loopzero-api container sets OPENBAO_VAULT_ROLE_ID in env, which overrides the Secret")
	}
}

func firstContainer(deploy map[string]any) map[string]any {
	c, _ := dig(deploy, "spec", "template", "spec", "containers", 0).(map[string]any)
	return c
}

func envFromNamesSecret(deploy map[string]any, secret string) bool {
	sources, _ := firstContainer(deploy)["envFrom"].([]any)
	for _, s := range sources {
		if dig(s, "secretRef", "name") == secret {
			return true
		}
	}
	return false
}

func envSetsName(deploy map[string]any, name string) bool {
	env, _ := firstContainer(deploy)["env"].([]any)
	for _, e := range env {
		if dig(e, "name") == name {
			return true
		}
	}
	return false
}

// The Tilt-owned loopzero-api renders its ConfigMap and Secret from the same chart with
// infra/tilt/loopzero-api-values.yaml, and the seeds patched that Deployment too. Its
// declared values must say what the seeds used to write.
func TestTiltLoopZeroAPIValuesDeclareTheSeededAddressesAndNoRoleID(t *testing.T) {
	values := filepath.Join(repoRoot(t), "infra/tilt/loopzero-api-values.yaml")
	rendered := renderChart(t, publishedChart(t, "loopzero-api"), "loopzero-api", nil, values)

	data := configMapData(t, rendered, "loopzero-api")
	if got := data["LOKI_URL"]; got != lokiCrossClusterURL {
		t.Errorf("Tilt LOKI_URL = %v, want %q", got, lokiCrossClusterURL)
	}
	secret := renderedObject(t, rendered, "Secret", "loopzero-api")
	for _, field := range []string{"data", "stringData"} {
		if v := dig(secret, field, "OPENBAO_VAULT_ROLE_ID"); v != nil {
			t.Errorf("the Tilt Secret carries a committed OPENBAO_VAULT_ROLE_ID (%s); the Tilt Deployment lists "+
				"Secret loopzero-api AFTER loopzero-openbao-approle in envFrom, so the stale literal wins", field)
		}
	}
}

// Replaces the `loki-cross-cluster` seed's Layer 2 (lokiEnsureNodePort flipping the
// chart's ClusterIP `loki` Service). The grafana/loki chart hard-codes that Service
// as ClusterIP, so the bridge is its own object, rendered through the chart's
// extraObjects on the cluster where Loki runs.
func TestBuildLokiPublishesTheCrossClusterNodePort(t *testing.T) {
	for _, env := range []string{"dev-public", "standalone-multi"} {
		t.Run(env, func(t *testing.T) {
			values := helmfileValues(t, "build.helmfile.yaml.gotmpl", env, "loki")
			objects, _ := values["extraObjects"].([]any)
			var bridge any
			for _, o := range objects {
				if dig(o, "kind") == "Service" && dig(o, "metadata", "name") == "loki-cross-cluster" {
					bridge = o
				}
			}
			if bridge == nil {
				t.Fatal("the loki release declares no Service loki-cross-cluster in extraObjects")
			}
			if got := dig(bridge, "spec", "type"); got != "NodePort" {
				t.Errorf("loki-cross-cluster type = %v, want NodePort", got)
			}
			port := dig(bridge, "spec", "ports", 0)
			if fmt.Sprint(dig(port, "nodePort")) != "30120" || fmt.Sprint(dig(port, "port")) != "3100" {
				t.Errorf("loki-cross-cluster port = %v, want port 3100 on nodePort 30120 (published by build-cluster.yaml)", port)
			}
			if got := dig(bridge, "spec", "selector", "app.kubernetes.io/component"); got != "single-binary" {
				t.Errorf("loki-cross-cluster selects component %v, want single-binary — the pod the loki chart runs here", got)
			}
		})
	}
}

// Replaces the `loki-cross-cluster` seed's Layer 1, which rewrote the build
// Promtail Secret's push URL to the in-cluster Service on the k3d topologies —
// the ones where the build cluster's Loki is the platform's Loki.
func TestBuildPromtailPushesToTheColocatedLoki(t *testing.T) {
	for _, env := range []string{"dev-public", "standalone-multi", "standalone-public"} {
		t.Run(env, func(t *testing.T) {
			values := helmfileValues(t, "build.helmfile.yaml.gotmpl", env, "promtail")
			if got := dig(values, "config", "clients", 0, "url"); got != colocatedLokiPushURL {
				t.Errorf("build Promtail pushes to %v, want %q — Loki runs in the same cluster", got, colocatedLokiPushURL)
			}
		})
	}
}

// Where the environment names a central Loki (prod), the build Promtail keeps
// pushing there: the colocated rule is a k3d topology fact and must not move prod.
func TestProdBuildPromtailKeepsTheCentralPushURL(t *testing.T) {
	values := helmfileValues(t, "build.helmfile.yaml.gotmpl", "prod", "promtail")
	const want = "http://loki.loopzero-platform.internal:3100/loki/api/v1/push" // environments/prod.yaml loki.pushUrl
	if got := dig(values, "config", "clients", 0, "url"); got != want {
		t.Errorf("prod build Promtail pushes to %v, want the central %q", got, want)
	}
}

// A cell has no Loki of its own: its Promtail keeps the environment's push URL.
// This pins the other half, so the build-only rule cannot leak onto the cells.
func TestCellPromtailKeepsTheEnvironmentPushURL(t *testing.T) {
	values := helmfileValues(t, "cell.helmfile.yaml.gotmpl", "dev", "promtail", "LOOPZERO_CELL_CONTEXT=k3d-loopzero-cell-0")
	const want = "http://172.18.0.3:30094/loki/api/v1/push" // infra/helmfile/environments/dev.yaml loki.pushUrl
	if got := dig(values, "config", "clients", 0, "url"); got != want {
		t.Errorf("cell Promtail pushes to %v, want the environment's loki.pushUrl %q", got, want)
	}
}

// Replaces the `broker` seed's `helm upgrade --install loopzero-broker` on the cell,
// which Helmfile never saw.
func TestDevPublicCellInstallsTheSecretFetchBroker(t *testing.T) {
	for cell, ctx := range map[string]string{"cell-0": "k3d-loopzero-cell-0", "cell-1": "k3d-loopzero-cell-1"} {
		t.Run(cell, func(t *testing.T) {
			rendered := helmfileTemplate(t, "cell.helmfile.yaml.gotmpl", "dev-public", "loopzero-broker",
				"infra/helm/loopzero-external-secrets", "LOOPZERO_CELL_CONTEXT="+ctx)

			store := renderedObject(t, rendered, "ClusterSecretStore", "loopzero-broker")
			if store == nil {
				t.Fatal("no ClusterSecretStore loopzero-broker in the cell render")
			}
			url, _ := dig(store, "spec", "provider", "webhook", "url").(string)
			if !strings.HasPrefix(url, coreAPIFromCell+"/") {
				t.Errorf("broker url = %q, want it under %q", url, coreAPIFromCell)
			}
			if got := dig(store, "spec", "provider", "webhook", "headers", "X-Cell-ID"); got != cell {
				t.Errorf("the broker on %s sends X-Cell-ID %v — loopzero-api binds a secret fetch to the cell that asked", cell, got)
			}
			if es := renderedObjectsOfKind(t, rendered, "ExternalSecret"); len(es) != 0 {
				t.Errorf("the cell broker release renders %d platform ExternalSecrets; they belong to the core and "+
					"build clusters, and one targets a namespace the cell does not have", len(es))
			}
		})
	}
}

// The chart refuses a plaintext bearer outside dev-public; the seed got past that
// guard by always passing envName=dev-public. Helmfile passes the real environment,
// so the broker is rendered only where the environment opts in.
func TestSecretFetchBrokerIsNotRenderedOutsideDevPublic(t *testing.T) {
	for _, env := range []string{"standalone-multi", "standalone-public"} {
		t.Run(env, func(t *testing.T) {
			rendered := helmfileTemplate(t, "cell.helmfile.yaml.gotmpl", env, "loopzero-broker",
				"infra/helm/loopzero-external-secrets", "LOOPZERO_CELL_CONTEXT=k3d-loopzero-cell-0")
			if stores := renderedObjectsOfKind(t, rendered, "ClusterSecretStore"); len(stores) != 0 {
				t.Errorf("%s renders the broker store; its chart forbids a plaintext bearer there", env)
			}
		})
	}
}

// Replaces the `patch-kratos` seed. Every URL it wrote is a value Helmfile renders
// from the environment (publicAuthUrl, uiBaseUrl, cookieDomain, corsAllowedOrigins);
// the OIDC and Resend credentials are read from the operator's environment at
// template time, which is the mechanism infra/helm/ory-kratos documents.
func TestDevPublicKratosDeclaresTheURLsThePatchSeedWrote(t *testing.T) {
	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", "dev-public", "ory-kratos",
		append(helmfileCoreEnv, "LOOPZERO_GITHUB_CLIENT_ID=gh-id", "LOOPZERO_GOOGLE_CLIENT_ID=google-id",
			"LOOPZERO_RESEND_API_KEY=re_render_test")...)
	cfg := dig(values, "kratos", "config")

	want := map[string]string{
		"serve.public.base_url":                                     "https://auth-dev.loopzero.example/",
		"selfservice.methods.oidc.config.base_redirect_uri":         "https://auth-dev.loopzero.example/",
		"selfservice.default_browser_return_url":                    "https://app-dev.loopzero.example/",
		"selfservice.flows.login.ui_url":                            "https://app-dev.loopzero.example/login",
		"selfservice.flows.registration.ui_url":                     "https://app-dev.loopzero.example/register",
		"selfservice.flows.verification.ui_url":                     "https://app-dev.loopzero.example/verification",
		"selfservice.flows.recovery.ui_url":                         "https://app-dev.loopzero.example/recovery",
		"selfservice.flows.settings.ui_url":                         "https://app-dev.loopzero.example/settings",
		"selfservice.flows.error.ui_url":                            "https://app-dev.loopzero.example/login",
		"selfservice.flows.logout.after.default_browser_return_url": "https://app-dev.loopzero.example/login",
		"cookies.domain":                                            "loopzero.example",
		"session.cookie.domain":                                     "loopzero.example",
		"selfservice.methods.oidc.config.providers.0.client_id":     "gh-id",
		"selfservice.methods.oidc.config.providers.1.client_id":     "google-id",
		"courier.http.request_config.headers.Authorization":         "Bearer re_render_test",
	}
	for path, expected := range want {
		if got := dig(cfg, dottedPath(path)...); got != expected {
			t.Errorf("kratos.config.%s = %v, want %q", path, got, expected)
		}
	}
	if got := dig(cfg, "serve", "public", "cors", "enabled"); got != true {
		t.Errorf("kratos CORS enabled = %v, want true", got)
	}
	origins, _ := dig(cfg, "serve", "public", "cors", "allowed_origins").([]any)
	if len(origins) == 0 || origins[0] != "https://app-dev.loopzero.example" {
		t.Errorf("kratos CORS origins = %v, want the dashboard origin", origins)
	}
}

// Replaces the `hydra-urls` seed, whose regexes only ever matched the chart's
// localhost defaults that the dev-public overlay already replaces.
func TestDevPublicHydraDeclaresTheURLsTheSeedRewrote(t *testing.T) {
	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", "dev-public", "ory-hydra", helmfileCoreEnv...)
	urls := dig(values, "hydra", "config", "urls")

	want := map[string]string{
		"self.issuer":         "https://auth-dev.loopzero.example/",
		"self.public":         "https://auth-dev.loopzero.example/",
		"device.verification": "https://app-dev.loopzero.example/device-verify",
		"device.success":      "https://app-dev.loopzero.example/device-success",
		"login":               "https://app-dev.loopzero.example/auth/login",
		"consent":             "https://app-dev.loopzero.example/auth/consent",
		"logout":              "https://app-dev.loopzero.example/logout",
		"error":               "https://app-dev.loopzero.example/auth/error",
	}
	for path, expected := range want {
		if got := dig(urls, dottedPath(path)...); got != expected {
			t.Errorf("hydra.config.urls.%s = %v, want %q", path, got, expected)
		}
	}
}

// dottedPath turns "a.b.0.c" into the dig path, reading digit-only segments as
// list indexes.
func dottedPath(path string) []any {
	var out []any
	for _, seg := range strings.Split(path, ".") {
		var idx int
		if _, err := fmt.Sscanf(seg, "%d", &idx); err == nil && fmt.Sprint(idx) == seg {
			out = append(out, idx)
			continue
		}
		out = append(out, seg)
	}
	return out
}

// The seeds whose patches this file replaces must not come back under the same
// names: the registry is the one place `loopzero-ops seed` looks them up.
func TestNoSeedEditsAnObjectHelmfileRenders(t *testing.T) {
	registry := mustRead(t, filepath.Join(repoRoot(t), "cmd/loopzero-ops/seed_wave1.go"))
	for _, ctor := range []string{
		"NewVaultCrossClusterAddrSeed", "NewLokiCrossClusterSeed", "NewPatchKratosSeed", "NewHydraURLsSeed",
	} {
		if strings.Contains(registry, ctor) {
			t.Errorf("seed_wave1.go still registers %s — its patch is rendered by helmfile now", ctor)
		}
	}
	chain := mustRead(t, filepath.Join(repoRoot(t), "infra/scripts/local/seed-post-cleanroom-all.sh"))
	for _, step := range []string{"seed patch-kratos", "seed hydra-urls", "seed vault-cross-cluster-addr", "seed loki-cross-cluster"} {
		if strings.Contains(chain, step) {
			t.Errorf("seed-post-cleanroom-all.sh still runs %q", step)
		}
	}
	if _, err := os.Stat(filepath.Join(repoRoot(t), "cmd/loopzero-ops/internal/seed/broker.go")); err == nil {
		broker := mustRead(t, filepath.Join(repoRoot(t), "cmd/loopzero-ops/internal/seed/broker.go"))
		if strings.Contains(broker, `"helm"`) {
			t.Error("the broker seed still executes helm — the loopzero-broker release belongs to the cell helmfile")
		}
	}
}
