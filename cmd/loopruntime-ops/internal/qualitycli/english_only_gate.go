// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/englishonly"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "english-only",
		Summary: "Everything git tracks is written in English; a line keeps Portuguese only with an inline exemption that states its reason.",
		Setup:   setupEnglishOnly,
	})
}

// setupEnglishOnly refuses Portuguese in any tracked text file, Go identifier
// or tracked path name (#380).
func setupEnglishOnly(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		r, err := englishonly.Check(englishonly.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if r.Verdict() == verdict.Fail {
			inv.Stderr.Printf("%s: FAIL — %d finding(s) in %d scanned file(s) are not English:\n", inv.Name, len(r.Findings), r.Files)
			for _, f := range r.Findings {
				inv.Stderr.Printf("  %s\n", f)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("To fix: translate the line, or rename the identifier, file or directory")
			inv.Stderr.Println("and every reference to it. Keep Portuguese only where its language is a")
			inv.Stderr.Println("requirement — product copy, a verbatim quotation, a fixture that must be")
			inv.Stderr.Println("Portuguese — and say why on the line itself: the word english-only, a colon,")
			inv.Stderr.Println("and a reason of at least five words.")
			return r, nil
		}
		// The gate name comes from the invocation: written literally, it would read
		// as an exemption marker to the gate itself.
		inv.Stdout.Printf("%s: PASS — %d tracked text file(s) scanned, %d generated skipped, %d exempted line(s) with a reason\n",
			inv.Name, r.Files, r.Generated, r.Exempted)
		return r, nil
	}
}
