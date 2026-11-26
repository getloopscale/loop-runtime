// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"bytes"
	"strings"
	"testing"
)

func bootstrapUserArgs() []string {
	return []string{"bootstrap-user",
		"--user-id", "loopruntime-bootstrap", "--email", "bootstrap@loopruntime.local",
		"--display-name", "Bootstrap User", "--username", "bootstrap",
		"--account-id", "acct-bootstrap", "--tenant-id", "tn-e2etest", "--role", "owner"}
}

func TestSQLBootstrapUserPrintsTheSeedScript(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runSQL(bootstrapUserArgs(), &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "DELETE FROM tenant_members WHERE user_id = 'loopruntime-bootstrap' AND tenant_id <> 'tn-e2etest';") {
		t.Errorf("stdout is not the bootstrap-user script:\n%s", out.String())
	}
}

// A missing flag is refused before anything is printed: a caller piping stdout
// into psql must not receive a partial script.
func TestSQLBootstrapUserWithAMissingFieldExitsOneAndPrintsNothing(t *testing.T) {
	args := bootstrapUserArgs()
	args = args[:len(args)-2] // drop --role owner
	var out, errOut bytes.Buffer
	if code := runSQL(args, &out, &errOut); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", out.String())
	}
}

func TestSQLUnknownScriptIsAUsageError(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runSQL([]string{"drop-everything"}, &out, &errOut); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

func TestSQLBootstrapUserRejectsAPositionalArgument(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runSQL(append(bootstrapUserArgs(), "extra"), &out, &errOut); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

func TestMainDispatch_SQLRouted(t *testing.T) {
	if code := run([]string{"sql", "--help"}); code != 0 {
		t.Errorf("run sql --help: exit %d, want 0", code)
	}
}
