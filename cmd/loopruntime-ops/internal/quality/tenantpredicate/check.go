// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package tenantpredicate enforces that every SELECT / UPDATE / DELETE on a
// tenant-owned table carries a `tenant_id = $N` predicate (or equivalent
// JOIN-based filter), AND that every INSERT on a tenant-owned table
// specifies `tenant_id` as a column.
//
// T0.5 of plan 2026-05-29-sqlc-migration-plan v1.2.
// Per platform-security-engineer condition 1 (VETO-backed) + M-1 + M-2.
//
// Why this gate exists:
//
//	Pre-T0.5, the burden of "every query has tenant_id" was on code review.
//	Multi-tenant isolation is a CVSS 9.x class — a single missing predicate
//	on a single query leaks cross-tenant data. sqlc moves this from
//	review-time to compile-time-adjacent (CI gate at every PR).
//
// Phase 0 baseline: queries/ only has .gitkeep → trivially passes.
package tenantpredicate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// Config controls the gate.
type Config struct {
	// RepoRoot is the repository root. Default ".".
	RepoRoot string
}

// Violation captures a single tenant-predicate gap.
type Violation struct {
	File   string
	Query  string // sqlc query name (e.g., "GetReleaseByID")
	Table  string // table that triggered the violation
	Reason string // "missing-where-predicate" / "missing-insert-column" / etc.
	Detail string
}

// Outcome is what the gate returns.
type Outcome struct {
	Pass       bool
	Violations []Violation
	Scanned    int // count of .sql files scanned
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

const (
	defaultQueriesDir = "api/internal/db/queries"
	defaultRawDir     = "api/internal/db/raw"
	exemptionMarker   = "tenantpredicate:exempt"
)

// Check is the gate entry point.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}

	dir := filepath.Join(cfg.RepoRoot, defaultQueriesDir)

	var allViolations []Violation
	scanned := 0

	// Phase 0 baseline: queries/ may not exist yet → skip the
	// .sql scan loop but still run the raw/ scan below.
	queriesDirExists := false
	if _, err := os.Stat(dir); err == nil {
		queriesDirExists = true
	}

	if queriesDirExists {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(info.Name(), ".sql") {
				return nil
			}
			scanned++

			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fileViolations := scanQueryFile(path, string(data))
			allViolations = append(allViolations, fileViolations...)
			return nil
		})
		if err != nil {
			return Outcome{}, err
		}
	}

	// T4.1: extend gate to scan api/internal/db/raw/ subpackage. Raw
	// queries are the sqlc-unparseable escape hatch (see raw/README.md);
	// the gate enforces that raw .go files NEVER touch the
	// SensitiveTables exclusion list (secret, audit_log, bootstrap
	// users/tenants/tenant_members/subscriptions, tombstones, cells).
	rawDir := filepath.Join(cfg.RepoRoot, defaultRawDir)
	if _, err := os.Stat(rawDir); err == nil {
		rawViolations, rawScanned, err := scanRawSubpackage(rawDir)
		if err != nil {
			return Outcome{}, err
		}
		allViolations = append(allViolations, rawViolations...)
		scanned += rawScanned
	}

	return Outcome{
		Pass:       len(allViolations) == 0,
		Violations: allViolations,
		Scanned:    scanned,
	}, nil
}

// rawSensitiveTables mirrors api/internal/db/raw/sensitive_tables.go
// SensitiveTables. Kept in sync via TestRawSensitiveTablesAlignment
// (cmd/loopruntime-ops/internal/quality/tenantpredicate/raw_check_test.go).
var rawSensitiveTables = []string{
	"secrets",
	"audit_log",
	"users",
	"tenants",
	"tenant_members",
	"subscriptions",
	"tenants_pending_deletion",
	"tenant_tombstones",
	"cells",
}

// rawSQLLiteralPattern picks up SQL strings embedded in Go via the
// backtick raw-string syntax (`...`) — the canonical shape for SQL
// in api/internal/db/raw/*.go.
var rawSQLLiteralPattern = regexp.MustCompile("`([^`]*\\bFROM\\s+[a-z_][a-z0-9_]*\\b[^`]*|[^`]*\\bINTO\\s+[a-z_][a-z0-9_]*\\b[^`]*|[^`]*\\bUPDATE\\s+[a-z_][a-z0-9_]*\\b[^`]*|[^`]*\\bJOIN\\s+[a-z_][a-z0-9_]*\\b[^`]*)`")

// scanRawSubpackage walks api/internal/db/raw/ and rejects any raw
// query that references a sensitive table.
func scanRawSubpackage(rawDir string) ([]Violation, int, error) {
	var violations []Violation
	scanned := 0

	err := filepath.Walk(rawDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		// Exclude test files + sensitive_tables.go itself (it
		// names the tables as data, not as SQL).
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if name == "sensitive_tables.go" {
			return nil
		}
		scanned++

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(data)

		// Extract every SQL string literal (backtick-quoted) and
		// scan for references to sensitive tables.
		matches := rawSQLLiteralPattern.FindAllString(content, -1)
		for _, sqlLit := range matches {
			// Identify all referenced tables via the existing
			// fromTablePattern / insertTablePattern.
			refs := extractTables(sqlLit)
			for _, table := range refs {
				if isRawSensitive(table) {
					violations = append(violations, Violation{
						File:   path,
						Query:  filepath.Base(path),
						Table:  table,
						Reason: "raw-sensitive-table",
						Detail: fmt.Sprintf("raw query at %s references sensitive table %q — sensitive tables MUST go through sqlc-backed wrappers (see api/internal/db/raw/README.md)", path, table),
					})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return violations, scanned, nil
}

func isRawSensitive(table string) bool {
	for _, t := range rawSensitiveTables {
		if strings.EqualFold(t, table) {
			return true
		}
	}
	return false
}

// ActionableFix returns the operator-facing message for FAIL output.
func ActionableFix() string {
	return "Add `WHERE ... tenant_id = $N` to SELECT/UPDATE/DELETE on tenant-owned " +
		"tables (or specify `tenant_id` as a column in INSERT). To exempt a query " +
		"(e.g., for cross-tenant admin operations), add the comment " +
		"`-- tenantpredicate:exempt` directly above the `-- name:` line."
}

// sqlcNamePattern matches sqlc's canonical query header: `-- name: <Name> :one|:many|:exec`
var sqlcNamePattern = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)\s*:\w+\s*$`)

// fromTablePattern picks up tables in SELECT ... FROM <table> and SELECT ... JOIN <table>.
var fromTablePattern = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|UPDATE|DELETE\s+FROM)\s+([a-z_][a-z0-9_]*)\b`)

// insertTablePattern picks up tables in INSERT INTO <table>.
var insertTablePattern = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+([a-z_][a-z0-9_]*)\b`)

// scanQueryFile splits a .sql file into queries (each starts at `-- name:`)
// and validates each query against the tenant_id predicate rule.
//
// Heuristic: per-query check.
//   - Identify all tables referenced (FROM / JOIN / INSERT / UPDATE / DELETE).
//   - For each tenant-owned table:
//   - INSERT must mention `tenant_id` as a column.
//   - SELECT/UPDATE/DELETE must contain `tenant_id` somewhere in the
//     text (best-effort: real WHERE-clause parsing is overkill;
//     presence of `tenant_id` in the query body satisfies the
//     predicate check for now).
//   - The `tenants` table itself is exempt (queries use `id = $N`).
//   - Queries with `-- tenantpredicate:exempt` comment immediately above
//     the `-- name:` line are allowlisted.
func scanQueryFile(path, content string) []Violation {
	queries := splitQueries(content)

	var violations []Violation
	for _, q := range queries {
		if q.Exempt {
			continue
		}
		for _, table := range q.ReferencedTables {
			if !IsTenantOwned(table) {
				continue
			}
			if IsTenantSelfTable(table) {
				// `tenants` table uses `id = $N`, not `tenant_id`.
				continue
			}
			if q.IsInsert(table) {
				if !q.ReferencesTenantIDInInsertColumns() {
					violations = append(violations, Violation{
						File:   path,
						Query:  q.Name,
						Table:  table,
						Reason: "missing-insert-column",
						Detail: fmt.Sprintf("INSERT INTO %s does not specify `tenant_id` column", table),
					})
				}
				continue
			}
			// SELECT / UPDATE / DELETE: require `tenant_id` somewhere in body.
			if !q.ReferencesTenantIDInBody() {
				violations = append(violations, Violation{
					File:   path,
					Query:  q.Name,
					Table:  table,
					Reason: "missing-where-predicate",
					Detail: fmt.Sprintf("query on %s lacks `tenant_id` predicate (add WHERE ... tenant_id = $N or mark `-- tenantpredicate:exempt`)", table),
				})
			}
		}
	}
	return violations
}

// query holds a single parsed sqlc query block.
type query struct {
	Name             string
	Body             string
	Exempt           bool
	ReferencedTables []string
}

// IsInsert returns true if the query body contains an INSERT INTO on the
// specified table.
func (q *query) IsInsert(table string) bool {
	matches := insertTablePattern.FindAllStringSubmatch(q.Body, -1)
	for _, m := range matches {
		if len(m) > 1 && strings.EqualFold(m[1], table) {
			return true
		}
	}
	return false
}

// ReferencesTenantIDInBody returns true if the query body contains the
// substring `tenant_id` (case-insensitive). This is best-effort —
// real WHERE-clause parsing is left for a future upgrade if false
// positives appear.
func (q *query) ReferencesTenantIDInBody() bool {
	return containsIgnoreCase(q.Body, "tenant_id")
}

// ReferencesTenantIDInInsertColumns returns true if the INSERT statement
// in the body specifies `tenant_id` in its column list.
func (q *query) ReferencesTenantIDInInsertColumns() bool {
	// Heuristic: split on `INSERT INTO ... (` and look for `tenant_id` in
	// the column list before `VALUES`.
	upper := strings.ToUpper(q.Body)
	idx := strings.Index(upper, "INSERT INTO")
	if idx < 0 {
		return false
	}
	rest := q.Body[idx:]
	valuesIdx := strings.Index(strings.ToUpper(rest), "VALUES")
	if valuesIdx < 0 {
		valuesIdx = len(rest)
	}
	columnList := rest[:valuesIdx]
	return containsIgnoreCase(columnList, "tenant_id")
}

// splitQueries parses a sqlc .sql file content into individual queries.
// Each query starts at `-- name: <Name>` and runs until the next `-- name:`
// or end of file.
func splitQueries(content string) []*query {
	lines := strings.Split(content, "\n")
	var queries []*query

	var current *query
	var nextExempt bool

	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)

		if strings.HasPrefix(trimmed, "--") {
			// Check for exemption marker BEFORE the name line.
			if strings.Contains(trimmed, exemptionMarker) {
				nextExempt = true
				continue
			}
		}

		if match := sqlcNamePattern.FindStringSubmatch(raw); match != nil {
			if current != nil {
				current.ReferencedTables = extractTables(current.Body)
				queries = append(queries, current)
			}
			current = &query{Name: match[1], Exempt: nextExempt}
			nextExempt = false
			continue
		}

		if current != nil {
			current.Body += raw + "\n"
		}
	}
	if current != nil {
		current.ReferencedTables = extractTables(current.Body)
		queries = append(queries, current)
	}
	return queries
}

// extractTables pulls referenced tables out of a query body via regex.
func extractTables(body string) []string {
	seen := map[string]struct{}{}
	for _, pattern := range []*regexp.Regexp{fromTablePattern, insertTablePattern} {
		for _, m := range pattern.FindAllStringSubmatch(body, -1) {
			if len(m) > 1 {
				name := strings.ToLower(m[1])
				if _, ok := seen[name]; !ok {
					seen[name] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// containsIgnoreCase is strings.Contains case-insensitive.
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
