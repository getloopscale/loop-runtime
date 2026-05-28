// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package db schema-mirror helpers: reflective + regex + AST extractors used
// by schema_mirror_test.go to cross-check INFORMATION_SCHEMA against the
// Go-side surface (struct tags, INSERT SQL, SELECT SQL, scanner function).
//
// T2.2 of plan 2026-05-28-meet-001-engineering-closure-plan.md. The
// 3-layer Rekor URL persistence bug (LIVE 2026-05-28) had a working
// migration + struct + writer, but the column was silently omitted from
// 13 SELECT statements + scanReleaseFields. The mirror test fails CI any
// time a column lands in only some of (migration, struct, INSERT, SELECTs,
// scanner) — turning a "silent" multi-layer omission into a loud red
// gate at PR time.
package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// ExtractStructJSONColumns returns the canonical column names declared on a
// Go struct via its `json:"..."` tags, stripping comma-suffixes like
// `,omitempty`. Fields without a json tag (or with `json:"-"`) are skipped.
//
// LoopZero's Release struct uses `json:` tags whose left-hand side matches the
// PG column name 1:1. If a future table introduces a different tag convention,
// pass that struct through a sibling helper with the right tag key.
func ExtractStructJSONColumns(t reflect.Type) []string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := tag
		if idx := strings.Index(name, ","); idx >= 0 {
			name = name[:idx]
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// insertColumnsRegex captures the parenthesized column list of an
// `INSERT INTO <table> (col1, col2, ...) VALUES (...)` statement. The
// table name is the first capture group; the column list is the second.
var insertColumnsRegex = regexp.MustCompile(`(?is)INSERT\s+INTO\s+(\w+)\s*\(\s*([^)]+)\s*\)`)

// ExtractInsertColumns returns the column-list of every `INSERT INTO
// <tableName>` in the given SQL chunk, with each column name trimmed.
// Multiple inserts targeting the same table return concatenated unique lists.
// Empty list if no insert matches.
func ExtractInsertColumns(sql, tableName string) []string {
	seen := map[string]bool{}
	for _, m := range insertColumnsRegex.FindAllStringSubmatch(sql, -1) {
		if !strings.EqualFold(m[1], tableName) {
			continue
		}
		for _, col := range strings.Split(m[2], ",") {
			c := strings.TrimSpace(col)
			if c != "" {
				seen[c] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// selectColumnsForTable captures the column-list between `SELECT` and
// `FROM <tableName>`. (?s) lets `.` match newlines (multi-line SELECT is
// canonical LoopZero style). The captured group excludes the backtick rune so
// the regex CANNOT bridge across two Go raw-string SQL literals — without
// this guard the lazy `.+?` would happily eat through ` ` and into the next
// query, producing garbage columns.
//
// Caveat: the regex is intentionally simple. It does NOT support subqueries
// or CTEs in the column list. The LoopZero SELECTs in release_store.go don't use
// either; if a future codepath introduces them, extend this helper or
// hand-write the column list expected.
func selectColumnsForTable(tableName string) *regexp.Regexp {
	// Build the no-backtick character class via concatenation since Go raw
	// strings can't contain a literal backtick.
	noBacktick := "[^" + "`" + "]"
	pattern := `(?is)SELECT\s+(` + noBacktick + `+?)\s+FROM\s+` + regexp.QuoteMeta(tableName) + `\b`
	return regexp.MustCompile(pattern)
}

// ExtractSelectColumns returns the UNION of every column-name referenced in
// any `SELECT ... FROM <tableName>` block of the supplied SQL chunk. Columns
// are trimmed; aliases ("col AS alias") return the underlying col name;
// COALESCE(col, default) is unwrapped to `col` (LoopZero uses it heavily for
// JSONB defaults); other function calls / expressions are skipped.
func ExtractSelectColumns(sql, tableName string) []string {
	re := selectColumnsForTable(tableName)
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(sql, -1) {
		list := m[1]
		for _, part := range splitTopLevelCommas(list) {
			col := normalizeSelectExpression(part)
			if col != "" {
				seen[col] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// splitTopLevelCommas splits a SELECT column-list by commas that are at
// parenthesis depth zero (so COALESCE(col, 'default') stays a single part).
// Quote-handling: single-quoted SQL literals can contain commas — we track
// open-single-quote and skip commas inside.
func splitTopLevelCommas(list string) []string {
	var parts []string
	var current strings.Builder
	depth := 0
	inQuote := false
	for i := 0; i < len(list); i++ {
		c := list[i]
		switch {
		case c == '\'' && (i == 0 || list[i-1] != '\\'):
			inQuote = !inQuote
			current.WriteByte(c)
		case inQuote:
			current.WriteByte(c)
		case c == '(':
			depth++
			current.WriteByte(c)
		case c == ')':
			if depth > 0 {
				depth--
			}
			current.WriteByte(c)
		case c == ',' && depth == 0:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// coalesceFirstArgRegex captures the first identifier inside COALESCE(col, ...).
// LoopZero uses COALESCE heavily to project JSONB columns with default values
// (`COALESCE(side_effects, 'null'::jsonb)`); the extracted column name is the
// schema column the SELECT actually reads.
var coalesceFirstArgRegex = regexp.MustCompile(`(?i)^COALESCE\(\s*([a-z_][a-z0-9_]*)\s*[,)]`)

// normalizeSelectExpression returns the underlying column name for a SELECT
// list expression, or "" if it's a derived expression (function call,
// arithmetic) that doesn't map 1:1 to a column.
//
// Handles:
//   - bare ident:                       `col`           → `col`
//   - qualified ident:                  `t.col`         → `col`
//   - aliased ident:                    `col AS alias`  → `col`
//   - COALESCE wrap (LoopZero JSONB default): `COALESCE(col, 'x')` → `col`
//
// Returns "" for: wildcards, function calls other than COALESCE, arithmetic
// expressions, string literals, anything that doesn't reduce to a single
// schema column reference.
func normalizeSelectExpression(part string) string {
	s := strings.TrimSpace(part)
	if s == "" {
		return ""
	}
	// COALESCE(col, default)[::type] — peel to the inner column name.
	// Must be tried BEFORE the alias-strip below because the alias suffix
	// (` AS x`) often follows the COALESCE() close-paren and is irrelevant
	// to the column we read.
	if upper := strings.ToUpper(s); strings.HasPrefix(upper, "COALESCE(") {
		if m := coalesceFirstArgRegex.FindStringSubmatch(s); len(m) == 2 {
			return m[1]
		}
		return ""
	}
	// Strip leading qualifier (e.g., `t.col` → `col`).
	if dot := strings.LastIndex(s, "."); dot >= 0 {
		s = s[dot+1:]
	}
	// Strip alias ("col AS alias" → "col"). The "AS" is case-insensitive.
	lower := strings.ToLower(s)
	if asIdx := strings.Index(lower, " as "); asIdx >= 0 {
		s = strings.TrimSpace(s[:asIdx])
	}
	// Function calls / expressions: skip.
	if strings.ContainsAny(s, "()+-*/'\"") {
		return ""
	}
	// Wildcards: skip (mirror only catches explicit columns).
	if s == "*" {
		return ""
	}
	// Reject anything with whitespace remaining (compound expression
	// without explicit AS — e.g., `col col2`).
	if strings.ContainsAny(s, " \t") {
		return ""
	}
	return s
}

// ExtractScanFieldsFromGoFile parses a Go source file and returns the
// field-access path of every argument passed to `row.Scan(&r.X, ...)` inside
// the named function. Returns the bare field names (e.g., "ID", "BuildID")
// in argument order.
//
// LoopZero style: scanReleaseFields takes `r *Release` and calls
// `row.Scan(&r.ID, &r.BuildID, ...)`. The helper recognizes the
// `&<receiver>.<Field>` pattern; nested selectors return empty (out of scope).
func ExtractScanFieldsFromGoFile(filename, funcName string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != funcName {
			return true
		}
		// Walk the body looking for `row.Scan(...)` (or `.Scan(...)`)
		// calls and harvest their argument idents.
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			call, ok := inner.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "Scan" {
				return true
			}
			for _, arg := range call.Args {
				name := unaryDotFieldName(arg)
				if name != "" {
					out = append(out, name)
				}
			}
			return true
		})
		return false
	})
	return out, nil
}

// unaryDotFieldName returns "FieldName" for `&receiver.FieldName`, "" otherwise.
func unaryDotFieldName(expr ast.Expr) string {
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return ""
	}
	sel, ok := unary.X.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return ""
	}
	return sel.Sel.Name
}

// ToSnakeCase converts a Go field name to PG snake_case so struct-field
// extractions can be diffed against DB column names. Handles common LoopZero
// acronyms (ID, URL, SHA, SBOM) by treating them as single tokens.
//
// Examples:
//   - "ID"                         → "id"
//   - "BuildID"                    → "build_id"
//   - "ImageDigest"                → "image_digest"
//   - "ImageAttestationRekorURL"   → "image_attestation_rekor_url"
//   - "CreatedAt"                  → "created_at"
func ToSnakeCase(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		isUpper := r >= 'A' && r <= 'Z'
		if isUpper && i > 0 {
			prev := runes[i-1]
			// Insert underscore before a capital that follows a lowercase,
			// OR before the last capital of an acronym followed by a
			// lowercase (e.g., the "U" of "URL" before "...Url" -> ok;
			// the "I" of "ID" before lower is "_id"). The standard rule
			// covers both: insert "_" when prev is lower, or when prev
			// is upper AND next is lower.
			needsSep := (prev >= 'a' && prev <= 'z')
			if !needsSep && i+1 < len(runes) {
				next := runes[i+1]
				if next >= 'a' && next <= 'z' {
					needsSep = true
				}
			}
			if needsSep {
				b.WriteByte('_')
			}
		}
		if isUpper {
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
