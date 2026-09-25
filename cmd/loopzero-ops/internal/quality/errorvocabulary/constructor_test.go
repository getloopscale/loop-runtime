// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package errorvocabulary

import (
	"strings"
	"testing"
)

// A handler that builds its error first and writes it later authors the message
// in the constructor, not in the writer's argument. Measured 2026-09-25: moving
// the upload, build and team handlers to that shape took 37 of 168 messages out
// of the scan, and the gate stayed green while seeing less.

const constructorStubs = `
type LoopZeroError struct {
	Code    string
	Message string
}

func NewLoopZeroError(code, message string) *LoopZeroError { return &LoopZeroError{Code: code, Message: message} }

var loopzeroerrors struct {
	NewLoopZeroError func(string, string) *LoopZeroError
}

var httpio struct {
	WriteLoopZeroError func(any, *LoopZeroError)
}
`

func TestFailsBannedTermInAMessageAuthoredByTheConstructor(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/routes/x/x.go", `package x
`+constructorStubs+`
func reject() *LoopZeroError {
	return loopzeroerrors.NewLoopZeroError("SYNC_FAILED", "ArgoCD could not sync the release")
}
`)
	writeFile(t, root, "api/internal/services/y/y.go", "package y\n")

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Violations) != 1 || !strings.Contains(out.Violations[0].Message, "ArgoCD") {
		t.Fatalf("a constructor message naming ArgoCD must be one violation, got %+v", out.Violations)
	}
}

func TestFailsBannedTermInAMessageFieldOfALoopZeroErrorLiteral(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/routes/x/x.go", `package x
`+constructorStubs+`
func reject() *LoopZeroError {
	return &LoopZeroError{Code: "PUSH_FAILED", Message: "the Gitea push was refused"}
}
`)
	writeFile(t, root, "api/internal/services/y/y.go", "package y\n")

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Violations) != 1 || out.Violations[0].Term != "gitea" {
		t.Fatalf("a Message field naming Gitea must be one violation, got %+v", out.Violations)
	}
}

// The code is a machine-readable contract (`--json` consumers match on it), not
// prose a person reads; a code like GITOPS_PUSH_FAILED is not a Rule 3 leak.
func TestIgnoresTheErrorCodeOfAConstructor(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/routes/x/x.go", `package x
`+constructorStubs+`
func reject() *LoopZeroError {
	return loopzeroerrors.NewLoopZeroError("GITOPS_PUSH_FAILED", "The deploy could not be recorded")
}
`)
	writeFile(t, root, "api/internal/services/y/y.go", "package y\n")

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Fatalf("an error code was judged as a message: %+v", out.Violations)
	}
	if out.Messages != 1 {
		t.Errorf("want the one authored message counted, got %d", out.Messages)
	}
}

// A constructor inside a writer's argument is one message, counted once.
func TestCountsAConstructorInsideAWriterOnce(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/routes/x/x.go", `package x
`+constructorStubs+`
func h() {
	httpio.WriteLoopZeroError(nil, loopzeroerrors.NewLoopZeroError("SYNC_FAILED", "ArgoCD could not sync"))
}
`)
	writeFile(t, root, "api/internal/services/y/y.go", "package y\n")

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Violations) != 1 {
		t.Fatalf("want exactly one violation, got %d: %+v", len(out.Violations), out.Violations)
	}
}

// Inside a writer too, a constructor's code is not a message: counting it
// would inflate the population the floor is measured against.
func TestIgnoresTheCodeOfAConstructorInsideAWriter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "api/internal/routes/x/x.go", `package x
`+constructorStubs+`
func h() {
	httpio.WriteLoopZeroError(nil, loopzeroerrors.NewLoopZeroError("GITOPS_PUSH_FAILED", "The deploy could not be recorded"))
}
`)
	writeFile(t, root, "api/internal/services/y/y.go", "package y\n")

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass || out.Messages != 1 {
		t.Fatalf("want one message and no violation, got %d message(s): %+v", out.Messages, out.Violations)
	}
}
