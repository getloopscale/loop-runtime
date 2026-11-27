//go:build helm_render

// Package loopruntimemonitoring_test — render-time tests for loop-runtime-monitoring
// (Plan 2026-05-20 v1.1 T3.1 chart 8/10).
//
// loop-runtime-monitoring is a bundle of Prometheus operator artifacts, read from
// infra/helm/monitoring through the chart's `rules` and `dashboards` symlinks:
//   - 9 PrometheusRule manifests (capacity, critical, database, domains,
//     otel-pipeline, probes, sigstore, tracing, warning)
//   - 26 ConfigMap dashboards consumed by Grafana sidecar (label
//     grafana_dashboard=1) — every dashboard in infra/helm/monitoring/dashboards
package loopruntimemonitoring_test

import (
	"encoding/json"
	"testing"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
	"github.com/stretchr/testify/require"
	coreV1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

const (
	chartName        = "loop-runtime-monitoring"
	releaseName      = "loop-runtime-monitoring-test"
	expectedRules    = 9  // count locked: the 9 rule files prometheusrules.yaml publishes
	expectedDashCMs  = 26 // count locked: the 26 dashboards in infra/helm/monitoring/dashboards
	grafanaDiscovery = "grafana_dashboard"
)

// rule schema (minimal — full PrometheusRule type lives in
// prometheus-operator API which we do not want to vendor for tests).
type promRuleGroup struct {
	Name  string `json:"name"`
	Rules []struct {
		Alert       string            `json:"alert,omitempty"`
		Record      string            `json:"record,omitempty"`
		Expr        string            `json:"expr"`
		For         string            `json:"for,omitempty"`
		Labels      map[string]string `json:"labels,omitempty"`
		Annotations map[string]string `json:"annotations,omitempty"`
	} `json:"rules"`
}

type promRule struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Labels    map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Groups []promRuleGroup `json:"groups"`
	} `json:"spec"`
}

func renderAllRules(t *testing.T) []promRule {
	t.Helper()
	chartPath, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, chartPath, releaseName, nil)

	docs := chartrender.SplitYAMLByKind(out, "PrometheusRule")
	rules := make([]promRule, 0, len(docs))
	for _, d := range docs {
		var r promRule
		require.NoError(t, yaml.Unmarshal([]byte(d), &r))
		rules = append(rules, r)
	}
	return rules
}

func renderAllConfigMaps(t *testing.T) []coreV1.ConfigMap {
	t.Helper()
	chartPath, err := chartrender.ChartPath(chartName)
	require.NoError(t, err)
	opts, err := chartrender.NewOptions(chartPath, "")
	require.NoError(t, err)
	out := chartrender.RenderTemplate(t, opts, chartPath, releaseName, nil)

	docs := chartrender.SplitYAMLByKind(out, "ConfigMap")
	cms := make([]coreV1.ConfigMap, 0, len(docs))
	for _, d := range docs {
		var cm coreV1.ConfigMap
		require.NoError(t, yaml.Unmarshal([]byte(d), &cm))
		cms = append(cms, cm)
	}
	return cms
}

func TestMonitoring_PrometheusRuleCount(t *testing.T) {
	t.Parallel()
	rules := renderAllRules(t)
	require.Equalf(t, expectedRules, len(rules),
		"PrometheusRule count drifted — want %d (locked), got %d. "+
			"If intentional, update expectedRules + log in CHANGELOG.md",
		expectedRules, len(rules))
}

func TestMonitoring_AllRulesUseMonitoringV1(t *testing.T) {
	t.Parallel()
	for _, r := range renderAllRules(t) {
		require.Equalf(t, "monitoring.coreos.com/v1", r.APIVersion,
			"PromRule %q must use monitoring.coreos.com/v1", r.Metadata.Name)
	}
}

func TestMonitoring_AllRulesHaveReleaseLabel(t *testing.T) {
	t.Parallel()
	// Prometheus operator's serviceMonitorSelector defaults to selecting
	// resources labeled `release=<helm release name>`. Without this label,
	// the PrometheusRule will be silently ignored by the operator.
	for _, r := range renderAllRules(t) {
		require.NotEmptyf(t, r.Metadata.Labels["release"],
			"PromRule %q missing 'release' label — Prometheus operator "+
				"will silently ignore it (no alerts fire)", r.Metadata.Name)
	}
}

func TestMonitoring_AllRulesNonEmpty(t *testing.T) {
	t.Parallel()
	for _, r := range renderAllRules(t) {
		require.NotEmptyf(t, r.Spec.Groups,
			"PromRule %q has zero groups", r.Metadata.Name)
		for gi, g := range r.Spec.Groups {
			require.NotEmptyf(t, g.Rules,
				"PromRule %q group[%d] %q has zero rules",
				r.Metadata.Name, gi, g.Name)
		}
	}
}

func TestMonitoring_AllAlertsHaveSeverity(t *testing.T) {
	t.Parallel()
	for _, r := range renderAllRules(t) {
		for _, g := range r.Spec.Groups {
			for _, rule := range g.Rules {
				if rule.Alert == "" {
					continue // recording rule, no severity required
				}
				require.NotEmptyf(t, rule.Labels["severity"],
					"Alert %q (rule %q) missing severity label",
					rule.Alert, r.Metadata.Name)
				require.Containsf(t,
					[]string{"critical", "warning", "info"},
					rule.Labels["severity"],
					"Alert %q severity %q must be critical|warning|info",
					rule.Alert, rule.Labels["severity"])
			}
		}
	}
}

func TestMonitoring_AllAlertsHaveSummary(t *testing.T) {
	t.Parallel()
	for _, r := range renderAllRules(t) {
		for _, g := range r.Spec.Groups {
			for _, rule := range g.Rules {
				if rule.Alert == "" {
					continue
				}
				require.NotEmptyf(t, rule.Annotations["summary"],
					"Alert %q missing summary annotation (oncall needs it "+
						"to know what fired)", rule.Alert)
			}
		}
	}
}

func TestMonitoring_DashboardConfigMapCount(t *testing.T) {
	t.Parallel()
	dashCount := 0
	for _, cm := range renderAllConfigMaps(t) {
		if cm.Labels[grafanaDiscovery] == "1" {
			dashCount++
		}
	}
	require.Equalf(t, expectedDashCMs, dashCount,
		"Grafana dashboard ConfigMap count drifted — want %d, got %d. "+
			"Sidecar discovery uses label %s=1.",
		expectedDashCMs, dashCount, grafanaDiscovery)
}

func TestMonitoring_AllDashboardsHaveGrafanaDiscoveryLabel(t *testing.T) {
	t.Parallel()
	for _, cm := range renderAllConfigMaps(t) {
		// Only validate ConfigMaps whose name starts with the dashboard
		// prefix; non-dashboard ConfigMaps (if any) are exempt.
		// (Currently loop-runtime-monitoring emits only dashboard ConfigMaps, but
		// this guard future-proofs.)
		if len(cm.Name) < len("loop-console-dashboard-") ||
			cm.Name[:len("loop-console-dashboard-")] != "loop-console-dashboard-" {
			continue
		}
		require.Equalf(t, "1", cm.Labels[grafanaDiscovery],
			"Dashboard CM %q missing grafana_dashboard=1 label — Grafana "+
				"sidecar will NOT load it", cm.Name)
	}
}

func TestMonitoring_AllDashboardsHaveJSONPayload(t *testing.T) {
	t.Parallel()
	for _, cm := range renderAllConfigMaps(t) {
		if cm.Labels[grafanaDiscovery] != "1" {
			continue
		}
		require.NotEmptyf(t, cm.Data,
			"Dashboard CM %q has empty data — Grafana would show blank",
			cm.Name)
		for fname, body := range cm.Data {
			require.NotEmptyf(t, body,
				"Dashboard CM %q file %q empty", cm.Name, fname)
			var parsed map[string]interface{}
			require.NoErrorf(t, json.Unmarshal([]byte(body), &parsed),
				"Dashboard CM %q file %q has invalid JSON (Grafana would "+
					"silently fail to load)", cm.Name, fname)
			require.NotEmptyf(t, parsed["title"],
				"Dashboard CM %q file %q missing title", cm.Name, fname)
		}
	}
}
