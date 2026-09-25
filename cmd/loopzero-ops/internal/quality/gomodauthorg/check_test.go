// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gomodauthorg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDetectsDivergentOrg reproduces the exact defect that broke the loopzero-api image build on
// merge to develop (2026-08-08): go.mod moved to getloopscale, the Dockerfile kept authenticating
// to getloopscale.
func TestDetectsDivergentOrg(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/go.mod", "module x\n\nrequire github.com/getloopscale/loopzero-contracts v0.4.0\n")
	write(t, root, "api/Dockerfile", `FROM golang
ENV GOPRIVATE=github.com/getloopscale/*
RUN git config --global url."git@github.com:getloopscale/".insteadOf "https://github.com/getloopscale/"
`)

	rep, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() {
		t.Fatal("expected a finding: go.mod requires getloopscale but the Dockerfile authenticates " +
			"to getloopscale — this is the defect that shipped twice")
	}
	if !strings.Contains(rep.Findings[0].Message, "getloopscale") {
		t.Errorf("message should name the required org, got %q", rep.Findings[0].Message)
	}
}

func TestAcceptsWhenOrgsAgree(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/go.mod", "module x\n\nrequire github.com/getloopscale/loopzero-contracts v0.4.0\n")
	write(t, root, "api/Dockerfile", `FROM golang
ENV GOPRIVATE=github.com/getloopscale/*,github.com/getloopscale/*
RUN git config --global url."git@github.com:getloopscale/".insteadOf "https://github.com/getloopscale/"
`)

	rep, _ := Check(root)
	if !rep.OK() {
		t.Errorf("orgs agree; gate must pass. findings=%+v", rep.Findings)
	}
}

// A comment naming the old org is history, not a credential path. Flagging it would push people
// to delete the record of why something changed — the opposite of what the repo wants.
func TestHistoricalCommentIsNotAFinding(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/go.mod", "module x\n\nrequire github.com/getloopscale/loopzero-contracts v0.4.0\n")
	write(t, root, "api/Dockerfile", `FROM golang
# FIXED: it used to authenticate to github.com/getloopscale/ and the build broke.
ENV GOPRIVATE=github.com/getloopscale/*
RUN git config --global url."git@github.com:getloopscale/".insteadOf "https://github.com/getloopscale/"
`)

	rep, _ := Check(root)
	if !rep.OK() {
		t.Errorf("a historical comment must not be a finding. findings=%+v", rep.Findings)
	}
}

// A file that configures no module auth at all is not this gate's business.
func TestFileWithoutAuthIsIgnored(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/go.mod", "module x\n\nrequire github.com/getloopscale/loopzero-contracts v0.4.0\n")
	write(t, root, "api/Dockerfile", "FROM golang\nRUN echo hello\n")

	rep, _ := Check(root)
	if !rep.OK() {
		t.Errorf("no auth config means nothing to check. findings=%+v", rep.Findings)
	}
}

// The real repository must pass — this is the assertion that the fix actually landed.
func TestRealRepositoryPasses(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	rep, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		for _, f := range rep.Findings {
			t.Errorf("%s: %s", f.File, f.Message)
		}
	}
	// No assertion on RequiredOrgs being non-empty: if this tree ever stops requiring a private
	// module, Check returns no findings and the test passes on its own. A skip here would only
	// restate that, and a skip is a place a test goes to stop being read.
}
