// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The project Status tab and the readiness badges read four signals per tenant app:
// ready replicas, request rate, error rate and latency. A tenant app is an Argo Rollout
// that LoopScale does not instrument, so no series described it
// (getloopscale/loop-runtime#107). Each cell now builds them: kube-state-metrics exports the
// loopzero.io labels of pods and Services, the cell's Prometheus records one series per
// app from pod readiness and from the ingress, and the core Prometheus federates those.

// tenantAppLabels are the labels every recorded series carries, and tenantKubeLabels the
// Kubernetes labels they come from.
var (
	tenantAppLabels  = []string{"tenant_id", "project", "app", "environment"}
	tenantKubeLabels = []string{"loopzero.io/tenant", "loopzero.io/project", "loopzero.io/app-name", "loopzero.io/environment"}
)

// tenantAppSeries are the series the API queries
// (api/internal/projectmetrics/projectmetricshttp/metrics_proxy.go).
var tenantAppSeries = []string{
	"loopzero_app:kube_pod_status_ready:sum",
	"loopzero_app:traefik_service_requests:rate5m",
	"loopzero_app_status_class:traefik_service_requests:rate5m",
	"loopzero_app_le:traefik_service_request_duration_seconds_bucket:rate5m",
}

type recordingRule struct {
	Record string `yaml:"record"`
	Expr   string `yaml:"expr"`
}

// cellRecordingRules returns the recording rules file of the cell's Prometheus as the
// chart will mount it, and its rules.
func cellRecordingRules(t *testing.T) ([]byte, []recordingRule) {
	t.Helper()
	values := helmfileValues(t, "cell.helmfile.yaml.gotmpl", "staging", "prometheus-federation", stagingOutputs...)
	file := dig(values, "serverFiles", "recording_rules.yml")
	raw, err := yaml.Marshal(file)
	if err != nil {
		t.Fatalf("encode serverFiles.recording_rules.yml: %v", err)
	}
	var decoded struct {
		Groups []struct {
			Rules []recordingRule `yaml:"rules"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode serverFiles.recording_rules.yml: %v\n%s", err, raw)
	}
	var rules []recordingRule
	for _, group := range decoded.Groups {
		rules = append(rules, group.Rules...)
	}
	return raw, rules
}

func TestCellKubeStateMetricsExportsTheTenantLabelsOfPodsAndServices(t *testing.T) {
	values := helmfileValues(t, "cell.helmfile.yaml.gotmpl", "staging", "kube-state-metrics", stagingOutputs...)
	allowlist, _ := values["metricLabelsAllowlist"].([]any)
	for _, resource := range []string{"pods", "services"} {
		var entry string
		for _, item := range allowlist {
			if text, _ := item.(string); strings.HasPrefix(text, resource+"=[") {
				entry = text
			}
		}
		if entry == "" {
			t.Errorf("metricLabelsAllowlist has no entry for %s: %v", resource, allowlist)
			continue
		}
		listed := strings.Split(strings.TrimSuffix(strings.TrimPrefix(entry, resource+"=["), "]"), ",")
		for _, label := range tenantKubeLabels {
			found := false
			for _, l := range listed {
				found = found || l == label
			}
			if !found {
				t.Errorf("%s does not export the %s label: %s", resource, label, entry)
			}
		}
	}
}

var byClause = regexp.MustCompile(`by \(([^)]*)\)`)

func TestCellPrometheusRecordsTheTenantAppSeries(t *testing.T) {
	_, rules := cellRecordingRules(t)
	byName := map[string]recordingRule{}
	for _, rule := range rules {
		byName[rule.Record] = rule
	}
	for _, name := range tenantAppSeries {
		if _, ok := byName[name]; !ok {
			t.Errorf("the cell's Prometheus does not record %s (records %d rules)", name, len(rules))
		}
	}
	for _, rule := range rules {
		clause := byClause.FindStringSubmatch(rule.Expr)
		if clause == nil {
			t.Errorf("%s: the expression aggregates by nothing, so its labels are not stable:\n%s", rule.Record, rule.Expr)
			continue
		}
		kept := map[string]bool{}
		for _, label := range strings.Split(clause[1], ",") {
			kept[strings.TrimSpace(label)] = true
		}
		for _, label := range tenantAppLabels {
			if !kept[label] {
				t.Errorf("%s: the outer aggregation drops %s: by (%s)", rule.Record, label, clause[1])
			}
		}
	}
}

// The build cluster runs no tenant app; the rules are the cells'.
func TestBuildPrometheusRecordsNoTenantAppSeries(t *testing.T) {
	values := helmfileValues(t, "build.helmfile.yaml.gotmpl", "staging", "prometheus-federation", stagingOutputs...)
	if groups := dig(values, "serverFiles", "recording_rules.yml", "groups"); groups != nil {
		t.Errorf("the build cluster's Prometheus carries recording rules: %v", groups)
	}
}

// A recorded series has no job label, so the `{job=~".+"}` selector alone leaves it on
// the cell. The federation jobs select the recorded names too, beside it.
func TestCorePrometheusFederatesTheRecordedTenantAppSeries(t *testing.T) {
	_, rules := cellRecordingRules(t)
	federated := 0
	for _, job := range coreScrapeJobs(t, "staging", stagingMonitoringOutputs()...) {
		name, _ := job["job_name"].(string)
		if !strings.HasSuffix(name, "-federation") {
			continue
		}
		federated++
		match, _ := dig(job, "params", "match[]").([]any)
		var byRecordedName *regexp.Regexp
		keepsScraped := false
		for _, m := range match {
			selector, _ := m.(string)
			if selector == `{job=~".+"}` {
				keepsScraped = true
			}
			if sub := regexp.MustCompile(`^\{__name__=~"(.+)"\}$`).FindStringSubmatch(selector); sub != nil {
				byRecordedName = regexp.MustCompile("^(?:" + sub[1] + ")$")
			}
		}
		if !keepsScraped {
			t.Errorf(`%s: match[] = %v no longer selects the scraped series with {job=~".+"}`, name, match)
		}
		if byRecordedName == nil {
			t.Errorf("%s: match[] = %v selects no series by name, so the recorded series stay on the cell", name, match)
			continue
		}
		for _, series := range tenantAppSeries {
			if !byRecordedName.MatchString(series) {
				t.Errorf("%s: %v does not select %s", name, byRecordedName, series)
			}
		}
		for _, rule := range rules {
			wanted := strings.HasPrefix(rule.Record, "loopzero_app")
			if byRecordedName.MatchString(rule.Record) != wanted {
				t.Errorf("%s: %v selecting %s = %v, want %v (only the per-app series leave the cell)",
					name, byRecordedName, rule.Record, !wanted, wanted)
			}
		}
	}
	if federated != 3 {
		t.Errorf("found %d federation jobs, want one for each of build, cell-0 and cell-1", federated)
	}
}

// promtool loads the rules as Prometheus will, and evaluates them over synthetic series
// shaped like a cell's (testdata/tenant_app_recording_rules_promtool.yaml): the joins
// give each app its four labels, sum the stable and canary Services and the ingress
// replicas, and leave out workloads that are not tenant apps.
func TestTenantAppRecordingRulesPassPromtool(t *testing.T) {
	promtool, err := exec.LookPath("promtool")
	if err != nil {
		t.Skip("promtool is not on PATH (https://prometheus.io/download)")
	}
	rules, _ := cellRecordingRules(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rules.yml"), rules, 0o600); err != nil {
		t.Fatal(err)
	}
	cases, err := os.ReadFile("testdata/tenant_app_recording_rules_promtool.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cases.yml"), cases, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"check", "rules", "rules.yml"}, {"test", "rules", "cases.yml"}} {
		cmd := exec.Command(promtool, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("promtool %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}
