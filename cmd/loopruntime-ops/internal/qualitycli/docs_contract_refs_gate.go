// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/docsrefs"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		out, err := docsrefs.Check(docsrefs.Config{RepoRoot: inv.Root, AllowlistPath: *allowlist})
		if err != nil {
			return nil, err
		}
		switch {
		case out.MalformedAllowlist != nil:
			// A line that reads as an exemption and exempts nothing is the defect
			// this gate exists to remove, so a bad allowlist is louder than a bad
			// reference — never a skipped line.
			inv.Stderr.Printf("docs-contract-refs: FAIL — %v\n", out.MalformedAllowlist)
			return out, nil
		case out.Verdict() == verdict.Fail:
			inv.Stderr.Printf("docs-contract-refs: FAIL — %d normative reference(s) do not resolve:\n", len(out.Unresolved))
			for _, r := range out.Unresolved {
				inv.Stderr.Printf("  %s\n", r)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("A reference that does not resolve is worse than no reference: it is followed.")
			inv.Stderr.Printf("Fix: create the document, correct the reference, or add a DATED entry to %s.\n", docsrefs.DefaultAllowlistRelPath)
			inv.Stderr.Println("Do NOT recreate a missing contract from memory — invented documentation is worse than an absent one.")
			return out, nil
		}
		if len(out.NotVerified) > 0 {
			inv.Stdout.Printf("docs-contract-refs: PASS (PARTIAL) — %d normative reference(s) resolve (%d allowlisted with a sunset); "+
				"%d reference(s) into the ecosystem kit NOT verified: the kit is not installed on this checkout\n",
				out.Checked, out.Allowlisted, len(out.NotVerified))
			return out, nil
		}
		inv.Stdout.Printf("docs-contract-refs: PASS — %d normative reference(s) resolve (%d allowlisted with a sunset)\n",
			out.Checked, out.Allowlisted)
		return out, nil
	}
}
