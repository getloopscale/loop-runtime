// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The core Prometheus labels the series it federates from build and the cells with
// `cluster`, but its own series come from the chart's default scrape jobs and carry no
// such label (getloopscale/loopzero#41). A panel that groups by cluster names core
// through label_replace, so core shows as "core" instead of a blank series.
func TestMonitoring_PanelsGroupedByClusterNameTheCoreCluster(t *testing.T) {
	files, err := filepath.Glob("../../helm/monitoring/dashboards/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no dashboards found (%v)", err)
	}
	byCluster := regexp.MustCompile(`by\s*\([^)]*\bcluster\b[^)]*\)`)
	namesCore := `label_replace(`
	checked := 0
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var d struct {
			Panels []struct {
				Title   string `json:"title"`
				Targets []struct {
					Expr string `json:"expr"`
				} `json:"targets"`
			} `json:"panels"`
		}
		if err := json.Unmarshal(body, &d); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, p := range d.Panels {
			for _, tg := range p.Targets {
				if !byCluster.MatchString(tg.Expr) {
					continue
				}
				checked++
				if !strings.HasPrefix(strings.TrimSpace(tg.Expr), namesCore) ||
					!strings.Contains(tg.Expr, `"cluster", "core", "cluster", ""`) {
					t.Errorf("%s, panel %q groups by cluster without naming core: %s", filepath.Base(f), p.Title, tg.Expr)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no panel groups by cluster: the check covers nothing")
	}
}
