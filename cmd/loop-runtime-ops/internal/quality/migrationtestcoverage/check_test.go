// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package migrationtestcoverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}
	return root
}

// =============================================================================
// shortPrefix / pad3
// =============================================================================

func TestShortPrefix(t *testing.T) {
	assert.Equal(t, "11", shortPrefix("000011"))
	assert.Equal(t, "1", shortPrefix("000001"))
	assert.Equal(t, "0", shortPrefix("0"))
	assert.Equal(t, "0", shortPrefix("000000"))
	assert.Equal(t, "123", shortPrefix("00000123"))
}

func TestPad3(t *testing.T) {
	assert.Equal(t, "011", pad3("11"))
	assert.Equal(t, "001", pad3("1"))
	assert.Equal(t, "123", pad3("123"))
	assert.Equal(t, "", pad3("notanumber"))
}

// =============================================================================
// matchesException
// =============================================================================

func TestMatchesException(t *testing.T) {
	ex := []string{"000001_initial_schema", "000003"}
	assert.True(t, matchesException("000001_initial_schema", "000001", ex))
	assert.True(t, matchesException("000003_some_change", "000003", ex), "numeric prefix matches")
	assert.False(t, matchesException("000099_other", "000099", ex))
}

// =============================================================================
// findTestForMigration — both 6-digit and 3-digit prefixes
// =============================================================================

func TestFindTestForMigration_6DigitMatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "migration_000011_add_index_test.go"),
		[]byte("package x"), 0o600))
	ok, err := findTestForMigration(dir, "000011")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestFindTestForMigration_3DigitFallback(t *testing.T) {
	dir := t.TempDir()
	// Old-style 3-digit prefix must still match.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "migration_011_legacy_test.go"),
		[]byte("package x"), 0o600))
	ok, err := findTestForMigration(dir, "000011")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestFindTestForMigration_NoMatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "migration_999_other_test.go"),
		[]byte("package x"), 0o600))
	ok, err := findTestForMigration(dir, "000011")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestFindTestForMigration_NonTestFileIgnored(t *testing.T) {
	dir := t.TempDir()
	// File named migration_000011 but NOT *_test.go.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "migration_000011_notes.md"),
		[]byte("doc"), 0o600))
	ok, err := findTestForMigration(dir, "000011")
	require.NoError(t, err)
	assert.False(t, ok, "non-_test.go files must NOT count as coverage")
}

func TestFindTestForMigration_MissingDir(t *testing.T) {
	ok, err := findTestForMigration("/tmp/zzz-no-tests-dir", "000011")
	require.NoError(t, err)
	assert.False(t, ok)
}

// =============================================================================
// listMigrations
// =============================================================================

func TestListMigrations_Sorted(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "000003_foo.up.sql"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "000001_init.up.sql"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "000001_init.down.sql"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "000002_seed.up.sql"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte(""), 0o600))

	bases, err := listMigrations(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"000001_init", "000002_seed", "000003_foo"}, bases)
}

func TestListMigrations_MissingDir(t *testing.T) {
	_, err := listMigrations("/tmp/zzz-no-mig")
	require.Error(t, err)
}

// =============================================================================
// resolvePath
// =============================================================================

func TestResolvePath(t *testing.T) {
	assert.Equal(t, "/repo/api/internal/db/migrations", resolvePath("/repo", "api/internal/db/migrations"))
	assert.Equal(t, "/abs/path", resolvePath("/repo", "/abs/path"))
	assert.Equal(t, "api/internal/db/migrations", resolvePath("", "api/internal/db/migrations"))
}

// =============================================================================
// Check — end-to-end
// =============================================================================

func TestCheck_AllCovered(t *testing.T) {
	root := setup(t, map[string]string{
		"api/internal/db/migrations/000001_initial_schema.up.sql": "-- init",
		"api/internal/db/migrations/000011_add_index.up.sql":      "-- index",
		"api/internal/db/migration_000011_add_index_test.go":      "package db_test",
		// 000001 is in DefaultExceptions; 000011 has a test.
	})
	out, err := Check(Config{Root: root})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Equal(t, 2, out.Total)
	assert.Equal(t, 2, out.Covered)
	assert.Empty(t, out.Violations)
}

func TestCheck_MissingTestBlocks(t *testing.T) {
	root := setup(t, map[string]string{
		"api/internal/db/migrations/000020_new_feature.up.sql": "-- new",
		// No corresponding test file.
	})
	out, err := Check(Config{Root: root})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	require.Len(t, out.Violations, 1)
	assert.Equal(t, "000020_new_feature", out.Violations[0].MigrationBase)
	joined := strings.Join(out.Messages, "\n")
	assert.Contains(t, joined, "MIGRATION-TEST-COVERAGE VIOLATION")
	assert.Contains(t, joined, "1 migration(s) without test")
}

func TestCheck_3DigitTestFileAcceptedForLargeMigration(t *testing.T) {
	root := setup(t, map[string]string{
		"api/internal/db/migrations/000011_release_check.up.sql": "-- ...",
		"api/internal/db/migration_011_release_check_test.go":    "package db_test",
	})
	out, err := Check(Config{Root: root})
	require.NoError(t, err)
	assert.True(t, out.Pass)
}

func TestCheck_ExceptionByFullBase(t *testing.T) {
	root := setup(t, map[string]string{
		"api/internal/db/migrations/000099_custom.up.sql": "-- ...",
	})
	out, err := Check(Config{
		Root:       root,
		Exceptions: []string{"000099_custom"},
	})
	require.NoError(t, err)
	assert.True(t, out.Pass)
}

func TestCheck_ExceptionByNumericPrefix(t *testing.T) {
	root := setup(t, map[string]string{
		"api/internal/db/migrations/000099_custom.up.sql": "-- ...",
	})
	out, err := Check(Config{
		Root:       root,
		Exceptions: []string{"000099"},
	})
	require.NoError(t, err)
	assert.True(t, out.Pass)
}

func TestCheck_MigrationsDirMissingPasses(t *testing.T) {
	root := t.TempDir()
	out, err := Check(Config{Root: root})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "not found")
}

func TestCheck_MultipleMigrationsMixed(t *testing.T) {
	root := setup(t, map[string]string{
		"api/internal/db/migrations/000001_initial_schema.up.sql": "-- init", // exempt
		"api/internal/db/migrations/000020_add_a.up.sql":          "-- ...",  // test present
		"api/internal/db/migrations/000021_add_b.up.sql":          "-- ...",  // MISSING
		"api/internal/db/migrations/000022_add_c.up.sql":          "-- ...",  // test present
		"api/internal/db/migration_000020_a_test.go":              "package db_test",
		"api/internal/db/migration_000022_c_test.go":              "package db_test",
	})
	out, err := Check(Config{Root: root})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	assert.Equal(t, 4, out.Total)
	assert.Equal(t, 3, out.Covered)
	require.Len(t, out.Violations, 1)
	assert.Equal(t, "000021_add_b", out.Violations[0].MigrationBase)
}

func TestCheck_BaseWithoutUnderscore(t *testing.T) {
	// Edge case: migration name with no underscore separator.
	root := setup(t, map[string]string{
		"api/internal/db/migrations/000099.up.sql": "-- ...",
		"api/internal/db/migration_000099_test.go": "package db_test",
	})
	out, err := Check(Config{Root: root})
	require.NoError(t, err)
	assert.True(t, out.Pass)
}

// =============================================================================
// Defaults / Constants
// =============================================================================

func TestDefaults(t *testing.T) {
	assert.Equal(t, "api/internal/db/migrations", DefaultMigrationsDir)
	assert.Equal(t, "api/internal/db", DefaultUnitTestsDir)
	require.Len(t, DefaultExceptions, 5)
	assert.Contains(t, DefaultExceptions, "000001_initial_schema")
	assert.Contains(t, DefaultExceptions, "000003")
}
