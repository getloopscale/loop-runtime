// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmdrift"
)

func init() {
	register(Gate{
		Name:    "helm-drift",
		Summary: "Compares the two sides of each declared chart pair: the resource KINDS each installs, and the (apiGroup, resource, verb) RBAC cells each grants (B-001).",
		Setup:   setupHelmDrift,
	})
}

func setupHelmDrift(fs *flag.FlagSet) Check {
	format := fs.String("format", "human", "output format: human | github-annotations | json")
	return func(inv Invocation) (Outcome, error) {
		render, ok := driftRenderers[*format]
		if !ok {
			return Fail, usageError("unknown --format %q (use human | github-annotations | json)", *format)
		}
		o, err := helmdrift.Check(helmdrift.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if err := render(inv.Stdout, inv.Stderr, o); err != nil {
			return Fail, err
		}
		if !o.Pass {
			return Fail, nil
		}
		return Pass, nil
	}
}
