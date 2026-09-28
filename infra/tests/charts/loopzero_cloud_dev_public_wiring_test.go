// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loopzero-cloud on dev-public reaches three backends through configuration only: the
// Prometheus the cost aggregator queries, and the loopzero-rag and loopzero-lens brokers
// behind the dashboard. Each has already been lost the same way — a variable set
// by hand with `kubectl set env`, gone at the next `helmfile sync` — so the wiring
// is locked on what Helmfile renders.
//
// WHAT THESE DO NOT PROVE. A render is not a deployment: measured live on
// 2026-09-05, with every cost-aggregator assertion below passing, the running
// loopzero-cloud Deployment had ZERO PROM|COST variables (created 2026-08-10, never
// re-synced), `select count(*) from cost_counters` returned 0, and two of the three
// series the aggregator queries (loopzero_egress_bytes_total, loopzero_docr_pulls_total) are
// emitted by no repository. The render is link 1 of a four-link chain (overlay →
// deploy → aggregator → instrumentation); a guard that named the whole chain while
// observing one link is how B-033 stayed invisible for three weeks after its fix.

// canonicalPrometheusURL is the one Prometheus both overlays name (plan D1 of B-033).
const canonicalPrometheusURL = "http://prometheus-server.monitoring.svc.cluster.local:80"

// TestDevPublicLoopZeroCloudPublishesThePrometheusURLTheCostAggregatorQueries — without
// LOOPZERO_PROMETHEUS_URL the aggregator disables itself at boot, cost_counters stays
// empty, and the CostEnforcer in api/ degrades to pass-through: the build, egress
// and pull caps go unenforced (B-033).
func TestDevPublicLoopZeroCloudPublishesThePrometheusURLTheCostAggregatorQueries(t *testing.T) {
	got := configMapData(t, renderDevPublicLoopZeroCloud(t), "loopzero-cloud")["LOOPZERO_PROMETHEUS_URL"]
	if got != canonicalPrometheusURL {
		t.Errorf("ConfigMap/loopzero-cloud LOOPZERO_PROMETHEUS_URL = %v, want %q", got, canonicalPrometheusURL)
	}
}

// TestDevPublicLoopZeroCloudAndLoopZeroAPIQueryTheSamePrometheus — the loopzero-api readiness
// poller (moved from loopzero-app-operator on 2026-09-25) names the same Prometheus in
// its own overlay. Editing one of the two files without the other would pass every
// other check and break at runtime, the class of the original defect.
func TestDevPublicLoopZeroCloudAndLoopZeroAPIQueryTheSamePrometheus(t *testing.T) {
	cloud := configMapData(t, renderDevPublicLoopZeroCloud(t), "loopzero-cloud")["LOOPZERO_PROMETHEUS_URL"]
	api := dig(helmfileValues(t, "core.helmfile.yaml.gotmpl", "dev-public", "loopzero-api", helmfileCoreEnv...),
		"dataPlanePoller", "prometheusUrl")
	if api == nil {
		t.Fatal("the dev-public loopzero-api values carry no dataPlanePoller.prometheusUrl — the parity has nothing to compare")
	}
	if cloud != api {
		t.Errorf("loopzero-cloud LOOPZERO_PROMETHEUS_URL = %v but loopzero-api dataPlanePoller.prometheusUrl = %v — "+
			"both overlays must name the same Prometheus", cloud, api)
	}
}

// dashboardBroker is one backend the loopzero-cloud dashboard reaches through a broker:
// a base URL in the ConfigMap, and an admin key delivered by an ExternalSecret.
type dashboardBroker struct {
	name      string // rag, lens
	baseURL   string // ConfigMap key
	adminKey  string // env var and Secret key
	secret    string // target Secret and ExternalSecret name
	vaultProp string // property under loopzero/cloud
}

var dashboardBrokers = []dashboardBroker{
	{"rag", "RAG_BASE_URL", "RAG_PLATFORM_ADMIN_KEY", "loopzero-rag-admin-key", "rag_platform_admin_key"},
	{"lens", "LENS_BASE_URL", "LENS_PLATFORM_ADMIN_KEY", "loopzero-lens-admin-key", "lens_platform_admin_key"},
}

// TestDevPublicLoopZeroCloudReachesTheDashboardBrokers — with the base URL empty the
// broker stays nil and every /v1/dashboard/<name>/* route is skipped (the RAG half
// was delegated brief 2026-07-15; the lens half covers M41 read-only + M44
// management routes).
func TestDevPublicLoopZeroCloudReachesTheDashboardBrokers(t *testing.T) {
	data := configMapData(t, renderDevPublicLoopZeroCloud(t), "loopzero-cloud")
	for _, b := range dashboardBrokers {
		t.Run(b.name, func(t *testing.T) {
			url, _ := data[b.baseURL].(string)
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				t.Errorf("ConfigMap/loopzero-cloud %s = %q, want an http(s):// URL", b.baseURL, url)
			}
		})
	}
}

// TestDevPublicLoopZeroCloudReachesThePipelineRAGContainer — a URL that answers is not
// proof of the right target. Until 2026-07-29 RAG_BASE_URL named :18082, a `node`
// process started by hand on the host, while the container CI publishes served
// :8787; the endpoint answered 200 and the only symptom was an empty version
// column, because the loose process reports built_at "unknown".
func TestDevPublicLoopZeroCloudReachesThePipelineRAGContainer(t *testing.T) {
	url, _ := configMapData(t, renderDevPublicLoopZeroCloud(t), "loopzero-cloud")["RAG_BASE_URL"].(string)
	if strings.Contains(url, ":18082") {
		t.Errorf("RAG_BASE_URL = %q points at :18082, the loose host process; the pipeline container serves :8787 "+
			"(see infra/helmfile/values/dev-public/loopzero-cloud.yaml.gotmpl)", url)
	}
}

func TestDevPublicLoopZeroCloudReadsTheBrokerAdminKeysFromTheirSecrets(t *testing.T) {
	rendered := renderDevPublicLoopZeroCloud(t)
	deploy := renderedObject(t, rendered, "Deployment", "loopzero-cloud")
	if deploy == nil {
		t.Fatal("no Deployment loopzero-cloud in the render")
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

// TestDevPublicLoopZeroCloudRendersTheBrokerAdminKeyExternalSecrets — the Secrets the
// Deployment reads are materialised from OpenBao at loopzero/cloud, and owned by their
// ExternalSecret so deleting it does not orphan the Secret.
func TestDevPublicLoopZeroCloudRendersTheBrokerAdminKeyExternalSecrets(t *testing.T) {
	template := filepath.Join(repoRoot(t), "charts/loopzero-cloud/templates/externalsecret-admin-keys.yaml")
	if _, err := os.Stat(template); err != nil {
		t.Fatalf("the ExternalSecret template is gone: %v", err)
	}
	rendered := renderDevPublicLoopZeroCloud(t)
	for _, b := range dashboardBrokers {
		t.Run(b.name, func(t *testing.T) {
			es := renderedObject(t, rendered, "ExternalSecret", b.secret)
			if es == nil {
				t.Fatalf("ExternalSecret/%s is not rendered (%s.externalSecretEnabled off?)", b.secret, b.name)
			}
			if got := dig(es, "spec", "target", "name"); got != b.secret {
				t.Errorf("target.name = %v, want %s", got, b.secret)
			}
			if got := dig(es, "spec", "target", "creationPolicy"); got != "Owner" {
				t.Errorf("target.creationPolicy = %v, want Owner", got)
			}
			ref := externalSecretRemoteRef(es, b.adminKey)
			if ref == nil || ref["key"] != "loopzero/cloud" || ref["property"] != b.vaultProp {
				t.Errorf("remoteRef for %s = %v, want key=loopzero/cloud property=%s", b.adminKey, ref, b.vaultProp)
			}
		})
	}
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

// externalSecretRemoteRef returns the remoteRef of the data entry for secretKey, or nil.
func externalSecretRemoteRef(es map[string]any, secretKey string) map[string]any {
	data, _ := dig(es, "spec", "data").([]any)
	for _, d := range data {
		if dig(d, "secretKey") == secretKey {
			ref, _ := dig(d, "remoteRef").(map[string]any)
			return ref
		}
	}
	return nil
}
