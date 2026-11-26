// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db

import (
	"regexp"
	"strings"
)

// metricCheckSetFor returns the set of values admitted by the EFFECTIVE CHECK on
// `<table>.metric`: the last CHECK left after applying `sqls` in order. It is a
// test helper for the cost_counters drift guard and lives in a _test.go file so
// it never ships in the production package.
//
// WHY IT IS PARAMETERIZED BY TABLE (#241). The previous version scanned every
// migration for `metric ... IN (...)` and kept the last occurrence without
// checking which table it belonged to. When lens_alert_rules and
// lens_fired_alerts gained their own `metric` column (000054/000056/000058/
// 000059), the "last occurrence" became the lens one, and the cost_counters
// drift guard compared the cost vocabulary with the observability one. It
// failed claiming `cpu_core_seconds` was missing from the CHECK (it is there)
// and that `p95_latency` was extra (it never was; it belongs to another table).
// A red test nobody can turn green teaches the team to ignore red.
//
// HONEST LIMIT: this is a TEXTUAL parser, not SQL. It covers the two shapes
// these migrations use (the inline column in CREATE TABLE, and the DROP
// CONSTRAINT / ADD CONSTRAINT pair) and deliberately ignores
// `UPDATE ... WHERE metric IN (...)` (000032 has one; it constrains nothing). A
// CHECK written a third way (function, domain, trigger) would escape it. The
// faithful alternative would start PostgreSQL and read pg_constraint, which
// this unit test does not want to pay for.
func metricCheckSetFor(table string, sqls []string) map[string]bool {
	var last string
	for _, sql := range sqls {
		stripped := stripSQLComments(sql)
		if v, ok := lastMetricCheckInFile(table, stripped); ok {
			last = v
		}
	}
	if last == "" {
		return nil
	}
	set := map[string]bool{}
	for _, m := range reQuotedValue.FindAllStringSubmatch(last, -1) {
		set[m[1]] = true
	}
	return set
}

var (
	// `-- comments` are stripped before any match: several of these migrations describe in prose
	// the CHECK they are about to replace, and one of those sentences quoted verbatim would
	// otherwise become the answer.
	reLineComment = regexp.MustCompile(`(?m)--.*$`)
	reQuotedValue = regexp.MustCompile(`'([^']+)'`)
	// The value list of a CHECK on the `metric` column. Anchored on CHECK on purpose: an
	// `UPDATE ... WHERE metric IN (...)` does not match, and must not; it constrains nothing.
	reMetricCheck = regexp.MustCompile(`(?is)CHECK\s*\(\s*metric\s+IN\s*\(([^)]*)\)`)
)

func stripSQLComments(sql string) string { return reLineComment.ReplaceAllString(sql, "") }

// lastMetricCheckInFile returns the value list of the last CHECK on
// `<table>.metric` in the file, if any.
//
// The unit is the STATEMENT (the slice between `;`), and it only counts when the
// statement is a `CREATE TABLE <table>` or an `ALTER TABLE <table>`. That is what
// separates the two shapes these migrations use
//
//	CREATE TABLE cost_counters ( ... metric TEXT NOT NULL CHECK (metric IN (...)) ... );
//	ALTER TABLE  cost_counters ADD CONSTRAINT ... CHECK (metric IN (...));
//
// from a mention of the table that constrains nothing: a FK to it, an index, an
// UPDATE.
//
// Splitting on `;` without a lexer would be wrong if a literal contained `;`;
// none of these migrations do, and the alternative is a full SQL parser for a
// unit test.
func lastMetricCheckInFile(table, sql string) (string, bool) {
	var last string
	found := false

	for _, stmt := range strings.Split(sql, ";") {
		lower := strings.ToLower(stmt)
		if !strings.Contains(lower, strings.ToLower(table)) {
			continue
		}
		isAlter := reAlterOf(table).MatchString(stmt)
		isCreate := reCreateOf(table).MatchString(stmt)
		if !isAlter && !isCreate {
			// The table is mentioned (FK, index) but the statement is not about it.
			continue
		}
		for _, m := range reMetricCheck.FindAllStringSubmatch(stmt, -1) {
			last, found = m[1], true
		}
	}
	return last, found
}

func reAlterOf(table string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?"?` + regexp.QuoteMeta(table) + `"?\b`)
}

func reCreateOf(table string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?"?` + regexp.QuoteMeta(table) + `"?\b`)
}
