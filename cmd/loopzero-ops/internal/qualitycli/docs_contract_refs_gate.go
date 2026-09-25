// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/docsrefs"
)

func init() {
	register(Gate{
		Name:    "docs-contract-refs",
		Summary: "Every .claude/rules/*.md reference in CLAUDE.md and every ADR-NNNN cited in a Go comment resolves (or has an exception with a deadline).",
		Setup:   setupDocsContractRefs,
	})
}

func setupDocsContractRefs(fs *flag.FlagSet) Check {
	allowlist := fs.String("allowlist", "", "path to the allowlist (default: <root>/"+docsrefs.DefaultAllowlistRelPath+")")
	return func(inv Invocation) (Outcome, error) {
		out, err := docsrefs.Check(docsrefs.Config{RepoRoot: inv.Root, AllowlistPath: *allowlist})
		switch {
		case errors.Is(err, docsrefs.ErrMalformedAllowlist):
			// A line that reads as an exemption and exempts nothing is the defect
			// this gate exists to remove, so a bad allowlist is louder than a bad
			// reference — never a skipped line.
			fmt.Fprintf(inv.Stderr, "docs-contract-refs: FAIL — %v\n", err)
			return Fail, nil
		case errors.Is(err, docsrefs.ErrUnresolvedReferences):
			fmt.Fprintf(inv.Stderr, "docs-contract-refs: FAIL — %d normative reference(s) do not resolve:\n", len(out.Unresolved))
			for _, r := range out.Unresolved {
				fmt.Fprintf(inv.Stderr, "  %s\n", r)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "A reference that does not resolve is worse than no reference: it is followed.")
			fmt.Fprintf(inv.Stderr, "Fix: create the document, correct the reference, or add a DATED entry to %s.\n", docsrefs.DefaultAllowlistRelPath)
			fmt.Fprintln(inv.Stderr, "Do NOT recreate a missing contract from memory — invented documentation is worse than an absent one.")
			return Fail, nil
		case err != nil:
			return Fail, err
		}
		if len(out.NotVerified) > 0 {
			fmt.Fprintf(inv.Stdout, "docs-contract-refs: PASS (PARTIAL) — %d normative reference(s) resolve (%d allowlisted with a sunset); "+
				"%d reference(s) into the ecosystem kit NOT verified: the kit is not installed on this checkout\n",
				out.Checked, out.Allowlisted, len(out.NotVerified))
			return Pass, nil
		}
		fmt.Fprintf(inv.Stdout, "docs-contract-refs: PASS — %d normative reference(s) resolve (%d allowlisted with a sunset)\n",
			out.Checked, out.Allowlisted)
		return Pass, nil
	}
}
