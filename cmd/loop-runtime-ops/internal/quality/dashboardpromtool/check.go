// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package dashboardpromtool validates Grafana dashboards in two passes:
//  1. Basic JSON syntax (ALWAYS runs) — catches accidental commits of
//     broken dashboards.
//  2. PromQL semantic validation via promtool (SKIP if binary missing) —
//     verifies each panel query parses against Prometheus rules grammar.
//
// T4.3 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md.
package dashboardpromtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// Config tells Check which dashboard tree to scan.
type Config struct {
	// DashboardsDir is the directory containing dashboard JSON files
	// (e.g., infra/helm/monitoring/dashboards/).
	DashboardsDir string
}

// Outcome is the gate result.
type Outcome struct {
	// TotalScanned is the count of .json files found.
	TotalScanned int
	// PromtoolSkipped=true when promtool binary not in PATH.
	PromtoolSkipped bool
	// PromtoolReason explains the skip when applicable.
	PromtoolReason string
	// SyntaxErrors are JSON-parse failures (always-fatal).
	SyntaxErrors []string
	// PromtoolErrors are PromQL parse failures (when promtool present).
	PromtoolErrors []string
	// Pass=true if zero syntax errors AND zero promtool errors
	// (skipped promtool is acceptable — does not fail Pass).
	Pass bool
}

// Verdict is Fail on any JSON syntax or PromQL parse error. A run without
// promtool is still a Pass on the syntax it checked; its report names the
// skipped half.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Check scans every .json in DashboardsDir.
//   - Pass 1: json.Unmarshal each file (ALWAYS).
//   - Pass 2: if promtool present, extract PromQL targets from each
//     panel + run `promtool check rules` against synthesized rules file.
//     If promtool absent: PromtoolSkipped=true, no failure.
func Check(ctx context.Context, cfg Config) (*Outcome, error) {
	out := &Outcome{}
	if cfg.DashboardsDir == "" {
		return out, fmt.Errorf("DashboardsDir is required")
	}

	files, err := listJSONFiles(cfg.DashboardsDir)
	if err != nil {
		return nil, err
	}
	out.TotalScanned = len(files)

	// Pass 1: JSON syntax (always runs)
	for _, f := range files {
		// #nosec G304 -- f comes from filepath.Walk of fixed cfg.DashboardsDir
		data, rErr := os.ReadFile(f)
		if rErr != nil {
			out.SyntaxErrors = append(out.SyntaxErrors,
				fmt.Sprintf("%s: read: %v", f, rErr))
			continue
		}
		var v any
		if jErr := json.Unmarshal(data, &v); jErr != nil {
			out.SyntaxErrors = append(out.SyntaxErrors,
				fmt.Sprintf("%s: invalid JSON: %v", f, jErr))
		}
	}

	// Pass 2: PromQL semantic (skip-on-missing)
	if _, err := exec.LookPath("promtool"); err != nil {
		out.PromtoolSkipped = true
		out.PromtoolReason = "promtool binary not in PATH; install via https://prometheus.io/download"
	} else {
		for _, f := range files {
			queries, qErr := extractPromQLQueries(f)
			if qErr != nil {
				// JSON parse already reported above
				continue
			}
			for _, q := range queries {
				if pErr := validateWithPromtool(ctx, q); pErr != nil {
					out.PromtoolErrors = append(out.PromtoolErrors,
						fmt.Sprintf("%s: query %q: %v", filepath.Base(f), truncate(q, 80), pErr))
				}
			}
		}
	}

	out.Pass = len(out.SyntaxErrors) == 0 && len(out.PromtoolErrors) == 0
	return out, nil
}

func listJSONFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".json") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// extractPromQLQueries pulls .panels[].targets[].expr fields from a Grafana
// dashboard JSON. Grafana stores panel queries as `expr` string in target
// objects. This is best-effort — dashboards using legacy `targets[].rawQuery`
// or non-Prometheus datasources are skipped silently.
func extractPromQLQueries(file string) ([]string, error) {
	// #nosec G304
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var dashboard struct {
		Panels []struct {
			Targets []struct {
				Expr       string `json:"expr"`
				Datasource any    `json:"datasource"`
			} `json:"targets"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(data, &dashboard); err != nil {
		return nil, err
	}
	var queries []string
	for _, p := range dashboard.Panels {
		for _, t := range p.Targets {
			if t.Expr != "" {
				queries = append(queries, t.Expr)
			}
		}
	}
	return queries, nil
}

// validateWithPromtool writes a synthetic rules file with the query as a
// recording rule and runs `promtool check rules` against it. Returns nil
// if the query parses cleanly; error otherwise.
func validateWithPromtool(ctx context.Context, query string) (err error) {
	tmp, err := os.CreateTemp("", "promtool-query-*.yaml")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.Remove(tmp.Name())) }()

	// Synthesize minimal rules group with this query.
	rulesYAML := fmt.Sprintf(`groups:
- name: dashboard-query-check
  rules:
  - record: dashboard:query:check
    expr: |
      %s
`, query)
	if _, err := tmp.WriteString(rulesYAML); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "promtool", "check", "rules", tmp.Name())
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("promtool: %w: %s", runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
