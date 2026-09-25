// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/costseriesproducers"
)

func init() {
	register(Gate{
		Name:    "cost-series-producers",
		Summary: "B-181: every loopzero_* series the loopzero-cloud cost aggregator queries has a declared producer here, or a tracked absence.",
		Setup:   setupCostSeriesProducers,
	})
}

// setupCostSeriesProducers is the B-181 gate: every `loopzero_*` series loopzero-cloud's
// cost aggregator queries must have a declared answer in this repo — a producing
// file, or a tracked absence naming the work item.
//
// It prints THREE verdicts, never fewer:
//
//   - PASS      the producer contract holds AND the declaration was compared
//     against loopzero-cloud's promQueries map.
//
//   - PARTIAL   the producer contract holds; loopzero-cloud is not on disk, so the
//     drift comparison did NOT run. Never printed as PASS: the gate
//     does not report a result it did not observe (B-169).
//
//     PARTIAL EXITS 0. The word is for whoever reads the log; the
//     chain does not stop, and inside 140 gates that makes it
//     operationally indistinguishable from a pass. The drift half is
//     therefore opt-in by environment and its absence is free — and
//     the default sibling path is the ABSENT case on any checkout
//     that does not sit beside loopzero-cloud, which is most of them.
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
	sibling := fs.String("loopzero-cloud", os.Getenv("LOOPZERO_CLOUD_ROOT"),
		"loopzero-cloud checkout (default $LOOPZERO_CLOUD_ROOT, else <root>/../loopzero-cloud)")
	declaration := fs.String("declaration", costseriesproducers.DefaultDeclarationFile,
		"pinned series/producer contract, relative to --root")
	aggregator := fs.String("aggregator", costseriesproducers.DefaultAggregatorFile,
		"cost aggregator source, relative to --loopzero-cloud")
	return func(inv Invocation) (Outcome, error) {
		outcome, err := costseriesproducers.Check(costseriesproducers.Config{
			RepoRoot:        inv.Root,
			SiblingRoot:     *sibling,
			DeclarationFile: *declaration,
			AggregatorFile:  *aggregator,
		})
		if err != nil {
			return Fail, err
		}

		// The known, tracked gap goes to stdout on every run — that is what turns
		// silence into a tracked absence.
		for _, a := range outcome.TrackedAbsences {
			fmt.Fprintf(inv.Stdout, "cost-series-producers: TRACKED ABSENCE — %s is queried by the aggregator and emitted by no repository\n", a)
		}

		if !outcome.Pass {
			fmt.Fprintf(inv.Stderr, "cost-series-producers: FAIL — %d violation(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintf(inv.Stderr, "The pin is %s. Every series the aggregator queries needs one line there:\n", *declaration)
			fmt.Fprintln(inv.Stderr, "  <series> PRODUCER <path in this repo that declares the metric>")
			fmt.Fprintln(inv.Stderr, "  <series> ABSENT   <work item tracking who will emit it, or the decision to drop it>")
			return Fail, nil
		}

		if !outcome.SiblingChecked {
			fmt.Fprintf(inv.Stderr, "cost-series-producers: DRIFT NOT CHECKED — %s\n", outcome.SiblingSkipReason)
			fmt.Fprintf(inv.Stdout, "cost-series-producers: PARTIAL — producer contract holds for %d declared series "+
				"(%d producer(s) verified, %d tracked absence(s)); drift vs loopzero-cloud NOT CHECKED\n",
				len(outcome.Declarations), len(outcome.VerifiedProducers), len(outcome.TrackedAbsences))
			return Pass, nil
		}

		fmt.Fprintf(inv.Stdout, "cost-series-producers: PASS — %d series queried by loopzero-cloud, all declared "+
			"(%d producer(s) verified, %d tracked absence(s)); drift CHECKED against %s\n",
			len(outcome.QueriedSeries), len(outcome.VerifiedProducers), len(outcome.TrackedAbsences),
			outcome.SiblingRoot)
		return Pass, nil
	}
}
