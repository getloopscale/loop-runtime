// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// RED test pinning B-041: versioned documentation prescribes a write to a
// column that the schema does not have. UNTRACKED — measurement artifact.
package docsschemarefs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const repoRoot = "../../../../.."

// colValue matches a prose prescription of the form  column='value'.
var colValue = regexp.MustCompile(`([a-z_][a-z0-9_]*)='([^']+)'`)

// createTable matches the head of a CREATE TABLE block in schema.gen.sql.
var createTable = regexp.MustCompile(`^CREATE TABLE public\.([a-z_][a-z0-9_]*) \($`)

// columnsOf parses schema.gen.sql into table -> set of column names.
func columnsOf(t *testing.T) map[string]map[string]bool {
	t.Helper()
	path := filepath.Join(repoRoot, "api/internal/db/schema.gen.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	out := map[string]map[string]bool{}
	var cur string
	for _, line := range strings.Split(string(raw), "\n") {
		if m := createTable.FindStringSubmatch(line); m != nil {
			cur = m[1]
			out[cur] = map[string]bool{}
			continue
		}
		if cur == "" {
			continue
		}
		if strings.HasPrefix(line, ");") {
			cur = ""
			continue
		}
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) < 2 {
			continue
		}
		name := strings.TrimSuffix(f[0], ",")
		if regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(name) && f[0] != "CONSTRAINT" {
			out[cur][name] = true
		}
	}
	return out
}

// TestDocsColumnPrescriptionsResolve asserts that when a line of versioned
// documentation names a table and prescribes column='value' in the same line,
// the column exists on that table.
func TestDocsColumnPrescriptionsResolve(t *testing.T) {
	schema := columnsOf(t)
	if !schema["audit_log"]["action"] {
		t.Fatalf("positive control failed: audit_log.action not parsed from schema")
	}

	docs := []string{"docs/runbooks.md", "docs/operations.md", "docs/architecture.md",
		"docs/api-contracts.md", "docs/decisions.md", "docs/loopzerokit-loopzerocloud-seam.md", "README.md"}

	violations := 0
	// qualified form:  table.column='value'
	qualified := regexp.MustCompile(`([a-z_][a-z0-9_]*)\\.([a-z_][a-z0-9_]*)='([^']+)'`)
	// a table name written inside backticks on the same line
	backticked := regexp.MustCompile("`([a-z_][a-z0-9_]*)`")

	for _, rel := range docs {
		raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(raw), "\n") {
			qual := map[string]bool{}
			for _, m := range qualified.FindAllStringSubmatch(line, -1) {
				qual[m[2]] = true
				if cols, ok := schema[m[1]]; ok && !cols[m[2]] {
					t.Errorf("%s:%d prescribes %s.%s=... but %s has no column %s",
						rel, i+1, m[1], m[2], m[1], m[2])
					violations++
				}
			}
			var tables []string
			for _, m := range backticked.FindAllStringSubmatch(line, -1) {
				if _, ok := schema[m[1]]; ok {
					tables = append(tables, m[1])
				}
			}
			if len(tables) == 0 {
				continue
			}
			for _, m := range colValue.FindAllStringSubmatch(line, -1) {
				col := m[1]
				if qual[col] {
					continue // already judged against its own table
				}
				for _, table := range tables {
					if schema[table][col] {
						continue
					}
					t.Errorf("%s:%d prescribes %s=... alongside table `%s`, which has no such column",
						rel, i+1, col, table)
					violations++
				}
			}
		}
	}
	if violations == 0 {
		t.Log("no violations — B-041 would be closed")
	}
}
