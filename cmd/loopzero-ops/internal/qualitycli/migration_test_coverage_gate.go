// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/migrationtestcoverage"
)

func init() {
	register(Gate{
		Name:    "migration-test-coverage",
		Summary: "Every DB migration has corresponding test coverage.",
		// 44 migrations with no matching api/tests/unit/migration_*_test.go
		// (re-measured 2026-09-06, B-067; it read 42 here, 43 in the item and 48
		// before this slice).
		//
		// "That is 42 pieces of real work, not a mislabel" was FALSE, measured:
		// FIVE violations were mislabels. migration_loopzero_api_keys_test.go,
		// migration_memory_p3_test.go, migration_api_key_rotation_test.go and
		// migration_webhook_delivery_test.go — 37 test functions, 68 asserts, green
		// — open the .up.sql of migrations 000024/000025/000026/000031/000033 and
		// were still flagged because the file name did not start with the number.
		// Renaming the four took the count from 48 to 44 without loosening the gate.
		//
		// Still true, and the reason not to widen the match: the gate checks that
		// the FILE exists, so empty stubs would turn it green while testing nothing
		// — the bar is the shape of migration_012_fk_on_delete_test.go. Matching on
		// CONTENT would trade this false positive for a worse false negative (a
		// comment naming the migration would count as a test).
		//
		// Known residual: 000026_memory_exports is still flagged and IS tested — a
		// file can carry only one numeric prefix. See the note in
		// migration_000025_000026_memory_test.go.
		Wiring: failingGate,
		Setup:  setupMigrationTestCoverage,
	})
}

// setupMigrationTestCoverage is the gate replacing migration_test_coverage.sh:
// every DB migration must have a regression test. Pure-logic, hermetic.
func setupMigrationTestCoverage(fs *flag.FlagSet) Check {
	migrationsDir := fs.String("migrations-dir", "", "override migrations dir")
	testsDir := fs.String("tests-dir", "", "override unit tests dir")
	return func(inv Invocation) (Outcome, error) {
		outcome, err := migrationtestcoverage.Check(migrationtestcoverage.Config{
			Root:          inv.Root,
			MigrationsDir: *migrationsDir,
			UnitTestsDir:  *testsDir,
		})
		if err != nil {
			return Fail, err
		}
		for _, m := range outcome.Messages {
			fmt.Fprintln(inv.Stdout, m)
		}
		if !outcome.Pass {
			return Fail, nil
		}
		return Pass, nil
	}
}
