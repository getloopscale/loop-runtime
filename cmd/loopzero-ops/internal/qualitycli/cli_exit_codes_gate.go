// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/cliexitcodes"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "cli-exit-codes",
		Summary: "Enforces the permanent CLI exit-code contract (0/1/2/3/4/5 only).",
		Setup:   setupCLIExitCodes,
	})
}

// setupCLIExitCodes is the Go home for the cli-exit-codes gate (migrated from
// infra/tests/scripts/check-cli-exit-codes_test.sh). Enforces Rule 4: a CLI
// process.exit() code is 0-5 only (130/143 allowed only in SIG handlers).
//
// Verdicts:
//
//   - PASS      a CLI source tree was found and walked, and every exit code is
//     in contract. The line names the tree it walked and the file count, so
//     "it passed" is separable from "it read nothing".
//
//   - PARTIAL   no CLI source tree exists and the absence is declared in
//     infra/quality/cli-exit-codes-subject.txt. Exit 0 — a known gap that turns
//     the chain red forever teaches that red is normal — but the word PASS is
//     never printed, and both the tracked absence and what it leaves unchecked
//     go to stdout on EVERY run. That is the B-181 shape, for the same reason:
//     a gap nobody prints is indistinguishable from no gap.
//
//   - FAIL      an exit code is out of contract; or no source tree exists and
//     nothing declares that; or a declared absence has CLOSED — the pinned
//     SUBJECT path itself exists, or a CLI source tree turned up, and the pin
//     still says it is gone. Both halves are measured on every run; before
//     B-190 only the second one was, so a line could name a directory sitting
//     right here and still print as an absence.
func setupCLIExitCodes(fs *flag.FlagSet) Check {
	cliRoot := fs.String("loopzero-cli", os.Getenv("LOOPZERO_CLI_ROOT"),
		"standalone loopzero-cli checkout (default $LOOPZERO_CLI_ROOT, else <root>/../loopzero-cli)")
	pin := fs.String("pin", cliexitcodes.DefaultPinFile,
		"declared-absence pin, relative to --root")
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := cliexitcodes.Check(cliexitcodes.Config{
			RepoRoot: inv.Root,
			CLIRoot:  *cliRoot,
			PinFile:  *pin,
		})
		if err != nil {
			return nil, err
		}

		// The tracked absence goes to stdout on every run — that is what turns a
		// silent SKIP into an absence somebody can read — and it names the path
		// that was stat'd this run to produce it. The sentence it replaces ("… is
		// not in this tree and not in any checkout beside it") was printed without
		// the subject ever being stat'd, so it was false for any subject that was
		// in fact right here (B-190).
		for _, a := range outcome.TrackedAbsences {
			inv.Stdout.Printf("cli-exit-codes: TRACKED ABSENCE — %s: stat'd at %s on this run and NOT present: %s\n",
				a.Subject, a.ResolvedPath, a.Why)
		}

		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("cli-exit-codes: FAIL — %d violation(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Permanent contract (Rule 4): 0/1/2/3/4/5 only. SIG handlers may use 130/143.")
			inv.Stderr.Printf("When there is no CLI source to read, %s must say so in one line:\n", *pin)
			inv.Stderr.Println("  <subject path>\t<why it is absent, naming what covers the contract instead>")
			return outcome, nil
		}

		// `!Checked` is "no subject found"; `ScannedFiles == 0` is "found and read
		// nothing", and the second used to fall through to PASS. Both are the same fact
		// for a reader: the contract was not checked.
		if !outcome.Checked || outcome.ScannedFiles == 0 {
			inv.Stdout.Printf("cli-exit-codes: NOT COVERED — no process.exit() call reachable from this tree is checked "+
				"against Rule 4, and no other gate enforces the exit-code contract; it holds only where "+
				"loopzero-cli's own CI runs it, which is UNVERIFIED from this tree (searched: %s)\n",
				strings.Join(outcome.SearchedPaths, ", "))
			where := "no CLI source tree was found"
			if outcome.Checked {
				where = fmt.Sprintf("%s exists and held no file this gate could read", outcome.ScannedRoot)
			}
			inv.Stdout.Printf("cli-exit-codes: PARTIAL — 0 file(s) scanned, exit-code contract NOT CHECKED "+
				"(%s; %d tracked absence(s))\n", where, len(outcome.TrackedAbsences))
			return outcome, nil
		}

		inv.Stdout.Printf("cli-exit-codes: PASS — %d file(s) under %s, no literal decimal process.exit() outside 0-5 "+
			"(signal handlers exempt)\n", outcome.ScannedFiles, outcome.ScannedRoot)
		return outcome, nil
	}
}
