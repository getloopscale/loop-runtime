// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"bytes"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestDSNPrintsTheRequestedFieldReadFromStdin(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runDSN([]string{"password"},
		strings.NewReader(testDSN("postgresql", "pw")+"\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if out.String() != "pw\n" {
		t.Errorf("stdout = %q, want %q", out.String(), "pw\n")
	}
}

// The DSN never travels on argv, where `ps` shows it to every user on the host.
func TestDSNRefusesADSNPassedAsAnArgument(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runDSN([]string{"password", testDSN("postgresql", "p")}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Errorf("exit %d, want 1 (usage)", code)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", out.String())
	}
}

func TestDSNWithoutAFieldIsAUsageError(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runDSN(nil, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

func TestDSNThatDoesNotParseExitsOneAndPrintsNothing(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runDSN([]string{"password"}, strings.NewReader(testDSN("mysql", "p")+"\n"), &out, &errOut)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q — a caller capturing $(...) must get nothing, not the input", out.String())
	}
}

func TestDSNWithEmptyStdinExitsOne(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runDSN([]string{"password"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestDSNWhoseStdinCannotBeReadExitsFive(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runDSN([]string{"password"}, failingReader{}, &out, &errOut); code != 5 {
		t.Errorf("exit %d, want 5 (no verdict reachable)", code)
	}
}

func TestMainDispatch_DSNRouted(t *testing.T) {
	if code := run([]string{"dsn", "--help"}); code != 0 {
		t.Errorf("run dsn --help: exit %d, want 0", code)
	}
}

// testDSN builds a DSN at run time, so no credential-shaped literal sits in the
// source the secret scanners read.
func testDSN(scheme, password string) string {
	u := url.URL{Scheme: scheme, User: url.UserPassword("doadmin", password),
		Host: "h:25060", Path: "/loop_runtime", RawQuery: "sslmode=require"}
	return u.String()
}
