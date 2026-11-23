// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Staging's monitoring collects from all four clusters, the way production is laid out:
// one Loki on the build cluster and one Prometheus on core, each reached from the other
// clusters through an internal ALB the AWS Load Balancer Controller creates for an
// Ingress. `task staging:platform` reads each ALB's DNSName from its Ingress status and
// exports it before the syncs that need it (infra/staging/monitoring-addresses.sh).

const (
	stagingLokiALB    = "internal-k8s-monitori-lokiinte-0123456789.us-east-1.elb.amazonaws.com"
	stagingBuildALB   = "internal-k8s-monitori-promethe-1111111111.us-east-1.elb.amazonaws.com"
	stagingCell0ALB   = "internal-k8s-monitori-promethe-2222222222.us-east-1.elb.amazonaws.com"
	stagingCell1ALB   = "internal-k8s-monitori-promethe-3333333333.us-east-1.elb.amazonaws.com"
	stagingEmulatorLB = "https://172.29.0.1:9597/_aws/elasticloadbalancing/"
)

// stagingMonitoringOutputs are the infra-live outputs plus the two monitoring addresses
// the platform task exports once the build cluster and the cells are synced.
func stagingMonitoringOutputs(extra ...string) []string {
	env := append([]string{}, stagingOutputs...)
	env = append(env,
		"LOOPZERO_LOKI_ALB_DNS_NAME="+stagingLokiALB,
		"LOOPZERO_METRICS_ALB_DNS_NAMES=build="+stagingBuildALB+",cell-0="+stagingCell0ALB+",cell-1="+stagingCell1ALB,
	)
	return append(env, extra...)
}

func promtailClient(t *testing.T, helmfile string, env []string) map[string]any {
	t.Helper()
	values := helmfileValues(t, helmfile, "staging", "promtail", env...)
	clients, _ := dig(values, "config", "clients").([]any)
	if len(clients) != 1 {
		t.Fatalf("%s promtail: config.clients = %v, want exactly one client", helmfile, clients)
	}
	client, _ := clients[0].(map[string]any)
	return client
}

// The cells and core have no Loki of their own. Their Promtail pushes to the build
// cluster's Loki through its internal ALB, which pods reach at the emulator's endpoint
// with the ALB named in the path, and labels every stream with the cluster it came from.
func TestStagingPromtailPushesToTheBuildLokiThroughItsALB(t *testing.T) {
	want := stagingEmulatorLB + stagingLokiALB + "/loki/api/v1/push"
	cases := []struct {
		helmfile, cluster string
		env               []string
	}{
		{"core.helmfile.yaml.gotmpl", "core", stagingMonitoringOutputs()},
		{"cell.helmfile.yaml.gotmpl", "cell-0", stagingMonitoringOutputs()},
		{"cell.helmfile.yaml.gotmpl", "cell-1", stagingMonitoringOutputs("LOOPZERO_CELL_CONTEXT=cell-1")},
	}
	for _, c := range cases {
		t.Run(c.cluster, func(t *testing.T) {
			client := promtailClient(t, c.helmfile, c.env)
			if got := client["url"]; got != want {
				t.Errorf("push url = %v, want %s", got, want)
			}
			if got := dig(client, "external_labels", "cluster"); got != c.cluster {
				t.Errorf("external_labels.cluster = %v, want %s", got, c.cluster)
			}
		})
	}
}

// The build cluster's Promtail runs next to the Loki and pushes to it in-cluster: its sync
// is the one that creates the ALB, so the ALB's name is not known yet when it renders.
func TestStagingBuildPromtailPushesInClusterLabelledBuild(t *testing.T) {
	client := promtailClient(t, "build.helmfile.yaml.gotmpl", stagingOutputs)
	if got := client["url"]; got != "http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push" {
		t.Errorf("push url = %v, want the in-cluster Loki Service", got)
	}
	if got := dig(client, "external_labels", "cluster"); got != "build" {
		t.Errorf("external_labels.cluster = %v, want build", got)
	}
}

// Core runs a Promtail on staging; before, its logs reached no Loki at all.
func TestStagingCoreInstallsPromtail(t *testing.T) {
	installed := stagingInstalledReleases(t, "core.helmfile.yaml.gotmpl")
	if !installed["monitoring/promtail"] {
		t.Error("monitoring/promtail is not installed on the staging core cluster")
	}
}

// albIngress returns the Ingress of the given name among a release's extra objects (a
// list of objects, or of YAML strings as the prometheus chart's extraManifests takes).
func albIngress(t *testing.T, objects []any, name string) map[string]any {
	t.Helper()
	for _, object := range objects {
		if text, ok := object.(string); ok {
			var decoded map[string]any
			if err := yaml.Unmarshal([]byte(text), &decoded); err != nil {
				t.Fatalf("decode extra manifest: %v\n%s", err, text)
			}
			object = decoded
		}
		if o, ok := object.(map[string]any); ok && o["kind"] == "Ingress" && dig(o, "metadata", "name") == name {
			return o
		}
	}
	t.Fatalf("no Ingress %s among %v", name, objects)
	return nil
}

func assertInternalInstanceALB(t *testing.T, ingress map[string]any) {
	t.Helper()
	annotations, _ := dig(ingress, "metadata", "annotations").(map[string]any)
	for key, want := range map[string]string{
		"alb.ingress.kubernetes.io/scheme":      "internal",
		"alb.ingress.kubernetes.io/target-type": "instance",
	} {
		if got := annotations[key]; got != want {
			t.Errorf("%s = %v, want %s", key, got, want)
		}
	}
	if got := dig(ingress, "spec", "ingressClassName"); got != "alb" {
		t.Errorf("ingressClassName = %v, want alb", got)
	}
}

// Loki is published through an internal ALB with instance targets, so the Service it
// forwards to is a NodePort.
func TestStagingLokiIsPublishedThroughAnInternalALB(t *testing.T) {
	values := helmfileValues(t, "build.helmfile.yaml.gotmpl", "staging", "loki", stagingOutputs...)
	objects, _ := values["extraObjects"].([]any)
	ingress := albIngress(t, objects, "loki-internal")
	assertInternalInstanceALB(t, ingress)
	backend := dig(ingress, "spec", "rules", 0, "http", "paths", 0, "backend", "service", "name")
	var service map[string]any
	for _, object := range objects {
		if o, ok := object.(map[string]any); ok && o["kind"] == "Service" && dig(o, "metadata", "name") == backend {
			service = o
		}
	}
	if service == nil {
		t.Fatalf("the Ingress forwards to Service %v, which the release does not render", backend)
	}
	if got := dig(service, "spec", "type"); got != "NodePort" {
		t.Errorf("Service %v type = %v, want NodePort for instance targets", backend, got)
	}
}

// Each remote cluster runs a Prometheus of its own that scrapes it (kube-state-metrics,
// node-exporter, the kubelets and cAdvisor) and that core federates, published through
// an internal ALB.
func TestStagingRemoteClustersPublishAFederationPrometheus(t *testing.T) {
	for _, helmfile := range []string{"build.helmfile.yaml.gotmpl", "cell.helmfile.yaml.gotmpl"} {
		t.Run(helmfile, func(t *testing.T) {
			values := helmfileValues(t, helmfile, "staging", "prometheus-federation", stagingOutputs...)
			if got := dig(values, "server", "service", "type"); got != "NodePort" {
				t.Errorf("server.service.type = %v, want NodePort for instance targets", got)
			}
			objects, _ := values["extraManifests"].([]any)
			assertInternalInstanceALB(t, albIngress(t, objects, "prometheus-federation"))
		})
	}
}

// coreScrapeJobs evaluates the core Prometheus' extraScrapeConfigs the way the chart
// does (`tpl` over the release values) and decodes the jobs. The template uses only the
// builtins text/template and Helm share.
func coreScrapeJobs(t *testing.T, env string, extraEnv ...string) []map[string]any {
	t.Helper()
	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", env, "prometheus", extraEnv...)
	text, ok := values["extraScrapeConfigs"].(string)
	if !ok {
		t.Fatal("the core prometheus values carry no extraScrapeConfigs string")
	}
	tmpl, err := template.New("extraScrapeConfigs").Parse(text)
	if err != nil {
		t.Fatalf("parse extraScrapeConfigs as a template: %v", err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, map[string]any{"Values": values}); err != nil {
		t.Fatalf("evaluate extraScrapeConfigs: %v", err)
	}
	var jobs []map[string]any
	if err := yaml.Unmarshal(out.Bytes(), &jobs); err != nil {
		t.Fatalf("decode the scrape jobs: %v\n%s", err, out.String())
	}
	return jobs
}

func staticTargets(job map[string]any) []string {
	var targets []string
	configs, _ := job["static_configs"].([]any)
	for _, c := range configs {
		list, _ := dig(c, "targets").([]any)
		for _, target := range list {
			targets = append(targets, target.(string))
		}
	}
	return targets
}

// Core's Prometheus federates each remote cluster through that cluster's ALB, one job
// per cluster, every series labelled with the cluster it came from.
func TestStagingCorePrometheusFederatesEveryRemoteCluster(t *testing.T) {
	jobs := coreScrapeJobs(t, "staging", stagingMonitoringOutputs()...)
	want := map[string]string{"build": stagingBuildALB, "cell-0": stagingCell0ALB, "cell-1": stagingCell1ALB}
	for cluster, alb := range want {
		var job map[string]any
		for _, j := range jobs {
			if dig(j, "static_configs", 0, "labels", "cluster") == cluster {
				job = j
			}
		}
		if job == nil {
			t.Errorf("no scrape job labels its target cluster=%s", cluster)
			continue
		}
		if got := job["metrics_path"]; got != "/_aws/elasticloadbalancing/"+alb+"/federate" {
			t.Errorf("%s: metrics_path = %v, want the federate endpoint behind %s", cluster, got, alb)
		}
		if got := job["scheme"]; got != "https" {
			t.Errorf("%s: scheme = %v, want https (the emulator's endpoint)", cluster, got)
		}
		if got := staticTargets(job); len(got) != 1 || got[0] != "172.29.0.1:9597" {
			t.Errorf("%s: targets = %v, want [172.29.0.1:9597]", cluster, got)
		}
		if job["honor_labels"] != true {
			t.Errorf("%s: honor_labels = %v, want true so federated series keep their job and instance", cluster, job["honor_labels"])
		}
		match, _ := dig(job, "params", "match[]").([]any)
		if len(match) == 0 {
			t.Errorf("%s: params match[] is empty; /federate returns nothing without a selector", cluster)
		}
	}
}

// The k3d cell's kube-state-metrics NodePort is a dev topology; on staging it can only
// fail (`lookup k3d-loopzero-cell-0-server-0 ... server misbehaving`). It is not rendered,
// also on the first core sync of a rebuild, before the remote ALBs exist.
func TestStagingCorePrometheusHasNoK3dTarget(t *testing.T) {
	for name, env := range map[string][]string{
		"addresses known":    stagingMonitoringOutputs(),
		"before the remotes": stagingOutputs,
	} {
		t.Run(name, func(t *testing.T) {
			for _, job := range coreScrapeJobs(t, "staging", env...) {
				for _, target := range staticTargets(job) {
					if strings.Contains(target, "k3d") {
						t.Errorf("job %v scrapes %s, a k3d node", job["job_name"], target)
					}
				}
			}
		})
	}
}

// dev keeps the k3d cell's kube-state-metrics job.
func TestDevCorePrometheusKeepsTheK3dCellTarget(t *testing.T) {
	for _, job := range coreScrapeJobs(t, "dev") {
		if job["job_name"] == "cell-0-cluster-kube-state-metrics" {
			if got := staticTargets(job); len(got) != 1 || got[0] != "k3d-loopzero-cell-0-server-0:30030" {
				t.Errorf("targets = %v, want [k3d-loopzero-cell-0-server-0:30030]", got)
			}
			return
		}
	}
	t.Error("dev renders no cell-0-cluster-kube-state-metrics job")
}

// Without cert-manager the AWS Load Balancer Controller chart generates a new webhook CA
// and certificate on every upgrade unless keepTLSSecret is set, and the running
// controller serves the old certificate until it reloads the Secret. An Ingress the same
// sync creates in that window is refused (`failed calling webhook "vingress.elbv2.k8s.aws"
// ... x509: certificate signed by unknown authority`), which failed the staging deploy of
// c8309c0 at the loki-internal Ingress.
func TestStagingLoadBalancerControllerKeepsItsWebhookCertificate(t *testing.T) {
	for _, helmfile := range []string{"core.helmfile.yaml.gotmpl", "build.helmfile.yaml.gotmpl", "cell.helmfile.yaml.gotmpl"} {
		t.Run(helmfile, func(t *testing.T) {
			values := helmfileValues(t, helmfile, "staging", "aws-load-balancer-controller", stagingOutputs...)
			if values["enableCertManager"] == true {
				return
			}
			if got := values["keepTLSSecret"]; got != true {
				t.Errorf("keepTLSSecret = %v without cert-manager, want true", got)
			}
		})
	}
}

func promtailJobs(t *testing.T, helmfile, env string, extraEnv []string) map[string]map[string]any {
	t.Helper()
	values := helmfileValues(t, helmfile, env, "promtail", extraEnv...)
	snippet, _ := dig(values, "config", "snippets", "scrapeConfigs").(string)
	var jobs []map[string]any
	if err := yaml.Unmarshal([]byte(snippet), &jobs); err != nil {
		t.Fatalf("decode the scrapeConfigs snippet: %v", err)
	}
	byName := map[string]map[string]any{}
	for _, j := range jobs {
		byName[j["job_name"].(string)] = j
	}
	return byName
}

// The other jobs select workload pods (Argo workflows, tenants, loopzero components),
// which an idle cell or build cluster does not run: on 2026-11-23 the Promtail of build,
// cell-0 and cell-1 pushed through the ALB with 0 active targets. Each cluster's platform
// pods (ingress, controllers, monitoring) are collected too, without the pods the other
// jobs already tail.
func TestStagingPromtailCollectsEachClustersPlatformPods(t *testing.T) {
	cases := []struct{ helmfile, cluster string }{
		{"core.helmfile.yaml.gotmpl", "core"},
		{"build.helmfile.yaml.gotmpl", "build"},
		{"cell.helmfile.yaml.gotmpl", "cell-0"},
	}
	for _, c := range cases {
		t.Run(c.cluster, func(t *testing.T) {
			job := promtailJobs(t, c.helmfile, "staging", stagingMonitoringOutputs())["platform-pods"]
			if job == nil {
				t.Fatal("no platform-pods job")
			}
			names, _ := dig(job, "kubernetes_sd_configs", 0, "namespaces", "names").([]any)
			for _, want := range []string{"kube-system", "monitoring", "traefik"} {
				found := false
				for _, n := range names {
					found = found || n == want
				}
				if !found {
					t.Errorf("platform-pods does not discover namespace %s (names %v)", want, names)
				}
			}
			dropped := map[string]bool{}
			relabels, _ := job["relabel_configs"].([]any)
			for _, r := range relabels {
				if dig(r, "action") == "drop" {
					sources, _ := dig(r, "source_labels").([]any)
					for _, s := range sources {
						dropped[s.(string)] = true
					}
				}
			}
			for _, label := range []string{
				"__meta_kubernetes_pod_label_workflows_argoproj_io_workflow",
				"__meta_kubernetes_pod_label_loopzero_io_component",
			} {
				if !dropped[label] {
					t.Errorf("platform-pods does not drop pods by %s, which another job already tails", label)
				}
			}
		})
	}
}

func TestDevPromtailHasNoPlatformPodsJob(t *testing.T) {
	if promtailJobs(t, "build.helmfile.yaml.gotmpl", "dev", nil)["platform-pods"] != nil {
		t.Error("dev renders a platform-pods job; it is staging's")
	}
}

// A DNSName that carries anything but DNS name characters (a backslash an interpreter
// left in, a quote) fails the core render instead of ending up in a scrape path.
func TestStagingCorePrometheusRefusesAMalformedALBName(t *testing.T) {
	cmd := exec.Command("helmfile", "-f", "infra/helmfile/core.helmfile.yaml.gotmpl", "-e", "staging",
		"--selector", "name=prometheus", "--skip-deps", "--output-file-template", filepath.Join(t.TempDir(), "{{ .Release.Name }}.yaml"), "write-values")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), stagingOutputs...)
	cmd.Env = append(cmd.Env, `LOOPZERO_METRICS_ALB_DNS_NAMES=build=`+stagingBuildALB+`\,cell-0=`+stagingCell0ALB+`\,cell-1=`+stagingCell1ALB)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("the core prometheus rendered with backslashes in LOOPZERO_METRICS_ALB_DNS_NAMES")
	}
	if !strings.Contains(string(out), "is not <cluster>=<DNSName>") {
		t.Errorf("render failed without naming the malformed pair:\n%s", truncate(string(out), 800))
	}
}
