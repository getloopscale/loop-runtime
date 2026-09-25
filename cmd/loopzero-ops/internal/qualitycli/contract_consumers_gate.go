// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/contractconsumers"
)

func init() {
	register(Gate{
		Name:    "contract-consumers",
		Summary: "Verifies cross-repo contract consumers stay in sync with the contract.",
		Setup:   setupContractConsumers,
	})
}

// setupContractConsumers detects backend response shape narrowing (Go struct
// field with `json:"<tag>"` REMOVED in the PR diff) when at least one
// consumer in cloud/dashboard/src/, cli/src/, or api/internal/ still reads
// that field. See cmd/loopzero-ops/internal/quality/contractconsumers/check.go package doc for
// scope + non-scope + plan rationale. Sponsor mandate 2026-06-08: this
// gate REDUCES the bug rate that reaches the sponsor walkthrough; sponsor
// walkthrough remains the final visual gate.
func setupContractConsumers(fs *flag.FlagSet) Check {
	baseRef := fs.String("base-ref", "origin/develop", "git ref the PR branches from")
	headRef := fs.String("head-ref", "HEAD", "git ref of the PR HEAD")
	return func(inv Invocation) (Outcome, error) {
		outcome, err := contractconsumers.Check(contractconsumers.Config{
			RepoRoot: inv.Root,
			BaseRef:  *baseRef,
			HeadRef:  *headRef,
		})
		if err != nil {
			return Fail, err
		}
		// Lost coverage is ALWAYS printed — including on PASS, which is where it would
		// vanish. A consumer root that is absent (usually a sibling checkout that is
		// not cloned) makes the verdict PARTIAL; it does not fail the gate on its own.
		if outcome.Partial {
			inv.Stderr.Printf("contract-consumers: PARTIAL — %d of %d consumer root(s) do not exist "+
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
			inv.Stderr.Printf("contract-consumers: FAIL — %d of %d tracked path(s) do not exist "+
				"in this tree: %v\n", len(outcome.MissingTracked), len(outcome.TrackedFiles),
				outcome.MissingTracked)
			inv.Stderr.Printf("  git diff/grep silently ignore a pathspec matching nothing, so this " +
				"gate would be reporting PASS without reading that surface.\n" +
				"  Fix: point trackedFiles in cmd/loopzero-ops/internal/quality/contractconsumers/check.go " +
				"at the new path.\n")
			return Fail, nil
		}
		if outcome.CommandError != "" {
			return Fail, fmt.Errorf("git/grep: %s", outcome.CommandError)
		}
		// Acknowledged drops are printed on every run: a decision recorded in
		// the registry is still a contract change a reviewer must see.
		if len(outcome.Acknowledged) > 0 {
			inv.Stderr.Printf("contract-consumers: %d dropped tag(s) acknowledged in %s (not judged by consumers): %v\n",
				len(outcome.Acknowledged), contractconsumers.DefaultAcknowledgementsFile, outcome.Acknowledged)
		}
		if len(outcome.StaleAcknowledgements) > 0 {
			inv.Stderr.Printf("contract-consumers: FAIL — %d acknowledgement(s) match no drop between %s and %s: %v\n"+
				"  Remove them: an acknowledgement that outlived its drop pre-approves the next removal of that tag from that file.\n",
				len(outcome.StaleAcknowledgements), *baseRef, *headRef, outcome.StaleAcknowledgements)
			return Fail, nil
		}
		if !outcome.Pass {
			inv.Stderr.Printf("contract-consumers: FAIL — %d consumer reference(s) to dropped JSON tag(s) (between %s and %s):\n",
				len(outcome.Findings), *baseRef, *headRef)
			inv.Stderr.Print(contractconsumers.FormatFindings(outcome.Findings))
			inv.Stderr.Println("")
			inv.Stderr.Printf("Fix:\n  %s\n", contractconsumers.ActionableFix())
			return Fail, nil
		}
		verdict := "PASS"
		if outcome.Partial {
			verdict = "PASS (PARTIAL)"
		}
		if len(outcome.DroppedTags) == 0 {
			inv.Stdout.Printf("contract-consumers: %s — no JSON tag drops in tracked files\n", verdict)
		} else {
			inv.Stdout.Printf("contract-consumers: %s — %d JSON tag(s) dropped + ZERO consumer reads found: %v\n",
				verdict, len(outcome.DroppedTags), outcome.DroppedTags)
		}
		return Pass, nil
	}
}
