// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package moduleselfcontained

import (
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// What `go build` printed in the fork-safe CI job, which receives no credential for the
// private sibling module: the build never reached the compiler, so it says nothing about
// whether the module stands alone.
const privateModuleWithoutCredential = `go: downloading github.com/getloopscale/contracts v0.4.0
cmd/setup_auth.go:18:2: reading github.com/getloopscale/contracts/go.mod at revision v0.4.0: git ls-remote -q --end-of-options https://github.com/getloopscale/contracts in /home/runner/go/pkg/mod/cache/vcs/3ec5: exit status 128:
	fatal: could not read Username for 'https://github.com': terminal prompts disabled
Confirm the import path was entered correctly.
If this is a private repository, see https://golang.org/doc/faq#git_https for additional information.`

func TestDependencyThatCannotBeFetchedForLackOfCredentialsIsNamed(t *testing.T) {
	mods, ok := unreachableDependencies(privateModuleWithoutCredential)
	if !ok || len(mods) != 1 || mods[0] != "github.com/getloopscale/contracts" {
		t.Fatalf("an authentication failure fetching a dependency must be recognised and name it; got %v, %v", mods, ok)
	}
}

// A version that does not exist upstream is a real defect of the module — a tag that
// was never pushed breaks every isolated build — and must stay a failure.
func TestDependencyVersionThatDoesNotExistIsNotAnAccessProblem(t *testing.T) {
	out := `cmd/x.go:3:2: reading github.com/example/dep/go.mod at revision v9.9.9: unknown revision v9.9.9`
	if mods, ok := unreachableDependencies(out); ok {
		t.Fatalf("an unknown revision is a defect, not an access problem; got %v", mods)
	}
}

func TestGoModInconsistencyBesideAnAccessFailureIsStillADefect(t *testing.T) {
	out := privateModuleWithoutCredential + "\ngo: updates to go.mod needed; to update it:\n\tgo mod tidy"
	if _, ok := unreachableDependencies(out); ok {
		t.Fatal("a go.mod inconsistency must not be hidden behind an access failure")
	}
}

func TestCompileErrorBesideAnAccessFailureIsStillADefect(t *testing.T) {
	out := privateModuleWithoutCredential + "\ninternal/y.go:9:4: undefined: Foo"
	if _, ok := unreachableDependencies(out); ok {
		t.Fatal("a compile error must not be hidden behind an access failure")
	}
}

func TestModuleWhoseDependencyIsUnreachableIsNotVerifiedNotFailed(t *testing.T) {
	root := tree(t, "api", "operators")
	b := &fakeBuilder{failures: map[string]error{
		"api": &UnreachableDependencyError{Dependencies: []string{"github.com/getloopscale/contracts"}, Output: privateModuleWithoutCredential},
	}}
	out, err := Check(Config{RepoRoot: root, Builder: b})
	if err != nil {
		t.Fatalf("an unreachable dependency is not a failure to run the check: %v", err)
	}
	if out.Violation != nil {
		t.Fatalf("an unreachable dependency proves nothing against the module: %v", out.Violation)
	}
	if got := out.Verdict(); got != verdict.NotVerified {
		t.Fatalf("Verdict() = %v, want NOT VERIFIED", got)
	}
	if len(out.Unreachable) != 1 || out.Unreachable[0].Module != "api" {
		t.Fatalf("the module left unverified must be named: %+v", out.Unreachable)
	}
}

func TestAViolationOutranksAnUnreachableDependency(t *testing.T) {
	root := tree(t, "api", "operators")
	b := &fakeBuilder{failures: map[string]error{
		"api":       &UnreachableDependencyError{Dependencies: []string{"github.com/getloopscale/contracts"}},
		"operators": errorString("go: updates to go.mod needed"),
	}}
	out, _ := Check(Config{RepoRoot: root, Builder: b})
	if out.Verdict() != verdict.Fail {
		t.Fatalf("a module that does not stand alone fails whatever else is unverified; got %v", out.Verdict())
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }
