// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The OpenBao audit trail (T8.1 + T8.3 of plan 2026-06-02-openbao-adoption): Promtail
// scrapes the audit log out of the secrets-system namespace and tags it
// loop_runtime_component=vault-audit, and the openbao-audit dashboard reads it by that tag.
//
// The scrape is read from the values Helmfile hands the chart, not from the gotmpl,
// so what is asserted is what Promtail receives.

// vaultAuditJob returns the vault-audit scrape job from the build Promtail's merged
// values, decoded from the scrapeConfigs snippet.
func vaultAuditJob(t *testing.T) map[string]any {
	t.Helper()
	values := helmfileValues(t, "build.helmfile.yaml.gotmpl", "dev", "promtail")
	snippet, ok := dig(values, "config", "snippets", "scrapeConfigs").(string)
	if !ok {
		t.Fatalf("promtail values carry no config.snippets.scrapeConfigs string")
	}
	var jobs []map[string]any
	if err := yaml.Unmarshal([]byte(snippet), &jobs); err != nil {
		t.Fatalf("decode the scrapeConfigs snippet: %v", err)
	}
	for _, j := range jobs {
		if j["job_name"] == "vault-audit" {
			return j
		}
	}
	t.Fatal("no scrape job named vault-audit — the OpenBao audit log reaches no Loki")
	return nil
}

// TestPromtailScrapesTheAuditLogOnlyFromSecretsSystem — pod discovery restricted to
// exactly one namespace. Without the filter the job tails every pod's logs into the
// audit stream (a privacy and a volume problem); with more than one, the audit
// stream carries logs that are not audit. On a cluster with no secrets-system (a
// cleanroom core) the same filter discovers nothing, which Promtail accepts.
func TestPromtailScrapesTheAuditLogOnlyFromSecretsSystem(t *testing.T) {
	job := vaultAuditJob(t)
	if role := dig(job, "kubernetes_sd_configs", 0, "role"); role != "pod" {
		t.Errorf("kubernetes_sd_configs role = %v, want pod — the audit log is a pod log", role)
	}
	names, _ := dig(job, "kubernetes_sd_configs", 0, "namespaces", "names").([]any)
	if !slices.Equal(names, []any{"secrets-system"}) {
		t.Errorf("namespace filter = %v, want exactly [secrets-system]", names)
	}
}

func TestPromtailExtractsTheAuditFields(t *testing.T) {
	job := vaultAuditJob(t)
	var expressions map[string]any
	stages, _ := job["pipeline_stages"].([]any)
	for _, s := range stages {
		if e, ok := dig(s, "json", "expressions").(map[string]any); ok {
			expressions = e
		}
	}
	for field, source := range map[string]string{"request_path": "request.path", "error": "error"} {
		if expressions[field] != source {
			t.Errorf("json stage extracts %s from %v, want %q", field, expressions[field], source)
		}
	}
}

// TestPromtailTagsTheAuditStreamForTheDashboard — the dashboard and the two P1
// alerts filter on loop_runtime_component=vault-audit; without the label they match nothing.
func TestPromtailTagsTheAuditStreamForTheDashboard(t *testing.T) {
	relabels, _ := vaultAuditJob(t)["relabel_configs"].([]any)
	for _, r := range relabels {
		if dig(r, "target_label") == "loop_runtime_component" && dig(r, "replacement") == "vault-audit" {
			return
		}
	}
	t.Errorf("no relabel sets loop_runtime_component=vault-audit; relabel_configs: %v", relabels)
}

func TestOpenBaoAuditDashboardCarriesItsPanelsAndDatasources(t *testing.T) {
	path := filepath.Join(repoRoot(t), "infra/helm/monitoring/dashboards/openbao-audit.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the dashboard: %v", err)
	}
	var dashboard map[string]any
	if err := json.Unmarshal(b, &dashboard); err != nil {
		t.Fatalf("%s is not JSON: %v", path, err)
	}
	if dashboard["uid"] != "openbao-audit" {
		t.Errorf("uid = %v, want openbao-audit", dashboard["uid"])
	}

	var titles []string
	datasources := map[string]bool{}
	walkJSON(dashboard, func(key string, value any) {
		if key == "title" {
			if s, ok := value.(string); ok {
				titles = append(titles, s)
			}
		}
		if key == "datasource" {
			if kind, ok := dig(value, "type").(string); ok {
				datasources[kind] = true
			}
		}
	})
	for _, prefix := range []string{
		"Auth Request Rate (5m)",
		"Policy Denials by Path (P1 alert source)",
		"Vault Seal Status (P1 alert source)",
		"Audit Log Entry Timestamp Lag (P1 alert source)",
		"Top Accessors (last 1h)",
		"Vault Audit Log (Loki",
		"Denial Heatmap (15m bins)",
	} {
		if !slices.ContainsFunc(titles, func(s string) bool { return strings.HasPrefix(s, prefix) }) {
			t.Errorf("no panel titled %q…", prefix)
		}
	}
	for _, kind := range []string{"prometheus", "loki"} {
		if !datasources[kind] {
			t.Errorf("no panel reads a %s datasource", kind)
		}
	}
}

// walkJSON calls visit for every key/value pair in a decoded JSON tree.
func walkJSON(node any, visit func(key string, value any)) {
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			visit(k, v)
			walkJSON(v, visit)
		}
	case []any:
		for _, v := range n {
			walkJSON(v, visit)
		}
	}
}
