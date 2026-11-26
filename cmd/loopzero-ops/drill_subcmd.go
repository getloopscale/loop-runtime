// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// drill subcommand group — pure-logic pieces extracted from the drill/DR bash
// orchestrators (Track C of 2026-05-24-drill-consolidation-migration-plan).
// Only validation/arithmetic/shaping lives here; provisioning (pulumi/doctl/helm)
// stays in bash (ADR D1 no-shell-wrap).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/drill/cost"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/drill/postgresrestore"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/shellrun"
)

const drillHelpText = `loopzero-ops drill — pure-logic helpers for the drill/DR orchestrators

USAGE:
  loopzero-ops drill <subcommand> [flags]

SUBCOMMANDS:
  cost --start <ISO> --end <ISO>    Compute drill run cost + render markdown report
                                    (replaces the arithmetic half of dr/cost-report.sh;
                                    the --auto Pulumi-state read stays in bash)
  postgres-restore [--cluster <name>] [--namespace <ns>] [--context <ctx>] [--timeout <d>]
                                    Prove the platform's PostgreSQL recovers from its
                                    backups: write a marker, wait for its WAL to be
                                    archived, recover a temporary cluster from the bucket,
                                    compare, and delete the temporary cluster. Prints a
                                    JSON report; exits 1 when the recovery fails.
`

func runDrill(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, drillHelpText)
		return 1
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(drillHelpText)
		return 0
	case "cost":
		return runDrillCost(args[1:])
	case "postgres-restore":
		return runDrillPostgresRestore(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown drill subcommand: %s\n\n%s", args[0], drillHelpText)
		return 1
	}
}

// runDrillCost is the pure-logic cost report (replaces dr/cost-report.sh:106-159).
func runDrillCost(args []string) int {
	fs := flag.NewFlagSet("drill cost", flag.ContinueOnError)
	start := fs.String("start", "", "provision-start ISO timestamp (required)")
	end := fs.String("end", "", "teardown-end ISO timestamp (required)")
	output := fs.String("output", "", "write markdown to this file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *start == "" || *end == "" {
		fmt.Fprintln(os.Stderr, "drill cost: --start AND --end required")
		return 1
	}
	rates := cost.DefaultRates()
	rep, err := cost.Compute(*start, *end, rates)
	if err != nil {
		fmt.Fprintf(os.Stderr, "drill cost: %v\n", err)
		return 1
	}
	md := rep.Markdown(rates)
	if *output != "" {
		if err := os.WriteFile(*output, []byte(md+"\n"), 0o644); err != nil { // #nosec G306 -- report artifact
			fmt.Fprintf(os.Stderr, "drill cost: write %s: %v\n", *output, err)
			return 5
		}
		fmt.Fprintf(os.Stderr, "[drill cost] report written: %s ($%.2f total)\n", *output, rep.CostTotal)
		return 0
	}
	fmt.Print(md)
	return 0
}

// runDrillPostgresRestore recovers a temporary cluster from the backups of a running one
// (internal/drill/postgresrestore) and reports what came back.
func runDrillPostgresRestore(args []string) int {
	fs := flag.NewFlagSet("drill postgres-restore", flag.ContinueOnError)
	cluster := fs.String("cluster", "platform-postgres", "CloudNativePG cluster to recover from")
	namespace := fs.String("namespace", "loopzero-data-plane", "namespace of the cluster")
	kubeContext := fs.String("context", "", "kubeconfig context (default: the current one)")
	timeout := fs.Duration("timeout", 10*time.Minute, "bound of each wait: WAL archive, and recovery")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	platform := postgresrestore.Kubectl{
		Runner: shellrun.NewDefault(), Context: *kubeContext, Namespace: *namespace, Cluster: *cluster,
	}
	report, err := postgresrestore.Run(ctx, platform, postgresrestore.Options{
		Timeout: *timeout, Poll: 5 * time.Second,
		RunID: time.Now().UTC().Format("20060102T150405.000000000Z"),
	})
	out, encodeErr := json.MarshalIndent(report, "", "  ")
	if encodeErr != nil {
		fmt.Fprintf(os.Stderr, "drill postgres-restore: encode the report: %v\n", encodeErr)
		return 1
	}
	fmt.Println(string(out))
	if err != nil {
		fmt.Fprintf(os.Stderr, "drill postgres-restore: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "[drill postgres-restore] %s recovered in %s (WAL archived in %s)\n",
		*cluster, report.RecoveryTime.Round(time.Second), report.ArchiveWait.Round(time.Second))
	return 0
}
