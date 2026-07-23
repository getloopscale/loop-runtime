// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/advisorylock"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/crdsemanticdiff"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/dbgeneratedboundary"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/ma7statuslocality"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/migrationtestcoverage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/nomigrateforce"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/renderschemagensql"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/sqlcsync"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/sqlctxprop"
)

// M9 Step 7: quality gate entry points for the "db" theme, decomposed out of the
// former quality_subcmd.go god file. Each func is referenced as a Run in
// gateRegistry (quality_registry.go) — the single source of truth
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism).

// runAdvisoryLockScope is the Go home for the advisory-lock-scope gate (migrated
// from infra/tests/scripts/check-advisory-lock-scope_test.sh). Forbids
// session-scoped pg_advisory_lock( in production code (CLAUDE.md Rule 7).
func runAdvisoryLockScope(args []string) int {
	fs := flag.NewFlagSet("advisory-lock-scope", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := advisorylock.Check(advisorylock.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "advisory-lock-scope: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "advisory-lock-scope: FAIL — %d session-scoped pg_advisory_lock( call(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Fix: replace with pg_advisory_xact_lock( — transaction-scoped, pool-safe (Rule 7).")
		return 1
	}
	fmt.Println("advisory-lock-scope: PASS — no session-scoped pg_advisory_lock( in production code")
	return 0
}

// runMigrationTestCoverage is the gate replacing migration_test_coverage.sh:
// every DB migration must have a regression test. Pure-logic, hermetic.
func runMigrationTestCoverage(args []string) int {
	fs := flag.NewFlagSet("migration-test-coverage", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	migrationsDir := fs.String("migrations-dir", "", "override migrations dir")
	testsDir := fs.String("tests-dir", "", "override unit tests dir")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := migrationtestcoverage.Check(migrationtestcoverage.Config{
		Root:          *root,
		MigrationsDir: *migrationsDir,
		UnitTestsDir:  *testsDir,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration-test-coverage: %v\n", err)
		return 5
	}
	for _, m := range outcome.Messages {
		fmt.Println(m)
	}
	if !outcome.Pass {
		return 1
	}
	return 0
}

// runCRDSemanticDiff is the gate replacing crd-semantic-diff.sh: detects
// semantic drift between source CRDs and the rendered loopzero-crds chart.
// Shells out to `helm template` directly (no shared abstraction); unit tests
// are hermetic via the override hook.
func runCRDSemanticDiff(args []string) int {
	fs := flag.NewFlagSet("crd-semantic-diff", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	crd := fs.String("crd", "", "limit to a single CRD short-name (e.g. tenants)")
	verbose := fs.Bool("verbose", false, "append inline diff snippets")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := crdsemanticdiff.Check(crdsemanticdiff.Config{
		Root:      *root,
		CRDFilter: *crd,
		Verbose:   *verbose,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "crd-semantic-diff: %v\n", err)
		return 5
	}
	for _, m := range outcome.Messages {
		fmt.Println(m)
	}
	if !outcome.Pass {
		return 1
	}
	return 0
}

func runNoMigrateForce(args []string) int {
	fs := flag.NewFlagSet("no-migrate-force", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := nomigrateforce.Check(nomigrateforce.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-migrate-force: %v\n", err)
		return 1
	}
	if !o.Pass {
		// allowlist: migrate force (this is the gate's own diagnostic message)
		msg := fmt.Sprintf("%d unallowlisted occurrence(s) of 'migrate force' in production code. "+
			"Per CORRECTIONS.txt item B, forcing a migration on a production DB corrupts "+
			"schema_migrations. Use psql for ad-hoc SQL, OR add a '# allowlist: migrate force "+
			"(rationale)' marker on the same line or within 3 lines above.", len(o.Blocked))
		if os.Getenv("GITHUB_ACTIONS") != "" {
			fmt.Printf("::error::%s\n", msg)
		} else {
			fmt.Fprintf(os.Stderr, "[no-migrate-force] BLOCK: %s\n", msg)
		}
		fmt.Fprintln(os.Stderr, "[no-migrate-force] blocked matches:")
		for _, b := range o.Blocked {
			fmt.Fprintf(os.Stderr, "  %s\n", b)
		}
		return 2 // contract: exit 2 for violations (matches legacy bash gate)
	}
	if o.MatchCount > 0 {
		fmt.Printf("[no-migrate-force] all %d matches allowlisted — OK.\n", o.MatchCount)
	} else {
		fmt.Println("[no-migrate-force] no matches — OK.")
	}
	return 0
}

// runSqlcSync verifies committed api/internal/db/generated/ is in-sync with a
// fresh `sqlc generate`. T0.3 of plan 2026-05-29-sqlc-migration v1.2.
// Successor gate to schema_mirror_test.go (deleted in T4.2 per D7 two-step
// after this gate goes green for one CI cycle).
func runSqlcSync(args []string) int {
	fs := flag.NewFlagSet("sqlc-sync", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := sqlcsync.Check(sqlcsync.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sqlc-sync: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "sqlc-sync: FAIL — %d violation(s) between committed generated/ and fresh sqlc generate (version %s):\n",
			len(outcome.Violations), outcome.SqlcVersion)
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  [%s] %s\n", v.Reason, v.File)
			if v.Detail != "" {
				fmt.Fprintf(os.Stderr, "%s\n", v.Detail)
			}
		}
		fmt.Fprintf(os.Stderr, "\nFix: %s\n", sqlcsync.ActionableFix())
		return 1
	}
	fmt.Printf("sqlc-sync: PASS — committed generated/ in-sync with sqlc %s\n", outcome.SqlcVersion)
	return 0
}

// runSqlcTxProp enforces that sqlc-generated calls inside withEnvironmentLock
// use the WithTx(tx) form. T0.4 of plan 2026-05-29-sqlc-migration v1.2.
// Per MEET-2026-05-29-001 distributed-systems-engineer R-DS-1: pool-bound
// form risks bypassing the lock holder's transaction. Rule 6 + Rule 7
// invariant preservation.
func runSqlcTxProp(args []string) int {
	fs := flag.NewFlagSet("sqlc-tx-prop", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := sqlctxprop.Check(sqlctxprop.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sqlc-tx-prop: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "sqlc-tx-prop: FAIL — %d violation(s) (scanned %d .go files):\n",
			len(outcome.Violations), outcome.Scanned)
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s:%d  %s  — %s\n", v.File, v.Line, v.Function, v.Detail)
		}
		fmt.Fprintf(os.Stderr, "\nFix: %s\n", sqlctxprop.ActionableFix())
		return 1
	}
	fmt.Printf("sqlc-tx-prop: PASS — %d .go files scanned; no pool-bound sqlc calls inside withEnvironmentLock\n", outcome.Scanned)
	return 0
}

// runDbGeneratedBoundary enforces MA-7 Gate A of the M9 db-split: no file under
// api/internal/{services,routes,server} imports api/internal/db/generated.
// ADR .claude/knowledge-base/decisions/2026-07-03-m9-db-store-subpackage-topology.md D3.
func runDbGeneratedBoundary(args []string) int {
	fs := flag.NewFlagSet("db-generated-boundary", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := dbgeneratedboundary.Check(dbgeneratedboundary.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "db-generated-boundary: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "db-generated-boundary: FAIL \u2014 %d violation(s) (scanned %d .go files):\n",
			len(outcome.Violations), outcome.Scanned)
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s:%d imports %s\n", v.File, v.Line, v.Import)
		}
		fmt.Fprintf(os.Stderr, "\nFix: %s\n", dbgeneratedboundary.ActionableFix())
		return 1
	}
	for _, d := range outcome.MissingDirs {
		fmt.Fprintf(os.Stderr, "db-generated-boundary: WARN \u2014 fenced dir %q absent from tree (a rename may have silently dropped a fence)\n", d)
	}
	fmt.Printf("db-generated-boundary: PASS \u2014 %d .go files scanned; no outer-layer imports of db/generated (MA-7 Gate A)\n", outcome.Scanned)
	return 0
}

// runMa7StatusLocality enforces MA-7 Gates B+C of the M9 db-split: the sqlc
// status UPDATE funcs + <machine>.SafeTransition are referenced only from
// api/internal/db/store/. ADR 2026-07-03-m9-db-store-subpackage-topology D3.
func runMa7StatusLocality(args []string) int {
	fs := flag.NewFlagSet("ma7-status-locality", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := ma7statuslocality.Check(ma7statuslocality.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ma7-status-locality: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "ma7-status-locality: FAIL \u2014 %d violation(s) (scanned %d .go files):\n", len(outcome.Violations), outcome.Scanned)
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s:%d Gate %s \u2014 %s outside db/store/\n", v.File, v.Line, v.Gate, v.Name)
		}
		fmt.Fprintf(os.Stderr, "\nFix: %s\n", ma7statuslocality.ActionableFix())
		return 1
	}
	fmt.Printf("ma7-status-locality: PASS \u2014 %d .go files scanned; status machinery confined to db/store/ (MA-7 Gates B+C)\n", outcome.Scanned)
	return 0
}

// runRenderSchemaGenSQL renders api/internal/db/schema.gen.sql via
// an ephemeral PG 16 container + golang-migrate replay + pg_dump + sqlc
// compile pre-flight. Use --render for the full destructive pipeline; the
// default check-only mode validates toolchain + config presence.
// Migrated from infra/scripts/ci/render-schema-gen-sql.sh (M-Migrate2 continuation 2026-07-07).
func runRenderSchemaGenSQL(args []string) int {
	fs := flag.NewFlagSet("render-schema-gen-sql", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	render := fs.Bool("render", false, "run the destructive render pipeline (spins docker + rewrites schema.gen.sql); default is check-only")
	advisoryOnly := fs.Bool("advisory-only", false, "return Warn instead of failure when a required tool is missing")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	cfg := renderschemagensql.Config{RepoRoot: *root, AdvisoryOnly: *advisoryOnly}
	if !*render {
		outcome, err := renderschemagensql.Check(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "render-schema-gen-sql: FAIL — %v\n", err)
			return 1
		}
		if outcome.Warn {
			fmt.Printf("render-schema-gen-sql: WARN (%s) — pass --advisory-only=false to convert to hard fail once toolchain lands\n", outcome.WarnReason)
			return 0
		}
		fmt.Println("render-schema-gen-sql: PASS — toolchain + migrations + sqlc.yaml present; pass --render to execute the pipeline")
		return 0
	}
	outcome, err := renderschemagensql.Render(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "render-schema-gen-sql: FAIL — %v\n%s\n", err, outcome.Diagnostic)
		return 1
	}
	if outcome.Warn {
		fmt.Printf("render-schema-gen-sql: WARN (%s)\n", outcome.WarnReason)
		return 0
	}
	fmt.Printf("render-schema-gen-sql: PASS — schema.gen.sql rendered (%d lines) + sqlc compile clean\n", outcome.SchemaLines)
	return 0
}
