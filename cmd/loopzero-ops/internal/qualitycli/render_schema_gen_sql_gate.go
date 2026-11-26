// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/renderschemagensql"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "render-schema-gen-sql",
		Summary: "T0.2 sqlc schema render pipeline: ephemeral PG16 + golang-migrate replay + pg_dump inside container + post-process (drop ALTER OWNER TO, banners, SET, restrict, orphan --, collapse blanks, trim trailing WS) + sqlc compile pre-flight with throwaway probe. Default check-only mode validates toolchain + migrations dir. Pass --render for destructive pipeline. M-Migrate2 2026-07-07 replaces infra/scripts/ci/render-schema-gen-sql.sh.",
		Setup:   setupRenderSchemaGenSQL,
	})
}

// setupRenderSchemaGenSQL renders api/internal/db/schema.gen.sql via
// an ephemeral PG 16 container + golang-migrate replay + pg_dump + sqlc
// compile pre-flight. Use --render for the full destructive pipeline; the
// default check-only mode validates toolchain + config presence.
// Migrated from infra/scripts/ci/render-schema-gen-sql.sh (M-Migrate2 continuation 2026-07-07).
func setupRenderSchemaGenSQL(fs *flag.FlagSet) Check {
	render := fs.Bool("render", false, "run the destructive render pipeline (spins docker + rewrites schema.gen.sql); default is check-only")
	advisoryOnly := fs.Bool("advisory-only", false, "return Warn instead of failure when a required tool is missing")
	return func(inv Invocation) (verdict.Result, error) {
		cfg := renderschemagensql.Config{RepoRoot: inv.Root, AdvisoryOnly: *advisoryOnly}
		if !*render {
			outcome, err := renderschemagensql.Check(cfg)
			if err != nil {
				return nil, err
			}
			if outcome.Verdict() == verdict.Skip {
				inv.Stdout.Printf("render-schema-gen-sql: WARN (%s) — pass --advisory-only=false to convert to hard fail once toolchain lands\n", outcome.WarnReason)
				return outcome, nil
			}
			inv.Stdout.Println("render-schema-gen-sql: PASS — toolchain + migrations + sqlc.yaml present; pass --render to execute the pipeline")
			return outcome, nil
		}
		outcome, err := renderschemagensql.Render(cfg)
		if err != nil {
			// The pipeline could not run: the runner reports the error and exits
			// Incomplete. The tool output is what explains it.
			if outcome.Diagnostic != "" {
				inv.Stderr.Println(outcome.Diagnostic)
			}
			return nil, err
		}
		if outcome.Verdict() == verdict.Skip {
			inv.Stdout.Printf("render-schema-gen-sql: WARN (%s)\n", outcome.WarnReason)
			return outcome, nil
		}
		inv.Stdout.Printf("render-schema-gen-sql: PASS — schema.gen.sql rendered (%d lines) + sqlc compile clean\n", outcome.SchemaLines)
		return outcome, nil
	}
}
