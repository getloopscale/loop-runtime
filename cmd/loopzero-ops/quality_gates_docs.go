// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// quality_gates_docs.go holds the Run entry points for gates whose subject is
// the repository's own normative documentation — the contracts and decision
// records that code and CLAUDE.md tell a reader to consult.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/docsrefs"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/envprefixparity"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testtargetcoverage"
)

func runDocsContractRefs(args []string) int {
	fs := flag.NewFlagSet("docs-contract-refs", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	allowlist := fs.String("allowlist", "", "path to the allowlist (default: <root>/"+docsrefs.DefaultAllowlistRelPath+")")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	out, err := docsrefs.Check(docsrefs.Config{RepoRoot: *root, AllowlistPath: *allowlist})
	switch {
	case errors.Is(err, docsrefs.ErrMalformedAllowlist):
		// A line that reads as an exemption and exempts nothing is the defect
		// this gate exists to remove, so a bad allowlist is louder than a bad
		// reference — never a skipped line.
		fmt.Fprintf(os.Stderr, "docs-contract-refs: FAIL — %v\n", err)
		return 2
	case errors.Is(err, docsrefs.ErrUnresolvedReferences):
		fmt.Fprintf(os.Stderr, "docs-contract-refs: FAIL — %d normative reference(s) do not resolve:\n", len(out.Unresolved))
		for _, r := range out.Unresolved {
			fmt.Fprintf(os.Stderr, "  %s\n", r)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "A reference that does not resolve is worse than no reference: it is followed.")
		fmt.Fprintf(os.Stderr, "Fix: create the document, correct the reference, or add a DATED entry to %s.\n", docsrefs.DefaultAllowlistRelPath)
		fmt.Fprintln(os.Stderr, "Do NOT recreate a missing contract from memory — invented documentation is worse than an absent one.")
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "docs-contract-refs: %v\n", err)
		return 2
	}
	fmt.Printf("docs-contract-refs: PASS — %d normative reference(s) resolve (%d allowlisted with a sunset)\n",
		out.Checked, out.Allowlisted)
	return 0
}

func runTestTargetCoverage(args []string) int {
	fs := flag.NewFlagSet("test-target-coverage", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	out, err := testtargetcoverage.Check(testtargetcoverage.Config{RepoRoot: *root})
	switch {
	case errors.Is(err, testtargetcoverage.ErrModuleUncovered):
		fmt.Fprintf(os.Stderr, "test-target-coverage: FAIL — %d module(s) with no Taskfile target running their tests:\n", len(out.Uncovered))
		for _, m := range out.Uncovered {
			fmt.Fprintf(os.Stderr, "  %s\n", m)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Add `cd <module> && go test ./...` to taskfiles/test.yml (GOWORK=off for")
		fmt.Fprintln(os.Stderr, "modules outside go.work), OR add a NAMED exemption with a reason in")
		fmt.Fprintln(os.Stderr, "internal/quality/testtargetcoverage. `go build` does not count: that is")
		fmt.Fprintln(os.Stderr, "exactly how cmd/loopzero-ops looked covered while no test of it ever ran (B-088).")
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "test-target-coverage: %v\n", err)
		return 2
	}
	fmt.Printf("test-target-coverage: PASS — %d module(s): %d named by a test target, %d exempt with a "+
		"dated reason. Granularity is the MODULE, not the package (B-092).\n",
		len(out.Modules), len(out.Covered), len(out.Exempt))
	return 0
}

func runEnvPrefixParity(args []string) int {
	fs := flag.NewFlagSet("env-prefix-parity", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	out, err := envprefixparity.Check(envprefixparity.Config{RepoRoot: *root})
	switch {
	case errors.Is(err, envprefixparity.ErrCaseFoldingInMatch):
		fmt.Fprintln(os.Stderr, "env-prefix-parity: FAIL — a reserved-prefix comparison folds case:")
		fmt.Fprint(os.Stderr, envprefixparity.FormatCaseFindings(out.CaseFindings))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Matching is exact per ADR 2026-08-12-reserved-env-case-matching: env vars are")
		fmt.Fprintln(os.Stderr, "case-sensitive on Linux and every name LoopZero injects is uppercase, so a lowercase")
		fmt.Fprintln(os.Stderr, "key cannot shadow one. Folding rejects builds over a collision the OS forbids.")
		fmt.Fprintln(os.Stderr, "The INSTRUCTION keyword (ARG/ENV) may still be folded — this looks only inside")
		fmt.Fprintln(os.Stderr, "the loop over the prefix slice.")
		return 1
	case errors.Is(err, envprefixparity.ErrPrefixSetsDiverge):
		fmt.Fprintln(os.Stderr, "env-prefix-parity: FAIL — the two reservedEnvKeyPrefixes declarations hold different sets:")
		for _, d := range out.Declarations {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", d.File, d.Prefixes)
		}
		for file, only := range out.OnlyIn {
			fmt.Fprintf(os.Stderr, "  only in %s: %v\n", file, only)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "This gate proves the two PREFIX SETS match, and nothing else. The same rule also")
		fmt.Fprintln(os.Stderr, "diverges on severity (B-069: silent discard vs hard build failure) and on case")
		fmt.Fprintln(os.Stderr, "matching (B-070). Green here does not mean the rule is unified.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "If this divergence is a KNOWN gap, it belongs in")
		fmt.Fprintln(os.Stderr, "cmd/loopzero-ops/internal/quality/envprefixparity/allowlist.txt with a sunset —")
		fmt.Fprintln(os.Stderr, "an entry there may also have EXPIRED, which is why a gap can reappear.")
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "env-prefix-parity: %v\n", err)
		return 2
	}
	// An exemption is PRINTED on the green path, not swallowed by it. A gap
	// nobody sees on a passing run is a gap nobody retires — which is how the
	// 21 inherited approvals of helm-drift (B-076) became invisible.
	if len(out.Exempted) > 0 {
		fmt.Printf("env-prefix-parity: PASS — the %d find_sec_004 copies agree on case matching; "+
			"%d prefix(es) exempt with a sunset: %v. This compares the two copies to each other and "+
			"says nothing about whether the rule is reachable (B-094).\n",
			len(out.Declarations), len(out.Exempted), out.Exempted)
		return 0
	}
	fmt.Printf("env-prefix-parity: PASS — the %d find_sec_004 copies hold the same prefix set and "+
		"the same case matching. This compares them to each other and says nothing about whether "+
		"the rule is reachable (B-094).\n", len(out.Declarations))
	return 0
}
