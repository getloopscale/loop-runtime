// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The Grafana the helmfiles install shows the repository's dashboards: the core helmfile
// installs charts/loopruntime-monitoring, one ConfigMap per infra/helm/monitoring/dashboards
// file labelled grafana_dashboard=1, and Grafana's dashboard sidecar loads every ConfigMap
// with that label. Before this, values-grafana.yaml had no sidecar and no helmfile
// installed the chart, so staging's Grafana started with no dashboard at all.

func TestCoreHelmfileInstallsEveryRepositoryDashboard(t *testing.T) {
	files, err := filepath.Glob("../../helm/monitoring/dashboards/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no dashboards found: %v", err)
	}
	for _, env := range []string{"staging", "prod"} {
		rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", env, "loopruntime-monitoring", "charts/loopruntime-monitoring", stagingOutputs...)
		dashboards := map[string]bool{}
		decoder := yaml.NewDecoder(strings.NewReader(rendered))
		for {
			var doc map[string]any
			if decoder.Decode(&doc) != nil {
				break
			}
			switch doc["kind"] {
			case "ConfigMap":
				if dig(doc, "metadata", "labels", "grafana_dashboard") != "1" {
					continue
				}
				for key := range doc["data"].(map[string]any) {
					dashboards[key] = true
				}
			case "PrometheusRule":
				t.Errorf("%s: loopruntime-monitoring renders a PrometheusRule; the helmfile Prometheus reads its rules from values-prometheus.yaml", env)
			}
		}
		for _, file := range files {
			if !dashboards[filepath.Base(file)] {
				t.Errorf("%s: no grafana_dashboard ConfigMap carries %s", env, filepath.Base(file))
			}
		}
	}
}

func TestGrafanaLoadsTheLabelledDashboardsWithTheirDatasources(t *testing.T) {
	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", "staging", "grafana", stagingOutputs...)
	if dig(values, "sidecar", "dashboards", "enabled") != true || dig(values, "sidecar", "dashboards", "label") != "grafana_dashboard" || dig(values, "sidecar", "dashboards", "labelValue") != "1" {
		t.Errorf("Grafana's dashboard sidecar does not load grafana_dashboard=1 ConfigMaps: %v", dig(values, "sidecar"))
	}
	// The dashboards name their datasources by uid.
	uids := map[string]string{}
	list, _ := dig(values, "datasources", "datasources.yaml", "datasources").([]any)
	for _, item := range list {
		ds := item.(map[string]any)
		uid, _ := ds["uid"].(string)
		uids[ds["type"].(string)] = uid
	}
	for kind, want := range map[string]string{"prometheus": "prometheus", "loki": "loki", "tempo": "tempo"} {
		if uids[kind] != want {
			t.Errorf("the %s datasource has uid %q, want %q", kind, uids[kind], want)
		}
	}
}

// Provisioned dashboards get no import dialog, so a ${DS_X} reference resolves only through
// a datasource templating variable of that name; an export's __inputs block is ignored.
func TestEveryDatasourceVariableADashboardUsesIsDeclared(t *testing.T) {
	files, err := filepath.Glob("../../helm/monitoring/dashboards/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no dashboards found: %v", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var dashboard struct {
			Templating struct {
				List []struct {
					Name string `json:"name"`
					Type string `json:"type"`
				} `json:"list"`
			} `json:"templating"`
		}
		if err := json.Unmarshal(data, &dashboard); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		declared := map[string]bool{}
		for _, v := range dashboard.Templating.List {
			if v.Type == "datasource" {
				declared[v.Name] = true
			}
		}
		for _, name := range []string{"DS_PROMETHEUS", "DS_LOKI"} {
			if strings.Contains(string(data), "${"+name+"}") && !declared[name] {
				t.Errorf("%s uses ${%s} but declares no datasource variable %s", filepath.Base(file), name, name)
			}
		}
	}
}
