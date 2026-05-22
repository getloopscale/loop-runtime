// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package alertrunbooks implements the T5.8 gate: every PrometheusRule alert
// must carry a runbook_url (or legacy runbook:) annotation so Alertmanager can
// link operators to recovery docs.
//
// Plan T5.3 of docs/plans/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces infra/scripts/check_alert_runbooks.sh (which embedded a python3
// heredoc). Pure-Go (file read + regexp) — no external CLI. (The "helm" in the
// rules-dir path is a directory name, not a helm invocation.)
package alertrunbooks

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultRulesDir holds the PrometheusRule manifests, relative to repo root.
const DefaultRulesDir = "infra/helm/monitoring/manifests"

// runbookLookahead is the per-alert-block window scanned for a runbook
// annotation (matches the bash `blk[:1500]`).
const runbookLookahead = 1500

var (
	alertSplitRe = regexp.MustCompile(`\n\s*- alert:`)
	alertNameRe  = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)`)
)

// DefaultExceptionList are pre-T5.8-baseline alerts permitted without a runbook.
var DefaultExceptionList = []string{
	"PulumiOrphanStackDetected", "GiteaAuthLatencyHigh", "ArgoWorkflowQuotaExceeded",
	"HPAAtMaxReplicas", "PVCPendingSustained", "TenantReconcileBacklog",
	"BuildFailureRateHigh", "LoopZeroQueueSlow", "LoopZeroDeployFailureRate",
	"LoopZeroLockContention", "LoopZeroSyncSlow", "LoopZeroBuildQueueSlow",
	"LoopZeroTempoStorageHigh", "LoopZeroCollectorExportErrors",
}

// Config drives the gate.
type Config struct {
	Root          string
	RulesDir      string   // default DefaultRulesDir (relative to Root)
	ExceptionList []string // default DefaultExceptionList
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	Violations []string // "<file>:<alertName>"
}

// Check scans PrometheusRule YAMLs for alerts missing a runbook annotation.
func Check(cfg Config) *Outcome {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	rulesDir := cfg.RulesDir
	if rulesDir == "" {
		rulesDir = DefaultRulesDir
	}
	exc := cfg.ExceptionList
	if exc == nil {
		exc = DefaultExceptionList
	}
	excepted := make(map[string]struct{}, len(exc))
	for _, e := range exc {
		excepted[e] = struct{}{}
	}

	out := &Outcome{Pass: true}
	dir := filepath.Join(root, rulesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out // missing rules dir → nothing to check
	}
	for _, e := range entries {
		// bash filter: filename contains "prometheusrule" AND ends ".yaml".
		if e.IsDir() || !strings.Contains(e.Name(), "prometheusrule") || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			continue
		}
		for _, name := range missingRunbookAlerts(string(data), excepted) {
			out.Violations = append(out.Violations, e.Name()+":"+name)
		}
	}
	if len(out.Violations) > 0 {
		out.Pass = false
	}
	return out
}

// missingRunbookAlerts returns alert names in content lacking a runbook
// annotation and not in the exception set.
func missingRunbookAlerts(content string, excepted map[string]struct{}) []string {
	var missing []string
	blocks := alertSplitRe.Split(content, -1)
	for _, blk := range blocks[1:] { // [0] is the pre-first-alert preamble
		m := alertNameRe.FindStringSubmatch(blk)
		if m == nil {
			continue
		}
		name := m[1]
		body := blk
		if len(body) > runbookLookahead {
			body = body[:runbookLookahead]
		}
		if strings.Contains(body, "runbook_url") || strings.Contains(body, "runbook:") {
			continue
		}
		if _, ok := excepted[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	return missing
}
