// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/featurefence"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "feature-fence",
		Summary: "Deny-by-default fence over api/internal by feature: registered features, kernel and composition roots; HTTP and adapter packages stay inside their feature.",
		Setup:   setupFeatureFence,
	})
}

// setupFeatureFence enforces the package-by-feature layout of api/internal
// (docs/decisions.md, 2026-09-25).
func setupFeatureFence(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		o, err := featurefence.Check(featurefence.Config{RepoRoot: inv.Root})
		return reportSubpackageFence(inv, o, err, featurefence.ActionableFix())
	}
}
