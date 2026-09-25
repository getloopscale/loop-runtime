// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/alertrunbooks"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/correlationheader"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/costseriesproducers"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/dashboardpromtool"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/k6loadrigor"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/lokiretentionschema"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/lokivaultretention"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/orphanalerts"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/runtimeslo"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/vaultpromalerts"
)

// M9 Step 7: quality gate entry points for the "observability" theme, decomposed out of the
// former quality_subcmd.go god file. Each func is referenced as a Run in
// gateRegistry (quality_registry.go) — the single source of truth
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism).

// runDashboardPromtool validates Grafana dashboards JSON syntax (always)
// and PromQL queries via promtool (skip-on-missing). T4.3 of plan
// 2026-05-26-maturity-3-62-to-4-0-roadmap.
func runDashboardPromtool(args []string) int {
	fs := flag.NewFlagSet("dashboard-promtool", flag.ContinueOnError)
	dashboardsDir := fs.String("dir", "infra/helm/monitoring/dashboards", "dashboard directory to scan")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	outcome, err := dashboardpromtool.Check(context.Background(),
		dashboardpromtool.Config{DashboardsDir: *dashboardsDir})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard-promtool: %v\n", err)
		return 5
	}
	if len(outcome.SyntaxErrors) > 0 {
		fmt.Fprintf(os.Stderr, "dashboard-promtool: FAIL — %d JSON syntax errors:\n", len(outcome.SyntaxErrors))
		for _, e := range outcome.SyntaxErrors {
			fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
		return 1
	}
	if len(outcome.PromtoolErrors) > 0 {
		fmt.Fprintf(os.Stderr, "dashboard-promtool: FAIL — %d PromQL parse errors:\n", len(outcome.PromtoolErrors))
		for _, e := range outcome.PromtoolErrors {
			fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
		return 1
	}
	if outcome.PromtoolSkipped {
		fmt.Printf("dashboard-promtool: PASS (JSON syntax %d/%d) — promtool SKIP (%s)\n",
			outcome.TotalScanned, outcome.TotalScanned, outcome.PromtoolReason)
	} else {
		fmt.Printf("dashboard-promtool: PASS — %d dashboards (JSON syntax + PromQL semantic)\n",
			outcome.TotalScanned)
	}
	return 0
}

func runRuntimeSLO(args []string) int {
	fs := flag.NewFlagSet("runtime-slo", flag.ContinueOnError)
	logFile := fs.String("log", "/tmp/go-test-output.log", "go test -v output log")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := runtimeslo.Check(runtimeslo.Config{LogFile: *logFile, Limits: runtimeslo.DefaultLimits()})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[runtime-slo] %v\n", err)
		return 5
	}
	if o.Skipped {
		fmt.Printf("[runtime-slo] no go test log at %s — skip\n", *logFile)
		return 0
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "[runtime-slo] FAIL: %d test(s)/suite(s) over SLO:\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "[runtime-slo] for legitimate slow tests, add: // slow_test: <issue-url>")
		return 1
	}
	fmt.Println("[runtime-slo] PASS — all tests/suites within SLO")
	return 0
}

func runAlertRunbooks(args []string) int {
	fs := flag.NewFlagSet("alert-runbooks", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := alertrunbooks.Check(alertrunbooks.Config{Root: *root})
	if !o.Pass {
		for _, v := range o.Violations {
			fmt.Printf("RUNBOOK-MISSING VIOLATION: %s\n", v)
		}
		// SITES and ALERTS are different numbers, and the old message said "alert(s)"
		// while counting sites. Nine alert files exist twice in the tree
		// (`infra/helm/...` and the mirrored copy in `charts/...`), so the site count
		// is higher than the alert count — measured 2026-09-06: 46 sites, 37 alerts.
		// Calling 46 "alerts without runbook" inflates the debt by 24%.
		fmt.Fprintf(os.Stderr, "\nFAIL — %d alert(s) without runbook annotation, in %d site(s).\n",
			o.DistinctAlerts(), len(o.Violations))
		if o.DistinctAlerts() != len(o.Violations) {
			fmt.Fprintf(os.Stderr, "(%d site(s) are mirrored copies of the same alert — fix every site.)\n",
				len(o.Violations)-o.DistinctAlerts())
		}
		fmt.Fprintln(os.Stderr, "To fix: add a `runbook_url:` annotation linking to recovery docs, OR add the")
		fmt.Fprintln(os.Stderr, "alert name to ExceptionList (cmd/loopzero-ops/internal/quality/alertrunbooks) with an explanation.")
		return 1
	}
	fmt.Println("PASS — every alert has a runbook (or is in the exception list).")
	return 0
}

func runOrphanAlerts(args []string) int {
	fs := flag.NewFlagSet("orphan-alerts", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := orphanalerts.Check(orphanalerts.Config{Root: *root})
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "FAIL — %d orphan alert(s) (vector(0) > 1 placeholder, metric not emitted):\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  ORPHAN-ALERT VIOLATION: %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Either wire the real metric + rewrite the expr, OR add the alert name to")
		fmt.Fprintln(os.Stderr, "ExceptionList (cmd/loopzero-ops/internal/quality/orphanalerts) with a comment + linked work item.")
		return 1
	}
	fmt.Println("PASS — no orphan alerts.")
	return 0
}

// runCorrelationHeader scans api/internal/routes/*.go for direct
// r.Header.Get("(X-)?Correlation-ID") literals. Routes MUST go through
// api/internal/correlation.Read(r) instead (T6.1 of plan
// 2026-06-01-p3-infra-observability-test-debt, ADR D5 — closes val-002).
func runCorrelationHeader(args []string) int {
	fs := flag.NewFlagSet("correlation-header", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := correlationheader.Check(correlationheader.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "correlation-header: %v\n", err)
		return 5
	}
	if !outcome.Pass() {
		fmt.Fprintf(os.Stderr, "correlation-header: FAIL — %d direct r.Header.Get(\"(X-)?Correlation-ID\") call(s) in routes/:\n", len(outcome.Findings))
		for _, v := range outcome.Findings {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Fix: replace with `id := correlation.Read(r)` (api/internal/correlation).")
		fmt.Fprintln(os.Stderr, "The helper handles backward-compat (X-Correlation-ID fallback) + deprecation WARN.")
		return 1
	}
	fmt.Printf("correlation-header: PASS — no direct r.Header.Get((X-)?Correlation-ID) in api/internal/routes/ (%s)\n", outcome.Summary())
	return 0
}

// runK6LoadRigor enforces the k6 load-harness rigor fixes (dropped_iterations
// threshold in base + L5 distributed manifest shape + README per-level doc).
// Migrated from infra/tests/check-k6-load-rigor_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runK6LoadRigor(args []string) int {
	fs := flag.NewFlagSet("k6-load-rigor", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := k6loadrigor.Check(k6loadrigor.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "k6-load-rigor: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("k6-load-rigor: PASS — dropped_iterations threshold in base + L5 manifest shape + per-level docs canonical")
	return 0
}

// runLokiRetentionSchema is the trio-compat regression for the retention_stream
// schema in infra/helm/loki/values.yaml. 7 assertions.
// Migrated from infra/tests/check-loki-retention-schema_test.sh
// (M-Migrate2 2026-07-10 M16 T3.2 Wave 2).
func runLokiRetentionSchema(args []string) int {
	fs := flag.NewFlagSet("loki-retention-schema", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := lokiretentionschema.Check(lokiretentionschema.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "loki-retention-schema: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("loki-retention-schema: PASS — 7 schema assertions (SingleBinary + retention_stream secrets-system + priority int + defaults) clean")
	return 0
}

// runLokiVaultRetention enforces T8.2 + T8.5 of plan 2026-06-02-openbao-adoption:
// Loki per-NS retention config + archive CronJob hardening. 14 assertions.
// Migrated from infra/tests/check-loki-vault-retention_test.sh
// (M-Migrate2 2026-07-10 M16 T3.1 Wave 2).
func runLokiVaultRetention(args []string) int {
	fs := flag.NewFlagSet("loki-vault-retention", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := lokivaultretention.Check(lokivaultretention.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "loki-vault-retention: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("loki-vault-retention: PASS — T8.5 archive CronJob hardening assertions clean")
	return 0
}

// runVaultPromAlerts enforces T8.4 PrometheusRule vault-audit-alerts contract.
// Migrated from infra/tests/check-vault-prom-alerts_test.sh
// (M-Migrate2 2026-07-10 M16 T3.3 Wave 2).
func runVaultPromAlerts(args []string) int {
	fs := flag.NewFlagSet("vault-prom-alerts", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := vaultpromalerts.Check(vaultpromalerts.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "vault-prom-alerts: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("vault-prom-alerts: PASS — 4 P1 alerts declared with severity=critical + runbook_url + team=platform-security + domain=vault (+ invariant marker on VaultAuditLogStalled)")
	return 0
}

// runCostSeriesProducers is the B-181 gate: every `loopzero_*` series loopzero-cloud's
// cost aggregator queries must have a declared answer in this repo — a producing
// file, or a tracked absence naming the work item.
//
// It prints THREE verdicts, never fewer:
//
//   - PASS      the producer contract holds AND the declaration was compared
//     against loopzero-cloud's promQueries map.
//
//   - PARTIAL   the producer contract holds; loopzero-cloud is not on disk, so the
//     drift comparison did NOT run. Never printed as PASS: the gate
//     does not report a result it did not observe (B-169).
//
//     PARTIAL EXITS 0. The word is for whoever reads the log; the
//     chain does not stop, and inside 140 gates that makes it
//     operationally indistinguishable from a pass. The drift half is
//     therefore opt-in by environment and its absence is free — and
//     the default sibling path is the ABSENT case on any checkout
//     that does not sit beside loopzero-cloud, which is most of them.
//     Making PARTIAL exit non-zero would fail the chain on all of
//     those, so it is a decision with a cost rather than an
//     oversight; what is fixed here is the comment claiming more
//     than the exit code delivers.
//
//   - FAIL      a series is queried and undeclared, a declared producer does not
//     emit what it claims, or a tracked absence has quietly closed.
//
// Tracked absences are printed on EVERY run, pass or not. A known gap that is
// silent is indistinguishable from no gap; a known gap that turns the gate red
// forever teaches that red is normal. Naming it is the third option.
func runCostSeriesProducers(args []string) int {
	fs := flag.NewFlagSet("cost-series-producers", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	sibling := fs.String("loopzero-cloud", os.Getenv("LOOPZERO_CLOUD_ROOT"),
		"loopzero-cloud checkout (default $LOOPZERO_CLOUD_ROOT, else <root>/../loopzero-cloud)")
	declaration := fs.String("declaration", costseriesproducers.DefaultDeclarationFile,
		"pinned series/producer contract, relative to --root")
	aggregator := fs.String("aggregator", costseriesproducers.DefaultAggregatorFile,
		"cost aggregator source, relative to --loopzero-cloud")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := costseriesproducers.Check(costseriesproducers.Config{
		RepoRoot:        *root,
		SiblingRoot:     *sibling,
		DeclarationFile: *declaration,
		AggregatorFile:  *aggregator,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "cost-series-producers: %v\n", err)
		return 5
	}

	// The known, tracked gap goes to stdout on every run — that is what turns
	// silence into a tracked absence.
	for _, a := range outcome.TrackedAbsences {
		fmt.Printf("cost-series-producers: TRACKED ABSENCE — %s is queried by the aggregator and emitted by no repository\n", a)
	}

	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "cost-series-producers: FAIL — %d violation(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintf(os.Stderr, "The pin is %s. Every series the aggregator queries needs one line there:\n", *declaration)
		fmt.Fprintln(os.Stderr, "  <series> PRODUCER <path in this repo that declares the metric>")
		fmt.Fprintln(os.Stderr, "  <series> ABSENT   <work item tracking who will emit it, or the decision to drop it>")
		return 1
	}

	if !outcome.SiblingChecked {
		fmt.Fprintf(os.Stderr, "cost-series-producers: DRIFT NOT CHECKED — %s\n", outcome.SiblingSkipReason)
		fmt.Printf("cost-series-producers: PARTIAL — producer contract holds for %d declared series "+
			"(%d producer(s) verified, %d tracked absence(s)); drift vs loopzero-cloud NOT CHECKED\n",
			len(outcome.Declarations), len(outcome.VerifiedProducers), len(outcome.TrackedAbsences))
		return 0
	}

	fmt.Printf("cost-series-producers: PASS — %d series queried by loopzero-cloud, all declared "+
		"(%d producer(s) verified, %d tracked absence(s)); drift CHECKED against %s\n",
		len(outcome.QueriedSeries), len(outcome.VerifiedProducers), len(outcome.TrackedAbsences),
		outcome.SiblingRoot)
	return 0
}
