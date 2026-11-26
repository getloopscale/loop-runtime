// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Dashboards and alert rules have ONE source: infra/helm/monitoring. The published
// charts/loopruntime-monitoring reaches them through symlinks that `helm package` resolves.
//
// Measured 2026-09-23, before this: all 21 published dashboards differed from their
// infra/helm twins, and 10 PrometheusRules existed in both trees — the published side
// still carried 16 alerts on metrics nothing emits, removed from the L2 side months
// earlier, and one rule queried a metric name the operators do not export.
func TestMonitoring_PublishedChartReadsTheInfraArtifacts(t *testing.T) {
	for link, want := range map[string]string{
		"../../../charts/loopruntime-monitoring/dashboards": "../../infra/helm/monitoring/dashboards",
		"../../../charts/loopruntime-monitoring/rules":      "../../infra/helm/monitoring/manifests",
	} {
		got, err := os.Readlink(link)
		if err != nil {
			t.Errorf("%s must be a symlink to %s (a copy drifts): %v", link, want, err)
			continue
		}
		if got != want {
			t.Errorf("%s points at %s, want %s", link, got, want)
		}
	}
}

func TestMonitoring_NoDashboardUIDDefinedTwice(t *testing.T) {
	files, err := filepath.Glob("../../helm/monitoring/dashboards/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no dashboards found (%v) — a check over nothing proves nothing", err)
	}
	seen := map[string]string{}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var d struct {
			UID string `json:"uid"`
		}
		if err := json.Unmarshal(body, &d); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		if d.UID == "" {
			t.Errorf("%s has no uid — Grafana would mint one per install", f)
			continue
		}
		if prev, dup := seen[d.UID]; dup {
			t.Errorf("dashboard uid %q defined twice: %s and %s", d.UID, prev, f)
		}
		seen[d.UID] = f
	}
}

// An alert's identity is its name plus its labels: Sloth emits the same burn-rate
// alert twice on purpose, once per severity (page and ticket windows). Two rules
// with the same name AND severity are the duplicate that silently doubles a page.
func TestMonitoring_NoAlertNameDefinedTwice(t *testing.T) {
	files, err := filepath.Glob("../../helm/monitoring/manifests/prometheusrule-*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no rule files found (%v) — a check over nothing proves nothing", err)
	}
	seen := map[string]string{}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		dec := yaml.NewDecoder(strings.NewReader(string(body)))
		for {
			var doc struct {
				Spec struct {
					Groups []struct {
						Rules []struct {
							Alert  string            `yaml:"alert"`
							Labels map[string]string `yaml:"labels"`
						} `yaml:"rules"`
					} `yaml:"groups"`
				} `yaml:"spec"`
			}
			if err := dec.Decode(&doc); err != nil {
				if err.Error() == "EOF" {
					break
				}
				t.Fatalf("parse %s: %v", f, err)
			}
			for _, g := range doc.Spec.Groups {
				for _, r := range g.Rules {
					if r.Alert == "" {
						continue
					}
					key := r.Alert + "/severity=" + r.Labels["severity"]
					if prev, dup := seen[key]; dup {
						t.Errorf("alert %q defined twice: %s and %s", key, prev, f)
					}
					seen[key] = f
				}
			}
		}
	}
}
