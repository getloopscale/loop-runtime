// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/englishonly"
)

func init() {
	register(Gate{
		Name:    "english-only",
		Summary: "Everything git tracks is written in English; a line keeps Portuguese only with an inline exemption that states its reason.",
		Setup:   setupEnglishOnly,
	})
}

// setupEnglishOnly refuses Portuguese in any tracked text file (#380).
func setupEnglishOnly(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		r, err := englishonly.Check(englishonly.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if len(r.Findings) > 0 {
			inv.Stderr.Printf("%s: FAIL — %d line(s) in %d scanned file(s) are not English:\n", inv.Name, len(r.Findings), r.Files)
			for _, f := range r.Findings {
				inv.Stderr.Printf("  %s\n", f)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("To fix: translate the line. Keep Portuguese only where its language is a")
			inv.Stderr.Println("requirement — product copy, a verbatim quotation, a fixture that must be")
			inv.Stderr.Println("Portuguese — and say why on the line itself: the word english-only, a colon,")
			inv.Stderr.Println("and a reason of at least five words.")
			return Fail, nil
		}
		// The gate name comes from the invocation: written literally, it would read
		// as an exemption marker to the gate itself.
		inv.Stdout.Printf("%s: PASS — %d tracked text file(s) scanned, %d generated skipped, %d exempted line(s) with a reason\n",
			inv.Name, r.Files, r.Generated, r.Exempted)
		return Pass, nil
	}
}
