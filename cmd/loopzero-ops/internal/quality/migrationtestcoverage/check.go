// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package migrationtestcoverage asserts every api/internal/db/migrations/
// *.up.sql has a corresponding regression test in api/internal/db/
// migration_NNN*_test.go, next to the migrations it reads (CLAUDE.md "every bug fix MUST generate a
// regression test before the fix").
//
// Replaces infra/scripts/migration_test_coverage.sh (~91 LOC) per
// Phase 7 of plan 2026-05-21-bash-to-go-migration-plan.
//
// Origin: T3.4 of deep-review-remediation-plan / find_test_002 HIGH.
package migrationtestcoverage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// DefaultMigrationsDir — bash default.
const DefaultMigrationsDir = "api/internal/db/migrations"

// DefaultUnitTestsDir is where the migration tests live: the db package,
// beside the migrations directory they read.
const DefaultUnitTestsDir = "api/internal/db"

// DefaultExceptions — pre-T3.4-baseline migrations implicitly covered.
// Entries match either the full migration base OR just the numeric
// prefix (e.g., "000001_initial_schema" OR "000003").
var DefaultExceptions = []string{
	"000001_initial_schema",
	"000002_seed_data",
	"000003",
	"000004_preview_environments",
	"000009_app_scaling_config",
}

// Violation captures one uncovered migration.
type Violation struct {
	MigrationBase  string
	ExpectedPrefix string // "migration_000011" or "migration_011"
}

// Config holds gate inputs.
type Config struct {
	// Root is the repo root. Default: ".".
	Root string

	// MigrationsDir overrides DefaultMigrationsDir.
	MigrationsDir string

	// UnitTestsDir overrides DefaultUnitTestsDir.
	UnitTestsDir string

	// Exceptions overrides DefaultExceptions.
	Exceptions []string
}

// Outcome captures gate result.
type Outcome struct {
	Pass       bool
	Total      int
	Covered    int
	Violations []Violation
	Messages   []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// shortPrefix strips leading zeros from num.
// "000011" → "11"  ;  "0" → "0"  ;  "000000" → "0".
func shortPrefix(num string) string {
	stripped := strings.TrimLeft(num, "0")
	if stripped == "" {
		return "0"
	}
	return stripped
}

// pad3 formats n as a 3-digit zero-padded string.
func pad3(short string) string {
	n, err := strconv.Atoi(short)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%03d", n)
}

// matchesException returns true when base or num is in the exception list.
func matchesException(base, num string, exceptions []string) bool {
	for _, e := range exceptions {
		if e == base || e == num {
			return true
		}
	}
	return false
}

// findTestForMigration checks whether a matching test file exists under
// unitTestsDir. Tries both 6-digit (migration_000011*_test.go) AND
// 3-digit (migration_011*_test.go) prefixes (bash bug-compat).
func findTestForMigration(unitTestsDir, num string) (bool, error) {
	short := shortPrefix(num)
	short3 := pad3(short)
	prefixes := []string{
		"migration_" + num,
		"migration_" + short3,
	}
	entries, err := os.ReadDir(unitTestsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		for _, p := range prefixes {
			if strings.HasPrefix(name, p) {
				return true, nil
			}
		}
	}
	return false, nil
}

// listMigrations returns the sorted list of `<base>.up.sql` basenames.
func listMigrations(migrationsDir string) ([]string, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		base := strings.TrimSuffix(name, ".up.sql")
		out = append(out, base)
	}
	sort.Strings(out)
	return out, nil
}

// resolvePath joins p under root unless absolute.
func resolvePath(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if root == "" {
		root = "."
	}
	return filepath.Join(root, p)
}

// Check executes the gate.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	migrationsDir := cfg.MigrationsDir
	if migrationsDir == "" {
		migrationsDir = DefaultMigrationsDir
	}
	unitTestsDir := cfg.UnitTestsDir
	if unitTestsDir == "" {
		unitTestsDir = DefaultUnitTestsDir
	}
	exceptions := cfg.Exceptions
	if exceptions == nil {
		exceptions = DefaultExceptions
	}

	resolvedMig := resolvePath(root, migrationsDir)
	resolvedTests := resolvePath(root, unitTestsDir)

	out := &Outcome{}

	bases, err := listMigrations(resolvedMig)
	if err != nil {
		if os.IsNotExist(err) {
			out.Pass = true
			out.Messages = []string{
				fmt.Sprintf("PASS — migrations dir %s not found.", resolvedMig),
			}
			return out, nil
		}
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("migrationtestcoverage: read %s: %v", resolvedMig, err),
			Cause:   err,
		}
	}

	for _, base := range bases {
		out.Total++
		// Extract numeric prefix: "000011_foo" → "000011".
		var num string
		if i := strings.Index(base, "_"); i >= 0 {
			num = base[:i]
		} else {
			num = base
		}
		if matchesException(base, num, exceptions) {
			out.Covered++
			continue
		}
		ok, err := findTestForMigration(resolvedTests, num)
		if err != nil {
			return nil, &loopzeroerrors.LoopZeroError{
				Code:    "OPS_DEPLOY_FAILURE",
				Message: fmt.Sprintf("migrationtestcoverage: scan tests dir: %v", err),
				Cause:   err,
			}
		}
		if ok {
			out.Covered++
			continue
		}
		short3 := pad3(shortPrefix(num))
		out.Violations = append(out.Violations, Violation{
			MigrationBase:  base,
			ExpectedPrefix: fmt.Sprintf("migration_{%s,%s}", num, short3),
		})
	}

	out.Messages = append(out.Messages, "")
	out.Messages = append(out.Messages,
		fmt.Sprintf("Coverage: %d/%d migrations have a regression test (or are in EXCEPTION_LIST).", out.Covered, out.Total))

	if len(out.Violations) == 0 {
		out.Pass = true
		out.Messages = append(out.Messages, "PASS — every migration has its regression test.")
		return out, nil
	}
	out.Pass = false
	// Prepend violation lines.
	var violLines []string
	for _, v := range out.Violations {
		violLines = append(violLines,
			fmt.Sprintf("MIGRATION-TEST-COVERAGE VIOLATION: %s has no matching %s/%s*_test.go",
				v.MigrationBase, resolvedTests, v.ExpectedPrefix))
	}
	out.Messages = append(violLines, out.Messages...)
	out.Messages = append(out.Messages,
		"",
		fmt.Sprintf("FAIL — %d migration(s) without test.", len(out.Violations)),
		"",
		"To fix: create api/internal/db/migration_NNN_<slug>_test.go with text-level",
		"invariants. See migration_011_release_check_canonical_test.go or",
		"migration_012_fk_on_delete_test.go for canonical patterns.",
		"",
		"Genuine exceptions may be added to the exception list with ADR approval.")
	return out, nil
}
