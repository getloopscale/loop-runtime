// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cleanroom

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The dev loop kept its own copy of the migration process: a host `psql -f` loop
// over every *.up.sql with no version tracking, then a stamp of schema_migrations
// to version 10. The Taskfile replaced that loop (F-1) with `task db:migrate`
// (golang-migrate, psql inside the Postgres pod); the Tilt copy stayed. On a
// machine without psql it failed outright, and on a database at 68 the stamp
// would have moved the version back to 10.

func tiltResource(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "Tiltfile"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	start := strings.Index(src, `"`+name+`",`)
	if start < 0 {
		t.Fatalf("the Tiltfile declares no resource %q", name)
	}
	end := strings.Index(src[start:], "\n)\n")
	if end < 0 {
		t.Fatalf("resource %q has no closing parenthesis", name)
	}
	return src[start : start+end]
}

func TestTheDevLoopMigratesThroughTheCanonicalTask(t *testing.T) {
	block := tiltResource(t, "db-migrate")
	if !strings.Contains(block, "task db:migrate") {
		t.Errorf("db-migrate does not run `task db:migrate`; it carries its own copy of the migration process:\n%s", block)
	}
}

func TestTheDevLoopNeverAppliesMigrationFilesByHand(t *testing.T) {
	block := tiltResource(t, "db-migrate")
	for _, forbidden := range []*regexp.Regexp{
		regexp.MustCompile(`for f in [^;]*\.up\.sql`),
		regexp.MustCompile(`(?i)INSERT INTO schema_migrations`),
	} {
		if forbidden.MatchString(block) {
			t.Errorf("db-migrate still %s — an untracked re-apply, or a version stamp that can move the database backwards", forbidden)
		}
	}
}
