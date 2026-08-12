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

func runEnvPrefixParity(args []string) int {
	fs := flag.NewFlagSet("env-prefix-parity", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	out, err := envprefixparity.Check(envprefixparity.Config{RepoRoot: *root})
	switch {
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
		fmt.Fprintln(os.Stderr, "matching (B-070: sensitive vs insensitive). Green here does not mean unified.")
		return 1
	case err != nil:
		fmt.Fprintf(os.Stderr, "env-prefix-parity: %v\n", err)
		return 2
	}
	fmt.Printf("env-prefix-parity: PASS — %d declarations hold the same prefix set\n", len(out.Declarations))
	return 0
}
