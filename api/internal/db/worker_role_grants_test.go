// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// loopruntime_worker is the one role that may cross tenants under fail-closed Row
// Level Security (migration 000062): the background loops and the lookups
// that span tenants run on it. Migration 000023 granted it an explicit table
// list, and every table created after it was left with no privilege at all —
// nothing failed until a cross-tenant path reached one of them in production
// and was refused with `permission denied` (issue #385).
//
// This test confronts the two sources that must agree, without a database:
// the privileges the migrations grant loopruntime_worker, and the tables and verbs
// the engine's SQL executes (the sqlc query files plus every SQL string
// literal in the api module). The privileges are least privilege derived from
// the code, so the rule is one-directional: every (table, verb) the engine
// runs must be granted, except on a table the engine touches ONLY on the
// request pool — declared below with the evidence, and required to hold no
// grant at all.
//
// What it cannot see: SQL assembled at runtime (a table name spliced in with
// fmt.Sprintf) and which pool a given statement runs on. The second is why
// the request-only tables are a declaration rather than a derivation: pool
// assignment is a runtime value threaded through transactions, and a
// store-level approximation flags tables no background path ever touches
// (setupStores builds every store on the background pool, including ones no
// loop receives). tests/integration/worker_role_grants_test.go exercises the
// real paths as loopruntime_worker.

// requestPoolOnly lists the tables the engine queries but never on the
// cross-tenant pool. Each carries the evidence; the test also requires that
// loopruntime_worker holds no privilege on it.
var requestPoolOnly = map[string]string{
	"preview_slot": "read and written only through StoreDeps.PreviewSlotStore of the request " +
		"stores (cmd/setup_server.go newEnvironmentHandler and wirePreviews, cmd/setup_github.go " +
		"preview service); backgroundloops.Deps carries no preview slot store and the controller " +
		"builds none",
	"rollbacks": "written and read only through the RollbackStoreSqlc that cmd/setup_orchestrator.go " +
		"builds over the request pool, for the rollback orchestrator and the handler that lists " +
		"rollbacks; a rollback is closed by the request's own context, which carries the tenant, " +
		"and neither backgroundloops.Deps nor the controller builds this store",
}

// A statement the engine runs, and where it was found.
type engineStatement struct {
	origin string
	sql    string
}

// A privilege one statement needs on one table.
type tableAccess struct {
	table string
	verb  string
}

func TestTheCrossTenantRoleHoldsEveryPrivilegeTheEngineSQLNeeds(t *testing.T) {
	apiRoot := apiModuleRoot(t)
	schema := replayWorkerGrants(t, filepath.Join(apiRoot, "internal", "db", "migrations"))
	statements := engineStatements(t, apiRoot)
	if len(statements) < 100 {
		t.Fatalf("found %d engine SQL statements; the scan is not reading the query files", len(statements))
	}

	missing := map[string][]string{}
	for _, st := range statements {
		for _, a := range accessesOf(st.sql, schema.tables) {
			if _, onlyRequest := requestPoolOnly[a.table]; onlyRequest {
				continue
			}
			if !schema.tableGrants[a.table][a.verb] {
				key := a.verb + " ON " + a.table
				missing[key] = append(missing[key], st.origin)
			}
			if a.verb == "INSERT" {
				if seq, ok := schema.serialSequence[a.table]; ok && !schema.sequenceUsage[seq] {
					key := "USAGE ON SEQUENCE " + seq
					missing[key] = append(missing[key], st.origin)
				}
			}
		}
	}
	for _, key := range sortedKeys(missing) {
		t.Errorf("loopruntime_worker lacks %s, which the engine runs (%s); grant it in a new migration, "+
			"or, if no cross-tenant path runs it, declare the table in requestPoolOnly with the evidence",
			key, strings.Join(uniqueSorted(missing[key]), ", "))
	}
}

// A request-only declaration is a claim that the cross-tenant role needs
// nothing on the table. The claim is checked both ways: the role holds no
// privilege there, and the engine still queries the table (a stale entry
// would silently excuse whatever query appears on it next).
func TestTheRequestOnlyTablesGrantTheCrossTenantRoleNothing(t *testing.T) {
	apiRoot := apiModuleRoot(t)
	schema := replayWorkerGrants(t, filepath.Join(apiRoot, "internal", "db", "migrations"))
	queried := map[string]bool{}
	for _, st := range engineStatements(t, apiRoot) {
		for _, a := range accessesOf(st.sql, schema.tables) {
			queried[a.table] = true
		}
	}
	for _, table := range sortedKeys(requestPoolOnly) {
		if !schema.tables[table] {
			t.Errorf("requestPoolOnly names %q, which no migration creates", table)
		}
		if !queried[table] {
			t.Errorf("requestPoolOnly names %q, which no engine statement queries; remove the entry", table)
		}
		if granted := sortedKeys(schema.tableGrants[table]); len(granted) > 0 {
			t.Errorf("loopruntime_worker holds %v on %q, declared request-only; revoke it in a new migration", granted, table)
		}
	}
}

// The parser must see the grants it is judged by. Pinned against migration
// 000023 and 000046, whose grants are known, so a regex that silently stopped
// matching cannot turn the test above green.
func TestTheGrantReplayReadsTheKnownWorkerGrants(t *testing.T) {
	schema := replayWorkerGrants(t, filepath.Join(apiModuleRoot(t), "internal", "db", "migrations"))
	for _, want := range []tableAccess{
		{"builds", "DELETE"}, {"tenants", "SELECT"}, {"work_queue", "INSERT"},
		{"metering_cursor", "UPDATE"}, {"metering_config", "SELECT"},
	} {
		if !schema.tableGrants[want.table][want.verb] {
			t.Errorf("replay lost %s on %s", want.verb, want.table)
		}
	}
	if schema.tableGrants["metering_config"]["UPDATE"] {
		t.Error("replay invented UPDATE on metering_config; 000046 grants SELECT only")
	}
	if !schema.sequenceUsage["audit_log_id_seq"] {
		t.Error("replay lost 000023's grant on the sequences that existed before it")
	}
}

// The access extractor is what the first test trusts; these pin the shapes
// the engine's SQL actually uses.
func TestTheAccessExtractorReadsTheVerbsOfEachStatementShape(t *testing.T) {
	tables := map[string]bool{"accounts": true, "tenants": true, "builds": true, "releases": true,
		"metering_cursor": true, "leader_generations": true, "cost_counters": true}
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"insert on conflict do nothing", "INSERT INTO accounts (id) VALUES ($1) ON CONFLICT (owner_id) DO NOTHING",
			[]string{"INSERT accounts"}},
		{"select with subselect", "SELECT id FROM tenants WHERE account_id = (SELECT id FROM accounts WHERE owner_id=$1)",
			[]string{"SELECT accounts", "SELECT tenants"}},
		{"delete with subselect", "DELETE FROM releases WHERE build_id IN (SELECT id FROM builds WHERE tenant_id = ANY($1))",
			[]string{"DELETE releases", "SELECT builds", "SELECT releases"}},
		{"row lock is not an update", "SELECT id FROM builds WHERE id=$1 FOR UPDATE",
			[]string{"SELECT builds"}},
		{"upsert", "INSERT INTO tenants (id) VALUES ($1) ON CONFLICT (id) DO UPDATE SET slug = EXCLUDED.slug",
			[]string{"INSERT tenants", "SELECT tenants", "UPDATE tenants"}},
		{"data-modifying CTE", "WITH adv AS (UPDATE metering_cursor SET x = now() WHERE a = @gen AND @gen = " +
			"(SELECT generation_id FROM leader_generations) RETURNING 1) INSERT INTO cost_counters (a) SELECT 1 FROM adv",
			[]string{"INSERT cost_counters", "SELECT leader_generations", "SELECT metering_cursor", "UPDATE metering_cursor"}},
		{"returning", "INSERT INTO builds (id) VALUES ($1) RETURNING id",
			[]string{"INSERT builds", "SELECT builds"}},
		{"function argument is not a table", "SELECT EXTRACT(EPOCH FROM now()) FROM builds",
			[]string{"SELECT builds"}},
	}
	for _, c := range cases {
		var got []string
		for _, a := range accessesOf(c.sql, tables) {
			got = append(got, a.verb+" "+a.table)
		}
		sort.Strings(got)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: accesses = %v; want %v", c.name, got, c.want)
		}
	}
}

// --- migrations ------------------------------------------------------------

type workerSchema struct {
	tables         map[string]bool
	tableGrants    map[string]map[string]bool
	serialSequence map[string]string // table -> its SERIAL/BIGSERIAL sequence
	sequenceUsage  map[string]bool
	// Grants ALTER DEFAULT PRIVILEGES gives tables and sequences created later.
	defaultTableVerbs map[string]bool
	defaultSeqUsage   bool
}

var (
	createTableRe = regexp.MustCompile(`(?i)\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:public\.)?([a-z_][a-z0-9_]*)\s*\(`)
	serialColRe   = regexp.MustCompile(`(?i)^\s*([a-z_][a-z0-9_]*)\s+(?:BIG|SMALL)?SERIAL\b`)
	workerGrantRe = regexp.MustCompile(`(?is)\b(GRANT|REVOKE)\s+([a-z ,]+?)\s+ON\s+([^;']+?)\s+(?:TO|FROM)\s+loopruntime_worker\b`)
)

var tableVerbs = []string{"SELECT", "INSERT", "UPDATE", "DELETE"}

// replayWorkerGrants applies every up migration in order and returns what
// loopruntime_worker holds at the end. "ALL ... IN SCHEMA" grants resolve against
// the objects that exist at that point of the replay, as PostgreSQL does.
func replayWorkerGrants(t *testing.T, dir string) workerSchema {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("list migrations in %s: %v (found %d)", dir, err, len(files))
	}
	sort.Strings(files)
	s := workerSchema{
		tables: map[string]bool{}, tableGrants: map[string]map[string]bool{},
		serialSequence: map[string]string{}, sequenceUsage: map[string]bool{},
		defaultTableVerbs: map[string]bool{},
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		sql := withoutSQLComments(string(raw))
		type event struct {
			at    int
			apply func()
		}
		var events []event
		for _, m := range createTableRe.FindAllStringSubmatchIndex(sql, -1) {
			table := strings.ToLower(sql[m[2]:m[3]])
			body := parenthesized(sql[m[1]-1:])
			events = append(events, event{m[0], func() { s.createTable(table, body) }})
		}
		for _, m := range workerGrantRe.FindAllStringSubmatchIndex(sql, -1) {
			grant := strings.EqualFold(sql[m[2]:m[3]], "GRANT")
			privs, objects := sql[m[4]:m[5]], sql[m[6]:m[7]]
			events = append(events, event{m[0], func() { s.applyGrant(grant, privs, objects) }})
		}
		sort.Slice(events, func(i, j int) bool { return events[i].at < events[j].at })
		for _, e := range events {
			e.apply()
		}
	}
	return s
}

func (s *workerSchema) createTable(table, body string) {
	s.tables[table] = true
	s.setTable(table, true, s.defaultTableVerbs)
	for _, line := range strings.Split(body, "\n") {
		if m := serialColRe.FindStringSubmatch(line); m != nil {
			seq := table + "_" + strings.ToLower(m[1]) + "_seq"
			s.serialSequence[table] = seq
			if s.defaultSeqUsage {
				s.sequenceUsage[seq] = true
			}
		}
	}
}

func (s *workerSchema) applyGrant(grant bool, privs, objects string) {
	objects = strings.ToLower(strings.Join(strings.Fields(objects), " "))
	verbs := map[string]bool{}
	for _, p := range strings.Split(privs, ",") {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p == "ALL" || p == "ALL PRIVILEGES" {
			for _, v := range append([]string{"USAGE"}, tableVerbs...) {
				verbs[v] = true
			}
			continue
		}
		verbs[p] = true
	}
	switch {
	case strings.HasPrefix(objects, "database"), strings.HasPrefix(objects, "schema"):
		return
	case objects == "tables": // ALTER DEFAULT PRIVILEGES ... ON TABLES
		for _, v := range tableVerbs {
			if verbs[v] {
				s.defaultTableVerbs[v] = grant
			}
		}
	case objects == "sequences": // ALTER DEFAULT PRIVILEGES ... ON SEQUENCES
		s.defaultSeqUsage = grant && (verbs["USAGE"] || verbs["UPDATE"])
	case objects == "all sequences in schema public":
		for _, seq := range s.serialSequence {
			s.setSequence(seq, grant, verbs)
		}
	case strings.HasPrefix(objects, "sequence "):
		for _, seq := range objectList(strings.TrimPrefix(objects, "sequence ")) {
			s.setSequence(seq, grant, verbs)
		}
	case objects == "all tables in schema public":
		for table := range s.tables {
			s.setTable(table, grant, verbs)
		}
	default:
		for _, table := range objectList(strings.TrimPrefix(objects, "table ")) {
			s.setTable(table, grant, verbs)
		}
	}
}

func (s *workerSchema) setSequence(seq string, grant bool, verbs map[string]bool) {
	if verbs["USAGE"] || verbs["UPDATE"] {
		s.sequenceUsage[seq] = grant
	}
}

func (s *workerSchema) setTable(table string, grant bool, verbs map[string]bool) {
	if s.tableGrants[table] == nil {
		s.tableGrants[table] = map[string]bool{}
	}
	for _, v := range tableVerbs {
		if !verbs[v] {
			continue
		}
		if grant {
			s.tableGrants[table][v] = true
		} else {
			delete(s.tableGrants[table], v)
		}
	}
}

func objectList(list string) []string {
	var out []string
	for _, o := range strings.Split(list, ",") {
		o = strings.TrimPrefix(strings.TrimSpace(o), "public.")
		if o != "" {
			out = append(out, o)
		}
	}
	return out
}

// parenthesized returns the text of the balanced parenthesis group s opens.
func parenthesized(s string) string {
	depth := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i]
			}
		}
	}
	return s
}

var (
	lineCommentRe  = regexp.MustCompile(`--[^\n]*`)
	blockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

func withoutSQLComments(sql string) string {
	return lineCommentRe.ReplaceAllString(blockCommentRe.ReplaceAllString(sql, " "), " ")
}

// --- engine SQL ------------------------------------------------------------

func apiModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve this test's path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

var sqlStartRe = regexp.MustCompile(`(?i)^\s*(SELECT|INSERT|UPDATE|DELETE|WITH)\s`)

// engineStatements returns every named query of the sqlc query files and
// every SQL string literal in the api module's production Go code.
func engineStatements(t *testing.T, apiRoot string) []engineStatement {
	t.Helper()
	var out []engineStatement
	queryFiles, err := filepath.Glob(filepath.Join(apiRoot, "internal", "db", "queries", "*.sql"))
	if err != nil || len(queryFiles) == 0 {
		t.Fatalf("list query files: %v (found %d)", err, len(queryFiles))
	}
	for _, f := range queryFiles {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, block := range strings.Split(string(raw), "-- name: ")[1:] {
			name := strings.Fields(block)[0]
			out = append(out, engineStatement{
				origin: filepath.Base(f) + ":" + name,
				sql:    withoutSQLComments(block[strings.Index(block, "\n")+1:]),
			})
		}
	}
	generated := filepath.Join(apiRoot, "internal", "db", "generated")
	walkErr := filepath.WalkDir(apiRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == generated || d.Name() == "testdata" || d.Name() == "tests" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		out = append(out, sqlLiteralsIn(t, apiRoot, path)...)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", apiRoot, walkErr)
	}
	return out
}

func sqlLiteralsIn(t *testing.T, apiRoot, path string) []engineStatement {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	rel, _ := filepath.Rel(apiRoot, path)
	var out []engineStatement
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(lit.Value)
		if err != nil || !sqlStartRe.MatchString(text) {
			return true
		}
		out = append(out, engineStatement{
			origin: rel + ":" + strconv.Itoa(fset.Position(lit.Pos()).Line),
			sql:    withoutSQLComments(text),
		})
		return true
	})
	return out
}

// --- access extraction -----------------------------------------------------

var sqlTokenRe = regexp.MustCompile(`[a-z_][a-z0-9_.]*|\S`)

// accessesOf returns the privileges one statement needs, restricted to the
// tables the migrations create (CTE names and functions fall out of it).
// UPDATE and DELETE read the rows they filter, and RETURNING reads the rows
// it returns, so both also need SELECT — as PostgreSQL requires.
func accessesOf(sql string, tables map[string]bool) []tableAccess {
	tokens := sqlTokenRe.FindAllString(strings.ToLower(sql), -1)
	next := func(i int) string {
		for j := i + 1; j < len(tokens); j++ {
			if tokens[j] != "only" {
				return strings.TrimPrefix(tokens[j], "public.")
			}
		}
		return ""
	}
	prev := func(i int) string {
		if i == 0 {
			return ""
		}
		return tokens[i-1]
	}
	seen := map[tableAccess]bool{}
	add := func(table, verb string) {
		if tables[table] {
			seen[tableAccess{table, verb}] = true
		}
	}
	var dmlTarget string
	for i, tok := range tokens {
		switch {
		case tok == "insert" && next(i) == "into":
			dmlTarget = next(i + 1)
			add(dmlTarget, "INSERT")
		case tok == "update" && !isNonStatementUpdate(prev(i)):
			dmlTarget = next(i)
			add(dmlTarget, "UPDATE")
			add(dmlTarget, "SELECT")
		case tok == "update" && prev(i) == "do":
			add(dmlTarget, "UPDATE")
			add(dmlTarget, "SELECT")
		case tok == "delete" && next(i) == "from":
			dmlTarget = next(i + 1)
			add(dmlTarget, "DELETE")
			add(dmlTarget, "SELECT")
		case tok == "returning":
			add(dmlTarget, "SELECT")
		case (tok == "from" || tok == "join" || tok == "using") && prev(i) != "delete":
			add(next(i), "SELECT")
		}
	}
	out := make([]tableAccess, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	return out
}

// isNonStatementUpdate recognises UPDATE words that are not an UPDATE
// statement: row locks (FOR [NO KEY] UPDATE), the upsert branch (DO UPDATE,
// handled apart) and referential actions (ON UPDATE).
func isNonStatementUpdate(prev string) bool {
	return prev == "for" || prev == "key" || prev == "do" || prev == "on"
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func uniqueSorted(in []string) []string {
	set := map[string]bool{}
	for _, s := range in {
		set[s] = true
	}
	return sortedKeys(set)
}
