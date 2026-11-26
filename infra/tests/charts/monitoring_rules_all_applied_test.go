// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SD-01.13 (system-design audit 2026-09-07). infra/helm/monitoring/manifests/
// held 19 PrometheusRule files with 110 alerts, and the only thing that applied
// any of them named exactly two — critical and warning, 36 alerts. The other 17
// were written, reviewed, committed and loaded by nothing: capacity, database,
// domains, multi-cell, otel-pipeline, probes, registry, sigstore, tracing,
// vault-audit, velero, pulumi-state, service-auth-ed25519, chaos,
// dogfood-slo-gate, operator-cutover, deep-review-remediation.
//
// The defect is not the 17 files. It is that adding a rule file did not add an
// alert, and nothing said so. This test asserts the class: every
// prometheusrule-*.yaml on disk must be reachable by the install task, so a file
// added tomorrow cannot go orphaned in silence.
func TestMonitoringRules_EveryRuleFileIsApplied(t *testing.T) {
	const manifestDir = "../../helm/monitoring/manifests"
	const taskfile = "../../../taskfiles/infra.yml"

	files, err := filepath.Glob(filepath.Join(manifestDir, "prometheusrule-*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no prometheusrule files found under %s (err=%v)", manifestDir, err)
	}

	raw, err := os.ReadFile(taskfile)
	if err != nil {
		t.Fatalf("read %s: %v", taskfile, err)
	}
	task := string(raw)

	// A glob over the directory covers every file, present and future, and is
	// the only form that fixes the class rather than the 17 instances.
	globbed := strings.Contains(task, "prometheusrule-*.yaml")

	var orphaned []string
	for _, f := range files {
		name := filepath.Base(f)
		if !globbed && !strings.Contains(task, name) {
			orphaned = append(orphaned, name)
		}
	}

	if len(orphaned) > 0 {
		t.Errorf("%d of %d PrometheusRule files are applied by nothing:\n  %s\n\n"+
			"Apply the directory with a glob rather than naming files — naming them is how "+
			"17 files came to be committed and never loaded.",
			len(orphaned), len(files), strings.Join(orphaned, "\n  "))
	}
}

// SRE-10.1 (same audit). infra/sloth/ holds seven SLO specs whose objectives
// match the declared targets exactly, and Sloth generates from them the
// multiwindow burn-rate rules the production canary runbook tells an operator to
// watch. The generated file was absent from the repository, its generator was a
// manual task gated on a CLI the setup never installs, and it was not in the
// apply list — so `loop-runtime:slo:deploy_burn_rate_5m`, named at
// docs/operations/production-canary-stage4-runbook.md:50, was defined nowhere.
//
// An operator waiting on an alert that cannot fire reads the silence as budget
// intact. The generated rules must be committed and applied like any other.
func TestMonitoringRules_BurnRateRulesExistAndAreApplied(t *testing.T) {
	const generated = "../../helm/monitoring/manifests/prometheusrule-slo-generated.yaml"

	body, err := os.ReadFile(generated)
	if err != nil {
		t.Fatalf("burn-rate rules not committed at %s: %v\n\n"+
			"infra/sloth/ declares the SLOs; without the generated rules the runbook "+
			"points at alerts no rule defines.\n\n"+
			"TO FIX: run `task sloth:generate` (the generator was repaired on 2026-09-07 — it\n"+
			"read only the first spec of seven, and emitted raw rule groups kubectl cannot apply).\n"+
			"The write lands under infra/helm/, which the governance gate protects, so it needs\n"+
			"the path added to the Approved Scope in .claude/governance-gate.local.md first.\n"+
			"This test is red on purpose until that happens: it is the finding, not a flake.",
			generated, err)
	}

	content := string(body)

	// This asserted a `loop-runtime:slo` prefix in its first form, taken from the
	// runbook. That prefix was ALWAYS fiction: no rule ever emitted it, which is
	// the finding. Sloth names recording rules `slo:*` and alerts after the spec,
	// and the runbook was corrected the same day to name what actually exists.
	// Asserting the real names is aligning the test with reality, not lowering it.
	if !strings.Contains(content, "kind: PrometheusRule") {
		t.Error("generated file is not a PrometheusRule; kubectl cannot apply raw rule groups")
	}
	if !strings.Contains(content, "slo:current_burn_rate:ratio") {
		t.Error("generated rules contain no burn-rate recording rule")
	}

	// SRE-10.2: the point is a fast page AND a slow ticket, not one threshold.
	// Sloth emits each SLO's alert twice, once per window.
	if n := strings.Count(content, "- alert:"); n < 14 {
		t.Errorf("only %d alerts generated; 7 SLO specs should each yield a page and a ticket (14)", n)
	}
}
