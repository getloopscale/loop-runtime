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
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/silentsurface"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/statuswritercontract"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/acyclicgraph"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/contractconsumers"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/noshellwrap"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/packageadmission"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/pkgusagematrix"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/servicesacyclic"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/stackcoverage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/subpackagefence"
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
	// Lost coverage is ALWAYS printed — including on PASS, which is where it would
	// vanish. A consumer root that is absent (usually a sibling checkout that is
	// not cloned) makes the verdict PARTIAL; it does not fail the gate on its own.
	if outcome.Partial {
		fmt.Fprintf(os.Stderr, "contract-consumers: PARTIAL — %d of %d consumer root(s) do not exist "+
			"and were NOT scanned: %v\n", len(outcome.MissingRoots), len(outcome.ScanRoots),
			outcome.MissingRoots)
	}
	// A missing tracked path FAILS, and the difference from the warning above is
	// deliberate. An absent consumer root shrinks where the gate hunts for the READER of
	// the field, and where to repoint those is a meeting decision (B-100) — hence a
	// warning. An absent tracked path takes away the gate's ability to see the field DROP
	// at all: it starts answering PASS over a contract surface it never read, and nothing
	// in the verdict text changes. That is not reduced coverage, it is a verdict without
	// a subject, and the fix depends on no meeting — point the list at the new path.
	if len(outcome.MissingTracked) > 0 {
		fmt.Fprintf(os.Stderr, "contract-consumers: FAIL — %d of %d tracked path(s) do not exist "+
			"in this tree: %v\n", len(outcome.MissingTracked), len(outcome.TrackedFiles),
			outcome.MissingTracked)
		fmt.Fprintf(os.Stderr, "  git diff/grep silently ignore a pathspec matching nothing, so this "+
			"gate would be reporting PASS without reading that surface.\n"+
			"  Fix: point trackedFiles in cmd/loopzero-ops/internal/quality/contractconsumers/check.go "+
			"at the new path.\n")
		return 1
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
	verdict := "PASS"
	if outcome.Partial {
		verdict = "PASS (PARTIAL)"
	}
	if len(outcome.DroppedTags) == 0 {
		fmt.Printf("contract-consumers: %s — no JSON tag drops in tracked files\n", verdict)
	} else {
		fmt.Printf("contract-consumers: %s — %d JSON tag(s) dropped + ZERO consumer reads found: %v\n",
			verdict, len(outcome.DroppedTags), outcome.DroppedTags)
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
	allowlist := fs.String("allowlist", "infra/quality/shell-scripts-allowlist.md", "allowlist markdown")
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
	switch {
	case errors.Is(err, iofs.ErrNotExist):
		// AN ABSENT ALLOWLIST DECLARES NO EXEMPTIONS — the same contract
		// `internal/quality/nonewbash` states, and for the same measured reason:
		// this allowlist lives under `.claude/`, which `3d4075ca3` (2026-09-08)
		// removed from tracking, so it exists in no clone and on no runner.
		// Measured 2026-09-16, this read returned 5 and blocked a push.
		//
		// Nothing exempt is the strictest reading, so losing the file cannot
		// weaken the gate. A file that is PRESENT and unreadable stays fatal.
		fmt.Fprintf(os.Stderr, "unhomed-logic: no allowlist at %s — nothing is exempt\n", *allowlist)
		data = nil
	case err != nil:
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
				e.Domain, e.Path, e.LOC, yn(e.HasPython), yn(e.ShellsLive), fmt.Sprintf("%s (%s)", e.Disposition, e.Rationale))
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
	return reportSubpackageFence("services-acyclic", outcome, err, servicesacyclic.ActionableFix())
}

// reportSubpackageFence prints a subpackagefence verdict and maps it to an exit
// code: 0 on PASS, 1 when the fence refused (a rule, including an invalid
// registry), 5 when the scan itself could not complete (I/O or parse).
func reportSubpackageFence(gate string, o subpackagefence.Outcome, err error, fix string) int {
	fmt.Printf("%s: %d subpackage(s), %d file(s) parsed, %d unregistered, %d forbidden edge(s)\n",
		gate, len(o.Subpackages), o.FilesParsed, len(o.Unregistered), len(o.Edges))
	for _, u := range o.Unregistered {
		fmt.Printf("  UNREGISTERED %s — a subpackage nobody decided on\n", u)
	}
	for _, e := range o.Edges {
		// The two kinds need different fixes — one moves shared code into a leaf,
		// the other moves the CALL into a _test.go — so they are labelled apart.
		if e.TestOnly {
			fmt.Printf("  TEST-ONLY LEAF IN PRODUCTION %s → %s (%s:%d)\n", e.From, e.To, e.File, e.Line)
			continue
		}
		fmt.Printf("  FORBIDDEN EDGE %s → %s (%s:%d)\n", e.From, e.To, e.File, e.Line)
	}
	if err == nil {
		fmt.Printf("%s: PASS — every subpackage is registered and imports only what its allow-list names\n", gate)
		return 0
	}
	fmt.Fprintf(os.Stderr, "%s: FAIL — %v\n", gate, err)
	if !isSubpackageFenceRule(err) {
		return 5
	}
	fmt.Fprintf(os.Stderr, "\nFix: %s\n", fix)
	return 1
}

func isSubpackageFenceRule(err error) bool {
	for _, rule := range []error{
		subpackagefence.ErrEmptyAllowList,
		subpackagefence.ErrIncoherentRegistry,
		subpackagefence.ErrRegistryCycle,
		subpackagefence.ErrUnregisteredSubpackage,
		subpackagefence.ErrForbiddenEdge,
		subpackagefence.ErrTestOnlyLeafFromProduction,
		subpackagefence.ErrNothingParsed,
	} {
		if errors.Is(err, rule) {
			return true
		}
	}
	return false
}

// runPackageAdmission — B-058.
//
// The item is about an admission CRITERION that can say no. api/internal/k8s
// admitted by technology ("it uses client-go"), which admits every domain that
// happens to speak to the API server. The criterion is now "plumbing names no
// domain", and this gate is what makes it a rule rather than a paragraph.
//
// It fails in three distinguishable ways on purpose, because the fixes differ:
// a domain name readmitted into the plumbing package (move it to a home), a home
// that emptied out (the split was undone, and a gate green over a vacuum is the
// failure adapter-ports records), and nothing scanned at all.
func runPackageAdmission(args []string) int {
	fs := flag.NewFlagSet("package-admission", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	out, err := packageadmission.Check(packageadmission.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "package-admission: FAIL \u2014 %v\n", err)
		for _, v := range out.Violations {
			fmt.Fprintf(os.Stderr, "  api/internal/k8s/%s: %s carries the domain word %q \u2014 move it to k8s/tenancy, k8s/workload or k8s/secretsync\n", v.File, v.Where, v.Word)
		}
		return 1
	}
	fmt.Printf("package-admission: PASS \u2014 %d plumbing file(s) in api/internal/k8s name no domain; homes %v are populated\n", out.Scanned, out.Homes)
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

// runSilentSurface refuses NEW HTTP surfaces that vanish without saying so.
//
// The router decides fifty times whether to register a surface and, until B-104,
// threw every decision away — measured on the droplet as a 404 on the GitHub
// webhook while /health answered 405, with nothing anywhere reporting it.
//
// Converting all fifty in one commit was refused: router.go is the single
// surface where the route set is read (Unbreakable Rule 5), so an error in any
// one edit would be indistinguishable from the other forty-nine. This gate does
// what unhomed-logic does instead — counts the debt and forbids it from growing.
//
// Decision: MEET-ASYNC-AMENDMENT-2026-08-16-observable-silent-surfaces
func runSilentSurface(args []string) int {
	fs := flag.NewFlagSet("silent-surface", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	// --report lists the debt instead of gating it. Same affordance
	// `unhomed-logic` carries, and for the same reason: a monotonic baseline is
	// only actionable if somebody can see WHICH items compose it. Without it the
	// only way to read the list is to make the gate fail.
	report := fs.Bool("report", false, "list the mute surfaces (no gate)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	v, err := silentsurface.Check(silentsurface.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "silent-surface: %v\n", err)
		return 1
	}
	if *report {
		fmt.Printf("%d silent surface(s) in %d route guard(s) (baseline %d):\n",
			len(v.Silent), v.Guarded, v.Baseline)
		for _, s := range v.Silent {
			fmt.Printf("  %s:%d  cfg.%s\n", silentsurface.RouterPath, s.Line, s.Field)
		}
		return 0
	}
	fmt.Println(v.Summary())
	if !v.Pass {
		for _, s := range v.Silent {
			fmt.Printf("  %s:%d — cfg.%s registers a route and does not report the decision\n",
				silentsurface.RouterPath, s.Line, s.Field)
		}
		fmt.Fprintln(os.Stderr, silentsurface.ActionableFix)
		return 1
	}
	return 0
}

// runRoutesAcyclic fences the handler families of api/internal/routes (B-041).
func runRoutesAcyclic(args []string) int {
	fs := flag.NewFlagSet("routes-acyclic", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	o, err := routesacyclic.Check(routesacyclic.Config{RepoRoot: *root})
	return reportSubpackageFence("routes-acyclic", o, err, routesacyclic.ActionableFix())
}
