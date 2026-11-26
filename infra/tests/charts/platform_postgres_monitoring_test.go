// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The platform's PostgreSQL is watched by the core Prometheus: a scrape job for the
// instances' metrics port, and alert rules for what loses data or stops writes when
// nobody is looking. The rules are one PrometheusRule file, which an environment's
// Prometheus loads into its alerting rules.

const platformPostgresRulesFile = "infra/helm/monitoring/manifests/prometheusrule-platform-postgres.yaml"

type alertRule struct {
	Alert       string            `yaml:"alert"`
	Expr        string            `yaml:"expr"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

func platformPostgresAlerts(t *testing.T) map[string]alertRule {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), platformPostgresRulesFile))
	if err != nil {
		t.Fatalf("read the platform-postgres rules: %v", err)
	}
	var doc struct {
		Spec struct {
			Groups []struct {
				Rules []alertRule `yaml:"rules"`
			} `yaml:"groups"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse the platform-postgres rules: %v", err)
	}
	alerts := map[string]alertRule{}
	for _, g := range doc.Spec.Groups {
		for _, r := range g.Rules {
			alerts[r.Alert] = r
		}
	}
	return alerts
}

// Each alert names the metric it reads. The names are the ones an instance of
// CloudNativePG 1.31 with the Barman Cloud plugin exposes on :9187, read on staging on
// 2026-11-26: a rule over a metric nothing emits never fires.
func TestPlatformPostgresAlertsCoverDataLossAndWriteStops(t *testing.T) {
	alerts := platformPostgresAlerts(t)
	for name, metric := range map[string]string{
		"LoopRuntimePlatformPostgresDown":                 `up{job="platform-postgres"}`,
		"LoopRuntimePlatformPostgresWALArchiveFailing":    "cnpg_pg_stat_archiver_failed_count",
		"LoopRuntimePlatformPostgresWALArchiveBacklog":    `cnpg_collector_pg_wal_archive_status{value="ready"`,
		"LoopRuntimePlatformPostgresBackupStale":          "barman_cloud_cloudnative_pg_io_last_available_backup_timestamp",
		"LoopRuntimePlatformPostgresBackupFailed":         "barman_cloud_cloudnative_pg_io_last_failed_backup_timestamp",
		"LoopRuntimePlatformPostgresVolumeFilling":        "kubelet_volume_stats_available_bytes",
		"LoopRuntimePlatformPostgresReplicaLagging":       "cnpg_pg_replication_lag",
		"LoopRuntimePlatformPostgresSyncReplicaMissing":   `cnpg_collector_sync_replicas{value="observed"`,
		"LoopRuntimePlatformPostgresTransactionIDAgeHigh": "cnpg_pg_database_xid_age",
		"LoopRuntimePlatformPostgresConnectionsNearLimit": "cnpg_backends_total",
	} {
		rule, ok := alerts[name]
		if !ok {
			t.Errorf("no alert %s", name)
			continue
		}
		if !strings.Contains(rule.Expr, metric) {
			t.Errorf("%s does not read %s: %s", name, metric, rule.Expr)
		}
		if s := rule.Labels["severity"]; s != "critical" && s != "warning" {
			t.Errorf("%s has severity %q", name, s)
		}
		if rule.Annotations["summary"] == "" || !strings.HasPrefix(rule.Annotations["runbook_url"], "docs/runbooks.md#") {
			t.Errorf("%s lacks a summary or a runbook in docs/runbooks.md: %v", name, rule.Annotations)
		}
	}
	// CloudNativePG 1.31 counts every attached standby as observed, so observed can
	// exceed expected: only fewer than expected is a fault.
	if expr := alerts["LoopRuntimePlatformPostgresSyncReplicaMissing"].Expr; strings.Contains(expr, "!=") {
		t.Errorf("SyncReplicaMissing compares with !=, which fires when observed exceeds expected: %s", expr)
	}
}

func TestPlatformPostgresAlertsHaveARunbookSection(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "runbooks.md"))
	if err != nil {
		t.Fatal(err)
	}
	runbooks := string(raw)
	if !strings.Contains(runbooks, "\n## Platform PostgreSQL alerts\n") {
		t.Fatal("docs/runbooks.md has no section \"Platform PostgreSQL alerts\"")
	}
	for name, rule := range platformPostgresAlerts(t) {
		if rule.Annotations["runbook_url"] != "docs/runbooks.md#platform-postgresql-alerts" {
			t.Errorf("%s points at %q", name, rule.Annotations["runbook_url"])
		}
		if !strings.Contains(runbooks, "`"+name+"`") {
			t.Errorf("the runbook section does not name %s", name)
		}
	}
}

func TestStagingCorePrometheusScrapesThePlatformPostgres(t *testing.T) {
	var job map[string]any
	for _, j := range coreScrapeJobs(t, "staging", stagingOutputs...) {
		if j["job_name"] == "platform-postgres" {
			job = j
		}
	}
	if job == nil {
		t.Fatal("the staging core Prometheus has no platform-postgres job")
	}
	raw, err := yaml.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"role: pod",
		"- loopruntime-data-plane", // where the cluster runs
		"__meta_kubernetes_pod_label_cnpg_io_cluster", // every instance of every cluster there
		"__meta_kubernetes_pod_container_port_name",   // the metrics port, not PostgreSQL's
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the platform-postgres job lacks %q:\n%s", want, raw)
		}
	}
}

func TestDevCorePrometheusHasNoPlatformPostgresJob(t *testing.T) {
	for _, j := range coreScrapeJobs(t, "dev") {
		if j["job_name"] == "platform-postgres" {
			t.Error("dev scrapes a platform-postgres it does not install")
		}
	}
}

func TestStagingCorePrometheusLoadsThePlatformPostgresAlerts(t *testing.T) {
	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", "staging", "prometheus", stagingOutputs...)
	groups, _ := dig(values, "serverFiles", "alerting_rules.yml", "groups").([]any)
	loaded := map[string]bool{}
	for _, g := range groups {
		rules, _ := dig(g.(map[string]any), "rules").([]any)
		for _, r := range rules {
			name, _ := r.(map[string]any)["alert"].(string)
			loaded[name] = true
		}
	}
	for name := range platformPostgresAlerts(t) {
		if !loaded[name] {
			t.Errorf("the staging core Prometheus does not load %s", name)
		}
	}
	if len(loaded) == 0 {
		t.Error("the staging core Prometheus loads no alert rule")
	}
}

// The data plane's pods carry no runtime.getloopscale.com label, so no other Promtail job tails them:
// PostgreSQL's log, the operator's and the services' reach Loki through platform-pods.
func TestStagingPromtailCollectsTheDataPlaneAndItsDatabaseOperator(t *testing.T) {
	job := promtailJobs(t, "core.helmfile.yaml.gotmpl", "staging", stagingMonitoringOutputs())["platform-pods"]
	if job == nil {
		t.Fatal("no platform-pods job on the core cluster")
	}
	names, _ := dig(job, "kubernetes_sd_configs", 0, "namespaces", "names").([]any)
	for _, want := range []string{"loopruntime-data-plane", "cnpg-system"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Errorf("platform-pods does not discover namespace %s (names %v)", want, names)
		}
	}
}
