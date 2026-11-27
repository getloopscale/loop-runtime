// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package sqlcsync provides the `sqlc-sync` quality gate: verifies that the
// committed `api/internal/db/generated/` is in-sync with a fresh `sqlc
// generate` run against the same `sqlc.yaml` + `queries/*.sql` + `schema.gen.sql`.
//
// T0.3 of plan 2026-05-29-sqlc-migration-plan v1.2.
//
// Why this gate exists:
//   - Pre-T0.3, `schema_mirror_test.go` (T2.2 of MEET-2026-05-28-001 closure)
//     caught schema↔struct↔SELECT↔INSERT↔Scan drift at TEST time.
//   - sqlc moves the guarantee to COMPILE time — if `generated/` is out-of-sync
//     with the SoT (queries.sql + schema.gen.sql), this gate fails CI loud.
//   - Per ADR-1 D7 + M-10 two-step delete: this gate must ship + go green
//     for one CI cycle BEFORE `schema_mirror_test.go` is deleted.
//
// Implementation:
//   - Spawn `sqlc generate` into a temp directory.
//   - Walk the temp output + diff each .go file against the committed
//     `api/internal/db/generated/` counterpart.
//   - Non-empty diff → FAIL with actionable error.
//   - Hermetic: no live DB needed (sqlc generate is offline against
//     schema.gen.sql).
package sqlcsync

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// Config controls the gate behavior.
type Config struct {
	// RepoRoot is the repository root. Defaults to "." in callers.
	RepoRoot string
	// SqlcBinary overrides the sqlc executable path (default: "sqlc" from PATH).
	// Test fixtures use this to point at a deterministic stub.
	SqlcBinary string
}

// Violation captures a single drift between committed and freshly-generated output.
type Violation struct {
	// File is the relative path of the offending .go file under generated/.
	File string
	// Reason classifies the violation: "missing" / "extra" / "content-diff".
	Reason string
	// Detail is a human-readable description (e.g., first 5 diff lines).
	Detail string
}

// Outcome is what the gate returns.
type Outcome struct {
	Pass       bool
	Violations []Violation
	// SqlcVersion is the version of the sqlc binary used (for evidence + debug).
	SqlcVersion string
}

// Verdict is Pass when the committed generated/ matches a fresh sqlc
// generate, and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Default paths relative to RepoRoot.
const (
	defaultSqlcYAML      = "api/internal/db/sqlc.yaml"
	defaultGeneratedDir  = "api/internal/db/generated"
	defaultQueriesDir    = "api/internal/db/queries"
	defaultSchemaFile    = "api/internal/db/schema.gen.sql"
	expectedSqlcVersion  = "v1.31.1"
	actionableFixMessage = "Run `task db:generate && git add api/internal/db/generated && git commit`"
)

// Check is the gate entry point. Returns Outcome + nil error on successful
// evaluation (regardless of Pass/Fail); returns non-nil error only when the
// gate itself cannot run (sqlc absent, sqlc.yaml missing, etc.).
func Check(cfg Config) (_ Outcome, err error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	if cfg.SqlcBinary == "" {
		cfg.SqlcBinary = "sqlc"
	}

	// Pre-flight: sqlc binary present at the expected version.
	versionCmd := exec.Command(cfg.SqlcBinary, "version")
	versionOut, err := versionCmd.Output()
	if err != nil {
		return Outcome{}, fmt.Errorf("sqlc-sync: sqlc not installed or not in PATH (cfg.SqlcBinary=%q): %w. %s",
			cfg.SqlcBinary, err, "Install: go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1")
	}
	gotVersion := strings.TrimSpace(string(versionOut))
	if gotVersion != expectedSqlcVersion {
		return Outcome{}, fmt.Errorf("sqlc-sync: sqlc version mismatch — got %q, want %q. Reinstall: go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1",
			gotVersion, expectedSqlcVersion)
	}

	// Locate inputs.
	sqlcYAML := filepath.Join(cfg.RepoRoot, defaultSqlcYAML)
	if _, err := os.Stat(sqlcYAML); err != nil {
		return Outcome{}, fmt.Errorf("sqlc-sync: sqlc.yaml not found at %s: %w", sqlcYAML, err)
	}

	committedDir := filepath.Join(cfg.RepoRoot, defaultGeneratedDir)
	if _, err := os.Stat(committedDir); err != nil {
		return Outcome{}, fmt.Errorf("sqlc-sync: committed generated/ not found at %s: %w", committedDir, err)
	}

	queriesDir := filepath.Join(cfg.RepoRoot, defaultQueriesDir)
	schemaFile := filepath.Join(cfg.RepoRoot, defaultSchemaFile)
	if _, err := os.Stat(schemaFile); err != nil {
		return Outcome{}, fmt.Errorf("sqlc-sync: schema.gen.sql not found at %s — run `task db:render-schema` first: %w", schemaFile, err)
	}

	// Spawn sqlc generate into a tempdir.
	tmpDir, err := os.MkdirTemp("", "sqlc-sync-")
	if err != nil {
		return Outcome{}, fmt.Errorf("sqlc-sync: mkdtemp: %w", err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(tmpDir)) }()

	// Mirror the canonical layout into tmpDir so sqlc.yaml's relative paths resolve.
	tmpDB := filepath.Join(tmpDir, "db")
	if err := os.MkdirAll(tmpDB, 0o755); err != nil {
		return Outcome{}, fmt.Errorf("sqlc-sync: mkdir tmpDB: %w", err)
	}
	if err := copyFile(sqlcYAML, filepath.Join(tmpDB, "sqlc.yaml")); err != nil {
		return Outcome{}, fmt.Errorf("sqlc-sync: copy sqlc.yaml: %w", err)
	}
	if err := copyFile(schemaFile, filepath.Join(tmpDB, "schema.gen.sql")); err != nil {
		return Outcome{}, fmt.Errorf("sqlc-sync: copy schema.gen.sql: %w", err)
	}
	if err := copyDir(queriesDir, filepath.Join(tmpDB, "queries")); err != nil {
		return Outcome{}, fmt.Errorf("sqlc-sync: copy queries/: %w", err)
	}

	// Queries exist, so a queries dir with no .sql is a comparison of nothing.
	if err := requireQueries(queriesDir); err != nil {
		return Outcome{}, err
	}

	// Run sqlc generate inside tmpDB.
	genCmd := exec.Command(cfg.SqlcBinary, "generate")
	genCmd.Dir = tmpDB
	if out, err := genCmd.CombinedOutput(); err != nil {
		return Outcome{}, fmt.Errorf("sqlc-sync: sqlc generate failed in tempdir: %w\nstdout/stderr:\n%s", err, string(out))
	}

	// Compare freshly-generated output against committed.
	freshDir := filepath.Join(tmpDB, "generated")
	violations, err := diffDirs(committedDir, freshDir)
	if err != nil {
		return Outcome{}, fmt.Errorf("sqlc-sync: diff failed: %w", err)
	}

	return Outcome{
		Pass:        len(violations) == 0,
		Violations:  violations,
		SqlcVersion: gotVersion,
	}, nil
}

// ActionableFix returns the operator-facing message attached to FAIL output.
func ActionableFix() string { return actionableFixMessage }

// diffDirs walks both directories and returns a list of Violations.
// It compares files by relative path and content.
func diffDirs(committed, fresh string) ([]Violation, error) {
	committedFiles, err := listGoFiles(committed)
	if err != nil {
		return nil, err
	}
	freshFiles, err := listGoFiles(fresh)
	if err != nil {
		return nil, err
	}

	var violations []Violation

	// Files in committed but missing in fresh → "extra" in committed.
	for relPath := range committedFiles {
		if _, ok := freshFiles[relPath]; !ok {
			violations = append(violations, Violation{
				File:   relPath,
				Reason: "extra",
				Detail: "exists in committed generated/ but NOT in fresh sqlc generate output",
			})
		}
	}

	// Files in fresh but missing in committed → "missing" from committed.
	for relPath := range freshFiles {
		if _, ok := committedFiles[relPath]; !ok {
			violations = append(violations, Violation{
				File:   relPath,
				Reason: "missing",
				Detail: "produced by fresh sqlc generate but NOT in committed generated/",
			})
		}
	}

	// Files in both → content diff.
	for relPath, committedData := range committedFiles {
		freshData, ok := freshFiles[relPath]
		if !ok {
			continue
		}
		if !bytes.Equal(committedData, freshData) {
			violations = append(violations, Violation{
				File:   relPath,
				Reason: "content-diff",
				Detail: firstNDiffLines(string(committedData), string(freshData), 5),
			})
		}
	}

	return violations, nil
}

// listGoFiles returns relative-path → content for every .go file under dir.
func listGoFiles(dir string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		result[rel] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// firstNDiffLines returns a short human-readable preview of the first N
// differing lines between a and b.
func firstNDiffLines(a, b string, n int) string {
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")

	var out []string
	max := len(aLines)
	if len(bLines) > max {
		max = len(bLines)
	}

	count := 0
	for i := 0; i < max && count < n; i++ {
		var aLine, bLine string
		if i < len(aLines) {
			aLine = aLines[i]
		}
		if i < len(bLines) {
			bLine = bLines[i]
		}
		if aLine != bLine {
			out = append(out, fmt.Sprintf("  - committed:%d: %s", i+1, truncate(aLine, 100)))
			out = append(out, fmt.Sprintf("  + fresh:%d:     %s", i+1, truncate(bLine, 100)))
			count++
		}
	}
	if count == 0 {
		return "(no line-level diff found; check trailing whitespace or BOM)"
	}
	return strings.Join(out, "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ErrNoQueries means the queries dir holds no .sql file: sqlc would generate
// nothing, and the gate would report the committed code in sync with nothing.
var ErrNoQueries = errors.New("sqlc-sync: no .sql file under api/internal/db/queries — there is nothing to compare the committed generated/ against")

// requireQueries fails when the queries dir cannot be read or holds no .sql.
func requireQueries(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("sqlc-sync: reading queries dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			return nil
		}
	}
	return ErrNoQueries
}

// copyFile is a simple file copy helper.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// copyDir recursively copies a directory.
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target)
	})
}
