// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"
	"os"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/costseriesproducers"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "cost-series-producers",
		Summary: "B-181: every loop_runtime_* series the loop-console cost aggregator queries has a declared producer here, or a tracked absence.",
		Setup:   setupCostSeriesProducers,
	})
}

// setupCostSeriesProducers is the B-181 gate: every `loop_runtime_*` series loop-console's
// cost aggregator queries must have a declared answer in this repo — a producing
// file, or a tracked absence naming the work item.
//
// It prints THREE verdicts, never fewer:
//
//   - PASS      the producer contract holds AND the declaration was compared
//     against loop-console's promQueries map.
//
//   - PARTIAL   the producer contract holds; loop-console is not on disk, so the
//     drift comparison did NOT run. Never printed as PASS: the gate
//     does not report a result it did not observe (B-169).
//
//     PARTIAL EXITS 0. The word is for whoever reads the log; the
//     chain does not stop, and inside 140 gates that makes it
//     operationally indistinguishable from a pass. The drift half is
//     therefore opt-in by environment and its absence is free — and
//     the default sibling path is the ABSENT case on any checkout
//     that does not sit beside loop-console, which is most of them.
//     Making PARTIAL exit non-zero would fail the chain on all of
//     those, so it is a decision with a cost rather than an
//     oversight; what is fixed here is the comment claiming more
//     than the exit code delivers.
//
//   - FAIL      a series is queried and undeclared, a declared producer does not
//     emit what it claims, or a tracked absence has quietly closed.
//
// Tracked absences are printed on EVERY run, pass or not. A known gap that is
// silent is indistinguishable from no gap; a known gap that turns the gate red
// forever teaches that red is normal. Naming it is the third option.
func setupCostSeriesProducers(fs *flag.FlagSet) Check {
	sibling := fs.String("loop-console", os.Getenv("LOOP_CONSOLE_ROOT"),
		"loop-console checkout (default $LOOP_CONSOLE_ROOT, else <root>/../loop-console)")
	declaration := fs.String("declaration", costseriesproducers.DefaultDeclarationFile,
		"pinned series/producer contract, relative to --root")
	aggregator := fs.String("aggregator", costseriesproducers.DefaultAggregatorFile,
		"cost aggregator source, relative to --loop-console")
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := costseriesproducers.Check(costseriesproducers.Config{
			RepoRoot:        inv.Root,
			SiblingRoot:     *sibling,
			DeclarationFile: *declaration,
			AggregatorFile:  *aggregator,
		})
		if err != nil {
			return nil, err
		}

		// The known, tracked gap goes to stdout on every run — that is what turns
		// silence into a tracked absence.
		for _, a := range outcome.TrackedAbsences {
			inv.Stdout.Printf("cost-series-producers: TRACKED ABSENCE — %s is queried by the aggregator and emitted by no repository\n", a)
		}

		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("cost-series-producers: FAIL — %d violation(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Printf("The pin is %s. Every series the aggregator queries needs one line there:\n", *declaration)
			inv.Stderr.Println("  <series> PRODUCER <path in this repo that declares the metric>")
			inv.Stderr.Println("  <series> ABSENT   <work item tracking who will emit it, or the decision to drop it>")
			return outcome, nil
		}

		if !outcome.SiblingChecked {
			inv.Stderr.Printf("cost-series-producers: DRIFT NOT CHECKED — %s\n", outcome.SiblingSkipReason)
			inv.Stdout.Printf("cost-series-producers: PARTIAL — producer contract holds for %d declared series "+
				"(%d producer(s) verified, %d tracked absence(s)); drift vs loop-console NOT CHECKED\n",
				len(outcome.Declarations), len(outcome.VerifiedProducers), len(outcome.TrackedAbsences))
			return outcome, nil
		}

		inv.Stdout.Printf("cost-series-producers: PASS — %d series queried by loop-console, all declared "+
			"(%d producer(s) verified, %d tracked absence(s)); drift CHECKED against %s\n",
			len(outcome.QueriedSeries), len(outcome.VerifiedProducers), len(outcome.TrackedAbsences),
			outcome.SiblingRoot)
		return outcome, nil
	}
}
