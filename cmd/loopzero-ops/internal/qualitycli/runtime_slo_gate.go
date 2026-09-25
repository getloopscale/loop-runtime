// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/runtimeslo"
)

func init() {
	register(Gate{
		Name:    "runtime-slo",
		Summary: "Validates runtime SLO definitions and thresholds.",
		Wiring: operatorRunOn(
			"/tmp/go-test-output.log, produced by a previous `go test` run — absent in a bare repo scan, where the gate SKIPs"),
		NoRoot: true,
		Setup:  setupRuntimeSLO,
	})
}

func setupRuntimeSLO(fs *flag.FlagSet) Check {
	logFile := fs.String("log", "/tmp/go-test-output.log", "go test -v output log")
	return func(inv Invocation) (Outcome, error) {
		o, err := runtimeslo.Check(runtimeslo.Config{LogFile: *logFile, Limits: runtimeslo.DefaultLimits()})
		if err != nil {
			return Fail, err
		}
		if o.Skipped {
			fmt.Fprintf(inv.Stdout, "[runtime-slo] no go test log at %s — skip\n", *logFile)
			return Pass, nil
		}
		if !o.Pass {
			fmt.Fprintf(inv.Stderr, "[runtime-slo] FAIL: %d test(s)/suite(s) over SLO:\n", len(o.Violations))
			for _, v := range o.Violations {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "[runtime-slo] for legitimate slow tests, add: // slow_test: <issue-url>")
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "[runtime-slo] PASS — all tests/suites within SLO")
		return Pass, nil
	}
}
