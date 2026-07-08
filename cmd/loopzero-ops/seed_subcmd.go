// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// seed subcommand group — Go replacements for the canonical post-Cleanroom
// shell seeds (M15 — operator-glue-go-migration). The composition root: it
// builds the KubeClient + loads creds, then dispatches to a registered seed.
//
// Seeds are registered by the per-wave files (seed_wave*.go); Phase 0 ships
// the dispatch + registry plumbing with an empty registry. `seed list` and
// `seed all` iterate the registry in canonical (insertion) order.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/seed"
)

const seedHelpText = `loopzero-ops seed — Go replacements for the canonical post-Cleanroom seeds (M15)

USAGE:
  loopzero-ops seed <name> [--verify] [--dry-run] [--json]
  loopzero-ops seed all    [--verify] [--dry-run] [--json]
  loopzero-ops seed list

SEMANTICS:
  Run (default) applies the seed idempotently (read → guard → CreateOrUpdate).
  --verify   probe the post-condition instead of applying (parity with bash --verify).
  --dry-run  print intended actions without mutating the cluster.
  --json     emit one machine-readable line per seed: {"seed":..,"verdict":..,"reason":..}.

EXIT CODES (PERMANENT — CLAUDE.md Rule 4):
  0  all seeds PASS or SKIP
  1  usage error (unknown seed, bad flags)
  2  at least one seed FAIL
`

// seedRegistry is the process-wide registry the wave files populate via init().
var seedRegistry = seed.NewRegistry()

func runSeed(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, seedHelpText)
		return 1
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(seedHelpText)
		return 0
	case "list":
		for _, n := range seedRegistry.Names() {
			fmt.Println(n)
		}
		return 0
	}

	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	verify := fs.Bool("verify", false, "probe post-condition instead of applying")
	dryRun := fs.Bool("dry-run", false, "print intended actions without mutating")
	asJSON := fs.Bool("json", false, "emit one JSON line per seed")
	name := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}

	var targets []seed.Seed
	if name == "all" {
		for _, n := range seedRegistry.Names() {
			s, _ := seedRegistry.Lookup(n)
			targets = append(targets, s)
		}
	} else {
		s, ok := seedRegistry.Lookup(name)
		if !ok {
			fmt.Fprintf(os.Stderr, "seed: unknown seed %q (run `loopzero-ops seed list`)\n", name)
			return 1
		}
		targets = []seed.Seed{s}
	}

	deps, err := buildSeedDeps(*dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		return 2
	}

	worst := 0
	for _, s := range targets {
		var r seed.Result
		if *verify {
			r = s.Verify(deps)
		} else {
			r = s.Run(deps)
		}
		emitSeedResult(s.Name(), r, *asJSON)
		if code := r.Verdict.ExitCode(); code > worst {
			worst = code // EC-2: continue-and-aggregate; worst verdict wins
		}
	}
	return worst
}

func emitSeedResult(name string, r seed.Result, asJSON bool) {
	if asJSON {
		line, _ := json.Marshal(map[string]string{
			"seed": name, "verdict": r.Verdict.String(), "reason": r.Reason,
		})
		fmt.Println(string(line))
		return
	}
	if r.Reason != "" {
		fmt.Printf("seed %s: %s — %s\n", name, r.Verdict, r.Reason)
	} else {
		fmt.Printf("seed %s: %s\n", name, r.Verdict)
	}
}

// buildSeedDeps is the composition root for the k8s client + creds. Phase 0
// leaves the KubeClient nil (no seeds registered yet to consume it); the wave
// PRs wire genericclioptions.ConfigFlags → controller-runtime client here.
func buildSeedDeps(dryRun bool) (seed.Deps, error) {
	return seed.Deps{
		Ctx:    context.Background(),
		DryRun: dryRun,
	}, nil
}
