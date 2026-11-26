// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/runtimeslo"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		o, err := runtimeslo.Check(runtimeslo.Config{LogFile: *logFile, Limits: runtimeslo.DefaultLimits()})
		if err != nil {
			return nil, err
		}
		if o.Verdict() == verdict.Skip {
			inv.Stdout.Printf("[runtime-slo] no go test log at %s — skip\n", *logFile)
			return o, nil
		}
		if o.Verdict() == verdict.Fail {
			inv.Stderr.Printf("[runtime-slo] FAIL: %d test(s)/suite(s) over SLO:\n", len(o.Violations))
			for _, v := range o.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("[runtime-slo] for legitimate slow tests, add: // slow_test: <issue-url>")
			return o, nil
		}
		inv.Stdout.Println("[runtime-slo] PASS — all tests/suites within SLO")
		return o, nil
	}
}
