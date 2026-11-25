// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package renderschemagensql

import (
	"strings"
	"testing"
)

// TestPostProcess_StripsAlterOwnerTo confirms the sed § A1 line drop.
func TestPostProcess_StripsAlterOwnerTo(t *testing.T) {
	raw := "CREATE TABLE foo (id int);\nALTER TABLE foo OWNER TO postgres;\n"
	out := postProcess(raw)
	if strings.Contains(out, "ALTER TABLE foo OWNER TO") {
		t.Fatalf("expected ALTER TABLE OWNER TO stripped, got %q", out)
	}
	if !strings.Contains(out, "CREATE TABLE foo") {
		t.Fatalf("post-process dropped a legitimate CREATE line: %q", out)
	}
}

func TestPostProcess_StripsDumpedFromBanner(t *testing.T) {
	raw := "-- Dumped from database version 16.0\n-- Dumped by pg_dump version 16.0\nCREATE TABLE foo (id int);\n"
	out := postProcess(raw)
	if strings.Contains(out, "Dumped from") || strings.Contains(out, "Dumped by") {
		t.Fatalf("expected pg_dump banner stripped, got %q", out)
	}
}

func TestPostProcess_StripsSetStatements(t *testing.T) {
	raw := "SET client_encoding = 'UTF8';\nSET default_tablespace = '';\nCREATE TABLE foo (id int);\n"
	out := postProcess(raw)
	if strings.Contains(out, "SET ") {
		t.Fatalf("expected SET statements stripped, got %q", out)
	}
}

func TestPostProcess_StripsRestrictLines(t *testing.T) {
	raw := "\\restrict abcd\nCREATE TABLE foo (id int);\n\\unrestrict abcd\n"
	out := postProcess(raw)
	if strings.Contains(out, "\\restrict") || strings.Contains(out, "\\unrestrict") {
		t.Fatalf("expected \\restrict/\\unrestrict stripped, got %q", out)
	}
}

func TestPostProcess_StripsOrphanDashDash(t *testing.T) {
	raw := "CREATE TABLE foo (id int);\n--\nCREATE TABLE bar (id int);\n"
	out := postProcess(raw)
	if strings.Contains(out, "\n--\n") {
		t.Fatalf("expected orphan `--` line stripped, got %q", out)
	}
}

func TestPostProcess_StripsTrailingWhitespace(t *testing.T) {
	raw := "CREATE TABLE foo (id int);   \nCREATE TABLE bar (id int);\t\n"
	out := postProcess(raw)
	if strings.Contains(out, "int);   ") || strings.Contains(out, "int);\t") {
		t.Fatalf("expected trailing whitespace trimmed, got %q", out)
	}
}

func TestPostProcess_CollapsesBlankLines(t *testing.T) {
	raw := "CREATE TABLE foo (id int);\n\n\n\nCREATE TABLE bar (id int);\n"
	out := postProcess(raw)
	if strings.Contains(out, "\n\n\n") {
		t.Fatalf("expected consecutive blank lines collapsed, got %q", out)
	}
	if !strings.Contains(out, "\n\n") {
		t.Fatalf("expected exactly one blank line between blocks, got %q", out)
	}
}

// TestCheck_MissingToolWarns confirms the advisory-mode Check exits WARN
// when a tool is absent.
func TestCheck_MissingToolWarns(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	outcome, err := Check(Config{RepoRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("expected advisory Warn, got err=%v", err)
	}
	if !outcome.Warn {
		t.Fatalf("expected Warn=true when tools absent, got %+v", outcome)
	}
}

// The postgres image runs its init scripts against a temporary server that
// listens on the unix socket alone and is then restarted. A readiness probe on
// the socket answers during that phase, and the migration that follows connects
// over TCP to a server about to go away.
func TestReadinessProbeAsksOverTCP(t *testing.T) {
	args := strings.Join(pgReadyArgs("cid-1"), " ")
	if !strings.Contains(args, "-h 127.0.0.1") {
		t.Errorf("pg_isready args = %q, want the TCP host so the init-phase server does not count as ready", args)
	}
}
