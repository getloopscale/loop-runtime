// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loop-console on dev-public reaches three backends through configuration only: the
// Prometheus the cost aggregator queries, and the loop-runtime-rag and loop-runtime-lens brokers
// behind the dashboard. Each has already been lost the same way — a variable set
// by hand with `kubectl set env`, gone at the next `helmfile sync` — so the wiring
// is locked on what Helmfile renders.
//
// WHAT THESE DO NOT PROVE. A render is not a deployment: measured live on
// 2026-09-05, with every cost-aggregator assertion below passing, the running
// loop-console Deployment had ZERO PROM|COST variables (created 2026-08-10, never
// re-synced), `select count(*) from cost_counters` returned 0, and two of the three
// series the aggregator queries (loop_runtime_egress_bytes_total, loop_runtime_docr_pulls_total) are
// emitted by no repository. The render is link 1 of a four-link chain (overlay →
// deploy → aggregator → instrumentation); a guard that named the whole chain while
// observing one link is how B-033 stayed invisible for three weeks after its fix.

// canonicalPrometheusURL is the one Prometheus both overlays name (plan D1 of B-033).
const canonicalPrometheusURL = "http://prometheus-server.monitoring.svc.cluster.local:80"

// TestDevPublicLoopConsolePublishesThePrometheusURLTheCostAggregatorQueries — without
// LOOP_RUNTIME_PROMETHEUS_URL the aggregator disables itself at boot, cost_counters stays
// empty, and the CostEnforcer in api/ degrades to pass-through: the build, egress
// and pull caps go unenforced (B-033).
func TestDevPublicLoopConsolePublishesThePrometheusURLTheCostAggregatorQueries(t *testing.T) {
	got := configMapData(t, renderDevPublicLoopConsole(t), "loop-console")["LOOP_RUNTIME_PROMETHEUS_URL"]
	if got != canonicalPrometheusURL {
		t.Errorf("ConfigMap/loop-console LOOP_RUNTIME_PROMETHEUS_URL = %v, want %q", got, canonicalPrometheusURL)
	}
}

// TestDevPublicLoopConsoleAndLoopRuntimeAPIQueryTheSamePrometheus — the loop-runtime-api readiness
// poller (moved from loop-runtime-app-operator on 2026-09-25) names the same Prometheus in
// its own overlay. Editing one of the two files without the other would pass every
// other check and break at runtime, the class of the original defect.
func TestDevPublicLoopConsoleAndLoopRuntimeAPIQueryTheSamePrometheus(t *testing.T) {
	cloud := configMapData(t, renderDevPublicLoopConsole(t), "loop-console")["LOOP_RUNTIME_PROMETHEUS_URL"]
	api := dig(helmfileValues(t, "core.helmfile.yaml.gotmpl", "dev-public", "loop-runtime-api", helmfileCoreEnv...),
		"dataPlanePoller", "prometheusUrl")
	if api == nil {
		t.Fatal("the dev-public loop-runtime-api values carry no dataPlanePoller.prometheusUrl — the parity has nothing to compare")
	}
	if cloud != api {
		t.Errorf("loop-console LOOP_RUNTIME_PROMETHEUS_URL = %v but loop-runtime-api dataPlanePoller.prometheusUrl = %v — "+
			"both overlays must name the same Prometheus", cloud, api)
	}
}

// dashboardBroker is one backend whose admin key the loop-console container reads
// through a secretKeyRef.
type dashboardBroker struct {
	name     string // rag, lens
	adminKey string // env var and Secret key
	secret   string // the Secret the seed writes
}

var dashboardBrokers = []dashboardBroker{
	{"rag", "RAG_PLATFORM_ADMIN_KEY", "loop-runtime-rag-admin-key"},
	{"lens", "LENS_PLATFORM_ADMIN_KEY", "loop-runtime-lens-admin-key"},
}

// TestDevPublicLoopConsoleReachesTheDashboardBrokers — with the base URL empty the
// broker stays nil and every /v1/dashboard/<name>/* route is skipped (the RAG half
// was delegated brief 2026-07-15; the lens half covers M41 read-only + M44
// management routes). THEMORY_BASE_URL also registers the edge's memory
// forward-auth: unset, /v1/memory/* is authorised by nothing on this cluster (#470).
func TestDevPublicLoopConsoleReachesTheDashboardBrokers(t *testing.T) {
	data := configMapData(t, renderDevPublicLoopConsole(t), "loop-console")
	for _, baseURL := range []string{"THEMORY_BASE_URL", "RAG_BASE_URL", "LENS_BASE_URL"} {
		t.Run(baseURL, func(t *testing.T) {
			url, _ := data[baseURL].(string)
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				t.Errorf("ConfigMap/loop-console %s = %q, want an http(s):// URL", baseURL, url)
			}
		})
	}
}

func TestDevPublicLoopConsoleReadsTheBrokerAdminKeysFromTheirSecrets(t *testing.T) {
	rendered := renderDevPublicLoopConsole(t)
	deploy := renderedObject(t, rendered, "Deployment", "loop-console")
	if deploy == nil {
		t.Fatal("no Deployment loop-console in the render")
	}
	for _, b := range dashboardBrokers {
		t.Run(b.name, func(t *testing.T) {
			ref := containerEnvSecretRef(deploy, "cloud", b.adminKey)
			if ref == nil {
				t.Fatalf("container cloud has no %s sourced from a secretKeyRef", b.adminKey)
			}
			if ref["name"] != b.secret || ref["key"] != b.adminKey || ref["optional"] != true {
				t.Errorf("%s secretKeyRef = %v, want name=%s key=%s optional=true", b.adminKey, ref, b.secret, b.adminKey)
			}
		})
	}
}

// TestDevPublicLoopConsoleLeavesTheBrokerAdminKeysToTheSeed — on dev-public the admin
// keys are minted by `loop-runtime-ops seed data-plane-admin-keys`, which writes the Secrets the
// Deployment reads. The ExternalSecrets that used to target the same Secrets never
// synced here: they read an OpenBao property no step writes, through a
// ClusterSecretStore this environment does not run (measured 2026-09-30:
// SecretSyncedError on both, no ClusterSecretStore on the core cluster). Rendering them
// would give each Secret a second writer, owned by ESO, that could overwrite the seed's
// key the day a store appears (#470). The template stays for installations that do
// hold the keys in OpenBao.
func TestDevPublicLoopConsoleLeavesTheBrokerAdminKeysToTheSeed(t *testing.T) {
	template := filepath.Join(repoRoot(t), "charts/loop-console/templates/externalsecret-admin-keys.yaml")
	if _, err := os.Stat(template); err != nil {
		t.Fatalf("the ExternalSecret template is gone: %v", err)
	}
	rendered := renderDevPublicLoopConsole(t)
	for _, b := range dashboardBrokers {
		t.Run(b.name, func(t *testing.T) {
			if es := renderedObject(t, rendered, "ExternalSecret", b.secret); es != nil {
				t.Errorf("ExternalSecret/%s is rendered — a second writer for the Secret the seed owns", b.secret)
			}
		})
	}
}

// TestDevPublicLoopConsoleReadsTheMemoryAdminKeyFromTheSeededSecret — the memory
// platform-admin key reaches the pod through envFrom, optional, from the Secret the
// seed writes.
func TestDevPublicLoopConsoleReadsTheMemoryAdminKeyFromTheSeededSecret(t *testing.T) {
	deploy := renderedObject(t, renderDevPublicLoopConsole(t), "Deployment", "loop-console")
	if deploy == nil {
		t.Fatal("no Deployment loop-console in the render")
	}
	containers, _ := dig(deploy, "spec", "template", "spec", "containers").([]any)
	for _, c := range containers {
		sources, _ := dig(c, "envFrom").([]any)
		for _, src := range sources {
			if dig(src, "secretRef", "name") == "loop-runtime-memory-platform-admin" {
				if dig(src, "secretRef", "optional") != true {
					t.Error("envFrom loop-runtime-memory-platform-admin is not optional — loop-console would not start before the seed ran")
				}
				return
			}
		}
	}
	t.Error("no container reads envFrom secretRef loop-runtime-memory-platform-admin")
}

// containerEnvSecretRef returns valueFrom.secretKeyRef of the named env var of the
// named container, or nil.
func containerEnvSecretRef(deploy map[string]any, container, env string) map[string]any {
	containers, _ := dig(deploy, "spec", "template", "spec", "containers").([]any)
	for _, c := range containers {
		if dig(c, "name") != container {
			continue
		}
		envs, _ := dig(c, "env").([]any)
		for _, e := range envs {
			if dig(e, "name") == env {
				ref, _ := dig(e, "valueFrom", "secretKeyRef").(map[string]any)
				return ref
			}
		}
	}
	return nil
}
