// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmdiffstructural"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "helm-diff-structural",
		Summary: "Structural pre-sync Helm diff to catch drift before apply.",
		// --base and --pr are required (quality_gates_helm.go:93)
		Wiring: parameterized,
		NoRoot: true,
		Setup:  setupHelmDiffStructural,
	})
}

// setupHelmDiffStructural is the gate replacing helm-diff-structural.sh: fails
// CI only on STRUCTURALLY RISKY chart diffs (security-resource removal,
// namespace change, security-context downgrade). Shells out to `diff -u`.
func setupHelmDiffStructural(fs *flag.FlagSet) Check {
	chart := fs.String("chart", "", "chart name (diagnostics)")
	base := fs.String("base", "", "path to base rendered YAML")
	pr := fs.String("pr", "", "path to PR rendered YAML")
	return func(inv Invocation) (verdict.Result, error) {
		if *base == "" || *pr == "" {
			return nil, usageError("--base and --pr are required")
		}

		outcome, err := helmdiffstructural.Check(helmdiffstructural.Config{
			Chart:    *chart,
			BasePath: *base,
			PRPath:   *pr,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range outcome.Messages {
			inv.Stdout.Println(m)
		}
		return outcome, nil
	}
}
