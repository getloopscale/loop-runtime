// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/domainsinkpurity"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/routesacyclic"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/statuswritercontract"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/acyclicgraph"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/contractconsumers"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/noshellwrap"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/pkgusagematrix"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/servicesacyclic"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/stackcoverage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/unhomedlogic"
)

// M9 Step 7: quality gate entry points for the "arch" theme, decomposed out of the
// former quality_subcmd.go god file. Each func is referenced as a Run in
// gateRegistry (quality_registry.go) — the single source of truth
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism).

// runContractConsumers detects backend response shape narrowing (Go struct
// field with `json:"<tag>"` REMOVED in the PR diff) when at least one
// consumer in cloud/dashboard/src/, cli/src/, or api/internal/ still reads
// that field. See cmd/loopzero-ops/internal/quality/contractconsumers/check.go package doc for
// scope + non-scope + plan rationale. Sponsor mandate 2026-06-08: this
// gate REDUCES the bug rate that reaches the sponsor walkthrough; sponsor
// walkthrough remains the final visual gate.
func runContractConsumers(args []string) int {
	fs := flag.NewFlagSet("contract-consumers", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	baseRef := fs.String("base-ref", "origin/develop", "git ref the PR branches from")
	headRef := fs.String("head-ref", "HEAD", "git ref of the PR HEAD")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := contractconsumers.Check(contractconsumers.Config{
		RepoRoot: *root,
		BaseRef:  *baseRef,
		HeadRef:  *headRef,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "contract-consumers: %v\n", err)
		return 5
	}
	if outcome.CommandError != "" {
		fmt.Fprintf(os.Stderr, "contract-consumers: git/grep error: %s\n", outcome.CommandError)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "contract-consumers: FAIL — %d consumer reference(s) to dropped JSON tag(s) (between %s and %s):\n",
			len(outcome.Findings), *baseRef, *headRef)
		fmt.Fprint(os.Stderr, contractconsumers.FormatFindings(outcome.Findings))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintf(os.Stderr, "Fix:\n  %s\n", contractconsumers.ActionableFix())
		return 1
	}
	if len(outcome.DroppedTags) == 0 {
		fmt.Println("contract-consumers: PASS — no JSON tag drops in tracked files")
	} else {
		fmt.Printf("contract-consumers: PASS — %d JSON tag(s) dropped + ZERO consumer reads found: %v\n",
			len(outcome.DroppedTags), outcome.DroppedTags)
	}
	return 0
}

// runAcyclicGraph is the Go home for the acyclic-graph gate (M5 — plan
// m5-governance-durable). Enforces the decomposition DAG: loopzero-contracts imports
// nothing from the engine/cloud/cli; the engine never imports the cloud; the
// cloud never imports engine-internal pkg/*. Sibling repos are resolved relative
// to the engine root and skipped when absent.
func runAcyclicGraph(args []string) int {
	fs := flag.NewFlagSet("acyclic-graph", flag.ContinueOnError)
	root := fs.String("root", ".", "engine repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := acyclicgraph.Check(acyclicgraph.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "acyclic-graph: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "acyclic-graph: FAIL — %d forbidden upward import edge(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Decomposition invariant (ADP): loopzero-contracts ◄── engine + loopzero-cloud + loopzero-cli. Arrows never point at a more-stable node.")
		return 1
	}
	msg := fmt.Sprintf("acyclic-graph: PASS — graph is acyclic (checked: %s", strings.Join(outcome.Checked, ", "))
	if len(outcome.Skipped) > 0 {
		msg += fmt.Sprintf("; skipped absent: %s", strings.Join(outcome.Skipped, ", "))
	}
	fmt.Println(msg + ")")
	return 0
}

// runPkgUsageMatrix is the gate replacing pkg-usage-matrix.sh: keeps the pkg
// Real Usage Matrix doc in sync with actual imports. --check verifies, --write
// regenerates. Pure-logic (grep of imports), hermetic.
func runPkgUsageMatrix(args []string) int {
	fs := flag.NewFlagSet("pkg-usage-matrix", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	docPath := fs.String("doc-path", "", "override matrix doc path")
	write := fs.Bool("write", false, "regenerate the matrix (default: --check only)")
	fs.Bool("check", true, "verify matrix freshness (default)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	mode := pkgusagematrix.ModeCheck
	if *write {
		mode = pkgusagematrix.ModeWrite
	}

	outcome, err := pkgusagematrix.Run(pkgusagematrix.Config{
		Root:    *root,
		DocPath: *docPath,
	}, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pkg-usage-matrix: %v\n", err)
		return 5
	}
	for _, m := range outcome.Messages {
		fmt.Println(m)
	}
	if !outcome.Pass {
		return 1
	}
	return 0
}

// runUnhomedLogic is the unbiased migration tracker: counts un-homed
// operational logic from the allowlist + ratchets a monotonic baseline.
func runUnhomedLogic(args []string) int {
	fs := flag.NewFlagSet("unhomed-logic", flag.ContinueOnError)
	allowlist := fs.String("allowlist", ".claude/rules/shell-scripts-allowlist.md", "allowlist markdown")
	baselinePath := fs.String("baseline", "infra/quality/unhomed-baseline.json", "baseline JSON")
	root := fs.String("root", ".", "repo root (for content-verifying KEEP claims)")
	report := fs.Bool("report", false, "print per-domain un-homed counts (no gate)")
	ledger := fs.Bool("ledger", false, "emit per-script disposition + evidence (markdown, for Phase-5 review)")
	update := fs.Bool("update-baseline", false, "ratchet the baseline to current (write)")
	fs.Bool("check", true, "ratchet against baseline (default)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	data, err := os.ReadFile(*allowlist) // #nosec G304 -- repo-internal allowlist path
	if err != nil {
		fmt.Fprintf(os.Stderr, "unhomed-logic: read allowlist: %v\n", err)
		return 5
	}
	// Content verification: read each script body to invalidate suspect KEEPs.
	readBody := func(p string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(*root, p)) // #nosec G304 -- repo-internal script path
		if err != nil {
			return "", false
		}
		return string(b), true
	}
	current := unhomedlogic.Count(string(data), readBody)

	if *report {
		for _, l := range current.Render() {
			fmt.Println(l)
		}
		return 0
	}

	if *ledger {
		entries := unhomedlogic.Ledger(string(data), readBody)
		fmt.Println("# Phase-5 Classification Ledger — un-homed operational logic")
		fmt.Println("")
		fmt.Println("Proposed disposition per script (operator ratifies). Evidence is mechanical fact.")
		fmt.Println("")
		fmt.Println("| Domain | Script | LOC | Python | ShellsLive | Proposed disposition |")
		fmt.Println("|---|---|---:|:---:|:---:|---|")
		yn := func(b bool) string {
			if b {
				return "yes"
			}
			return "—"
		}
		for _, e := range entries {
			fmt.Printf("| %s | %s | %d | %s | %s | %s |\n",
				e.Domain, e.Path, e.LOC, yn(e.HasPython), yn(e.ShellsLive), e.Disposition)
		}
		return 0
	}

	if *update {
		b, _ := json.MarshalIndent(current, "", "  ")
		if err := os.WriteFile(*baselinePath, append(b, '\n'), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "unhomed-logic: write baseline: %v\n", err)
			return 5
		}
		fmt.Printf("unhomed-logic: baseline ratcheted to %d un-homed\n", current.Total)
		return 0
	}

	// --check (default): fail if current exceeds baseline (regression).
	bdata, err := os.ReadFile(*baselinePath) // #nosec G304 -- repo-internal baseline path
	if err != nil {
		fmt.Fprintf(os.Stderr, "unhomed-logic: no baseline at %s — run --update-baseline first\n", *baselinePath)
		return 5
	}
	var baseline unhomedlogic.Counts
	if err := json.Unmarshal(bdata, &baseline); err != nil {
		fmt.Fprintf(os.Stderr, "unhomed-logic: parse baseline: %v\n", err)
		return 5
	}
	ok, regressions := unhomedlogic.RatchetCheck(current, baseline)
	for _, l := range current.Render() {
		fmt.Println(l)
	}
	if len(current.SuspectKeeps) > 0 {
		fmt.Fprintln(os.Stderr, "unhomed-logic: FAIL — suspect KEEP (script tagged KEEP-* but embeds a Python heredoc = reimplementable logic, not break-glass):")
		for _, s := range current.SuspectKeeps {
			fmt.Fprintf(os.Stderr, "  - %s\n", s)
		}
		fmt.Fprintln(os.Stderr, "A KEEP claim must be genuine operator glue. Move the logic to loopzero-ops/Go, or drop the KEEP tag.")
		return 1
	}
	if !ok {
		fmt.Fprintln(os.Stderr, "unhomed-logic: FAIL — un-homed operational logic increased:")
		for _, r := range regressions {
			fmt.Fprintf(os.Stderr, "  - %s\n", r)
		}
		fmt.Fprintln(os.Stderr, "Per plan 2026-05-23-operational-logic-capability-architecture: a PR may never")
		fmt.Fprintln(os.Stderr, "increase un-homed logic. Home the capability (loopzero-ops/declarative/test) or delete it.")
		return 1
	}
	fmt.Printf("unhomed-logic: PASS — %d un-homed (≤ baseline %d)\n", current.Total, baseline.Total)
	return 0
}

func runNoShellWrap(args []string) int {
	fs := flag.NewFlagSet("no-shell-wrap", flag.ContinueOnError)
	root := fs.String("root", "pkg", "directory to scan")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	outcome, err := noshellwrap.Check(noshellwrap.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-shell-wrap: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintln(os.Stderr, "no-shell-wrap: FAIL — Go code wraps forbidden CLI without nolint:")
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "To fix: use the Go SDK (pulumi-go-sdk, helm.sh/helm/v3, client-go,")
		fmt.Fprintln(os.Stderr, "aws-sdk-go-v2, go-github) OR add //nolint:shell-wrap on the call line")
		fmt.Fprintln(os.Stderr, "with justificativa per plan D5 + Anti-Pattern #8.")
		return 1
	}
	fmt.Println("no-shell-wrap: PASS — no forbidden shell wraps detected")
	return 0
}

func runStackCoverage(args []string) int {
	fs := flag.NewFlagSet("stack-coverage", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := stackcoverage.Check(stackcoverage.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[stack-coverage] FAIL: %v\n", err)
		return 1
	}
	if !o.Pass {
		if len(o.TypeErrors) > 0 {
			fmt.Fprintln(os.Stderr, "[stack-coverage] strategy type drift in languages.go:")
			for _, e := range o.TypeErrors {
				fmt.Fprintf(os.Stderr, "  %s\n", e)
			}
		}
		if len(o.Missing) > 0 {
			fmt.Fprintln(os.Stderr, "[stack-coverage] strategies without E2E fixture coverage:")
			for _, m := range o.Missing {
				fmt.Fprintf(os.Stderr, "  %s (add infra/tests/e2e/fixtures/<name>/loopzero.yaml with framework: %s)\n", m, m)
			}
		}
		fmt.Fprintln(os.Stderr, "[stack-coverage] add fixtures OR deregister the strategy from languages.go::ResolveStrategy.")
		return 1
	}
	fmt.Printf("[stack-coverage] PASS: all %d strategies have fixture coverage.\n", o.StrategyCount)
	return 0
}

// runServicesAcyclic enforces the M9 Step 6 bounded-context acyclic import DAG of
// api/internal/services (deny-by-default allow-list). ADR
// 2026-07-03-m9-db-store-subpackage-topology; MEET-2026-07-03-001.
func runServicesAcyclic(args []string) int {
	fs := flag.NewFlagSet("services-acyclic", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := servicesacyclic.Check(servicesacyclic.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "services-acyclic: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "services-acyclic: FAIL \u2014 %d forbidden edge(s), %d unknown context(s) (scanned %d .go files):\n", len(outcome.Violations), len(outcome.Unknown), outcome.Scanned)
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s:%d edge %s \u2192 %s not in allow-list\n", v.File, v.Line, v.From, v.To)
		}
		for _, u := range outcome.Unknown {
			fmt.Fprintf(os.Stderr, "  unregistered subpackage: services/%s\n", u)
		}
		fmt.Fprintf(os.Stderr, "\nFix: %s\n", servicesacyclic.ActionableFix())
		return 1
	}
	fmt.Printf("services-acyclic: PASS \u2014 %d .go files scanned; services bounded-context import graph is the frozen acyclic DAG (M9 Step 6)\n", outcome.Scanned)
	return 0
}

// runDomainSinkPurity — B-049 / slice-4 T1.1.
//
// A ratchet, not a fence: it freezes the persistence symbols measured crossing
// the services DAG sink and refuses the next one. Removing them costs 63
// call sites (B-079); stopping the growth costs this.
func runDomainSinkPurity(args []string) int {
	fs := flag.NewFlagSet("domain-sink-purity", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	o, err := domainsinkpurity.Check(domainsinkpurity.Config{RepoRoot: *root})
	fmt.Println(o.Summary())
	for _, s := range o.New {
		fmt.Printf("  NOVO VAZAMENTO %s — atravessa a fronteira do sink e alcança os 35 arquivos, em 10 pacotes, que dependem dele\n", s)
	}
	for _, s := range o.Gone {
		fmt.Printf("  REMOVIDO %s — aperte o conjunto congelado em FrozenDefault para travar o ganho\n", s)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "domain-sink-purity: %v\n", err)
		// 5 = could not measure; 1 = measured and found a violation. Collapsing
		// the two is exactly the distinction B-040 cost this repo.
		if errors.Is(err, domainsinkpurity.ErrSinkNotFound) || errors.Is(err, domainsinkpurity.ErrUnqualifiedImport) {
			return 5
		}
		return 1
	}
	fmt.Printf("PASS — nenhum símbolo novo (persistência ou camada de aplicação) atravessa o sink (%d congelados, rastreados em B-079)\n", o.FrozenCount)
	return 0
}

// runStatusWriterContract — B-085 bala 4.
//
// The gate the migration asked for in the same breath as the conversion, and
// that for a while did not exist: an adversarial audit injected a fresh bare
// Update into a package with no per-package ratchet and the whole operators
// suite stayed green. A ratchet protects the package somebody remembered; a
// walk fails on the ABSENCE of protection.
//
// Decision: MEET-ASYNC-AMENDMENT-2026-08-14-status-writer-gate
func runStatusWriterContract(args []string) int {
	fs := flag.NewFlagSet("status-writer-contract", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	o, err := statuswritercontract.Check(statuswritercontract.Config{Root: *root})
	fmt.Println(o.Summary())
	for _, v := range o.Violations {
		if v.Kind == statuswritercontract.UncoveredSummary {
			fmt.Printf("  %s:%d — the package of %s() persists status and folds no readiness summary, "+
				"in-line or deferred (ADR-0025 D3)\n", v.File, v.Line, v.Func)
			continue
		}
		fmt.Printf("  %s:%d — %s() persists status with Update (reached by %s)\n",
			v.File, v.Line, v.Func, v.Form)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "status-writer-contract: %v\n", err)
		return 1
	}
	if !o.Pass {
		fmt.Fprintln(os.Stderr, statuswritercontract.ActionableFix)
		return 1
	}
	return 0
}

// runRoutesAcyclic — B-041 / slice-5 T1.3.
//
// The fence that precedes the split. B-041 states the order explicitly: a fence
// written after the move is written to fit what was already done.
func runRoutesAcyclic(args []string) int {
	fs := flag.NewFlagSet("routes-acyclic", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	o, err := routesacyclic.Check(routesacyclic.Config{RepoRoot: *root})
	fmt.Println(o.Summary())
	for _, u := range o.Unregistered {
		fmt.Printf("  NÃO REGISTRADO %s — uma família de handlers que ninguém decidiu\n", u)
	}
	for _, e := range o.Edges {
		// The two kinds need different fixes — one moves shared code into a
		// leaf, the other moves the CALL into a _test.go — so a single label
		// for both would point the reader at the wrong remedy.
		if e.TestOnly {
			fmt.Printf("  LEAF SÓ-DE-TESTE em produção %s → %s (%s:%d)\n", e.From, e.To, e.File, e.Line)
			continue
		}
		fmt.Printf("  ARESTA IRMÃ %s → %s (%s:%d)\n", e.From, e.To, e.File, e.Line)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "routes-acyclic: %v\n", err)
		fmt.Fprintln(os.Stderr, routesacyclic.ActionableFix())
		if errors.Is(err, routesacyclic.ErrEmptyAllowList) {
			return 5
		}
		return 1
	}
	fmt.Println("PASS — todo subpacote de routes está registrado e nenhum importa um irmão")
	return 0
}
