// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loopruntime-cloud on dev-public reaches three backends through configuration only: the
// Prometheus the cost aggregator queries, and the loopruntime-rag and loopruntime-lens brokers
// behind the dashboard. Each has already been lost the same way — a variable set
// by hand with `kubectl set env`, gone at the next `helmfile sync` — so the wiring
// is locked on what Helmfile renders.
//
// WHAT THESE DO NOT PROVE. A render is not a deployment: measured live on
// 2026-09-05, with every cost-aggregator assertion below passing, the running
// loopruntime-cloud Deployment had ZERO PROM|COST variables (created 2026-08-10, never
// re-synced), `select count(*) from cost_counters` returned 0, and two of the three
// series the aggregator queries (loopruntime_egress_bytes_total, loopruntime_docr_pulls_total) are
// emitted by no repository. The render is link 1 of a four-link chain (overlay →
// deploy → aggregator → instrumentation); a guard that named the whole chain while
// observing one link is how B-033 stayed invisible for three weeks after its fix.

// canonicalPrometheusURL is the one Prometheus both overlays name (plan D1 of B-033).
const canonicalPrometheusURL = "http://prometheus-server.monitoring.svc.cluster.local:80"

// TestDevPublicLoopRuntimeCloudPublishesThePrometheusURLTheCostAggregatorQueries — without
// LOOPRUNTIME_PROMETHEUS_URL the aggregator disables itself at boot, cost_counters stays
// empty, and the CostEnforcer in api/ degrades to pass-through: the build, egress
// and pull caps go unenforced (B-033).
func TestDevPublicLoopRuntimeCloudPublishesThePrometheusURLTheCostAggregatorQueries(t *testing.T) {
	got := configMapData(t, renderDevPublicLoopRuntimeCloud(t), "loopruntime-cloud")["LOOPRUNTIME_PROMETHEUS_URL"]
	if got != canonicalPrometheusURL {
		t.Errorf("ConfigMap/loopruntime-cloud LOOPRUNTIME_PROMETHEUS_URL = %v, want %q", got, canonicalPrometheusURL)
	}
}

// TestDevPublicLoopRuntimeCloudAndLoopRuntimeAPIQueryTheSamePrometheus — the loopruntime-api readiness
// poller (moved from loopruntime-app-operator on 2026-09-25) names the same Prometheus in
// its own overlay. Editing one of the two files without the other would pass every
// other check and break at runtime, the class of the original defect.
func TestDevPublicLoopRuntimeCloudAndLoopRuntimeAPIQueryTheSamePrometheus(t *testing.T) {
	cloud := configMapData(t, renderDevPublicLoopRuntimeCloud(t), "loopruntime-cloud")["LOOPRUNTIME_PROMETHEUS_URL"]
	api := dig(helmfileValues(t, "core.helmfile.yaml.gotmpl", "dev-public", "loopruntime-api", helmfileCoreEnv...),
		"dataPlanePoller", "prometheusUrl")
	if api == nil {
		t.Fatal("the dev-public loopruntime-api values carry no dataPlanePoller.prometheusUrl — the parity has nothing to compare")
	}
	if cloud != api {
		t.Errorf("loopruntime-cloud LOOPRUNTIME_PROMETHEUS_URL = %v but loopruntime-api dataPlanePoller.prometheusUrl = %v — "+
			"both overlays must name the same Prometheus", cloud, api)
	}
}

// dashboardBroker is one backend whose admin key the loopruntime-cloud container reads
// through a secretKeyRef.
type dashboardBroker struct {
	name     string // rag, lens
	adminKey string // env var and Secret key
	secret   string // the Secret the seed writes
}

var dashboardBrokers = []dashboardBroker{
	{"rag", "RAG_PLATFORM_ADMIN_KEY", "loopruntime-rag-admin-key"},
	{"lens", "LENS_PLATFORM_ADMIN_KEY", "loopruntime-lens-admin-key"},
}

// TestDevPublicLoopRuntimeCloudReachesTheDashboardBrokers — with the base URL empty the
// broker stays nil and every /v1/dashboard/<name>/* route is skipped (the RAG half
// was delegated brief 2026-07-15; the lens half covers M41 read-only + M44
// management routes). THEMORY_BASE_URL also registers the edge's memory
// forward-auth: unset, /v1/memory/* is authorised by nothing on this cluster (#470).
func TestDevPublicLoopRuntimeCloudReachesTheDashboardBrokers(t *testing.T) {
	data := configMapData(t, renderDevPublicLoopRuntimeCloud(t), "loopruntime-cloud")
	for _, baseURL := range []string{"THEMORY_BASE_URL", "RAG_BASE_URL", "LENS_BASE_URL"} {
		t.Run(baseURL, func(t *testing.T) {
			url, _ := data[baseURL].(string)
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				t.Errorf("ConfigMap/loopruntime-cloud %s = %q, want an http(s):// URL", baseURL, url)
			}
		})
	}
}

func TestDevPublicLoopRuntimeCloudReadsTheBrokerAdminKeysFromTheirSecrets(t *testing.T) {
	rendered := renderDevPublicLoopRuntimeCloud(t)
	deploy := renderedObject(t, rendered, "Deployment", "loopruntime-cloud")
	if deploy == nil {
		t.Fatal("no Deployment loopruntime-cloud in the render")
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

// TestDevPublicLoopRuntimeCloudLeavesTheBrokerAdminKeysToTheSeed — on dev-public the admin
// keys are minted by `loopruntime-ops seed data-plane-admin-keys`, which writes the Secrets the
// Deployment reads. The ExternalSecrets that used to target the same Secrets never
// synced here: they read an OpenBao property no step writes, through a
// ClusterSecretStore this environment does not run (measured 2026-09-30:
// SecretSyncedError on both, no ClusterSecretStore on the core cluster). Rendering them
// would give each Secret a second writer, owned by ESO, that could overwrite the seed's
// key the day a store appears (#470). The template stays for installations that do
// hold the keys in OpenBao.
func TestDevPublicLoopRuntimeCloudLeavesTheBrokerAdminKeysToTheSeed(t *testing.T) {
	template := filepath.Join(repoRoot(t), "charts/loopruntime-cloud/templates/externalsecret-admin-keys.yaml")
	if _, err := os.Stat(template); err != nil {
		t.Fatalf("the ExternalSecret template is gone: %v", err)
	}
	rendered := renderDevPublicLoopRuntimeCloud(t)
	for _, b := range dashboardBrokers {
		t.Run(b.name, func(t *testing.T) {
			if es := renderedObject(t, rendered, "ExternalSecret", b.secret); es != nil {
				t.Errorf("ExternalSecret/%s is rendered — a second writer for the Secret the seed owns", b.secret)
			}
		})
	}
}

// TestDevPublicLoopRuntimeCloudReadsTheMemoryAdminKeyFromTheSeededSecret — the memory
// platform-admin key reaches the pod through envFrom, optional, from the Secret the
// seed writes.
func TestDevPublicLoopRuntimeCloudReadsTheMemoryAdminKeyFromTheSeededSecret(t *testing.T) {
	deploy := renderedObject(t, renderDevPublicLoopRuntimeCloud(t), "Deployment", "loopruntime-cloud")
	if deploy == nil {
		t.Fatal("no Deployment loopruntime-cloud in the render")
	}
	containers, _ := dig(deploy, "spec", "template", "spec", "containers").([]any)
	for _, c := range containers {
		sources, _ := dig(c, "envFrom").([]any)
		for _, src := range sources {
			if dig(src, "secretRef", "name") == "loopruntime-memory-platform-admin" {
				if dig(src, "secretRef", "optional") != true {
					t.Error("envFrom loopruntime-memory-platform-admin is not optional — loopruntime-cloud would not start before the seed ran")
				}
				return
			}
		}
	}
	t.Error("no container reads envFrom secretRef loopruntime-memory-platform-admin")
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
