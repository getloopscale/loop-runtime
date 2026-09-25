// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/lokiretentionschema"
)

func init() {
	register(Gate{
		Name:    "loki-retention-schema",
		Summary: "Trio-compat regression for T8.2 Loki retention_stream schema: infra/helm/loki/values.yaml parses cleanly + deploymentMode SingleBinary (cleanroom-compatible) + exactly 1 retention_stream entry + selector secrets-system + period 2160h + priority int (Loki startup schema) + default retention_period 168h. 7 assertions. Uses gopkg.in/yaml.v3 (already-vendored, per plan D2 parsimony rung 4). M-Migrate2 2026-07-10 M16 T3.2 Wave 2 replaces infra/tests/check-loki-retention-schema_test.sh.",
		Setup:   setupLokiRetentionSchema,
	})
}

// setupLokiRetentionSchema is the trio-compat regression for the retention_stream
// schema in infra/helm/loki/values.yaml. 7 assertions.
// Migrated from infra/tests/check-loki-retention-schema_test.sh
// (M-Migrate2 2026-07-10 M16 T3.2 Wave 2).
func setupLokiRetentionSchema(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := lokiretentionschema.Check(lokiretentionschema.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, lokiretentionschema.ErrViolations) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "loki-retention-schema: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				fmt.Fprintf(inv.Stderr, "  %s\n", f)
			}
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "loki-retention-schema: PASS — 7 schema assertions (SingleBinary + retention_stream secrets-system + priority int + defaults) clean")
		return Pass, nil
	}
}
