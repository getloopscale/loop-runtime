// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package lintcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner replaces the real execution. It is the seam `B-121` requires: its DoD asks for
// "a test that proves a finding in `pkg` does not keep `api` from being linted", and such a test
// cannot depend on actually running `golangci-lint` — it would be slow, would depend on the tree,
// and could not FABRICATE the failure it needs to prove.
type fakeRunner struct {
	lint     map[string]int
	fmtc     map[string]int
	failures map[string]error
	seen     []string
}

func (f *fakeRunner) Lint(_, mod string) (int, error) {
	f.seen = append(f.seen, mod)
	if e, ok := f.failures[mod]; ok {
		return 0, e
	}
	return f.lint[mod], nil
}

func (f *fakeRunner) Gofmt(_, mod string) (int, error) { return f.fmtc[mod], nil }

func repoWith(t *testing.T, baseline string, mods ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, m := range mods {
		p := filepath.Join(root, m, "go.mod")
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("module x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(root, BaselineRelPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(baseline), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// THE B-121 BULLET, and the reason the item exists: `task lint` runs the modules in separate
// `cmds` and Task aborts at the first one that exits != 0 — so a finding in `pkg` kept
// `api` from being measured, and the count cited in an item named what had NOT been looked at.
func TestFailureInOneModuleDoesNotBlockTheOthers(t *testing.T) {
	root := repoWith(t, `{"lint":{"api":0,"pkg":0},"gofmt":{"api":0,"pkg":0}}`, "api", "pkg")
	r := &fakeRunner{
		lint:     map[string]int{"api": 0},
		failures: map[string]error{"pkg": errors.New("golangci-lint blew up in pkg")},
	}
	out, err := violationOf(Check(Config{RepoRoot: root, Runner: r}))

	if len(r.seen) != 2 {
		t.Fatalf("BOTH modules must be run; seen=%v", r.seen)
	}
	if err == nil {
		t.Fatal("one module's failure must show up in the aggregate verdict")
	}
	if len(out.Failed) != 1 || out.Failed[0].Module != "pkg" {
		t.Fatalf("the failure must NAME the module: %+v", out.Failed)
	}
}

// The second B-121 bullet: the output says how many PER MODULE, so that a count cited in an
// item names what was measured.
func TestOutputStatesTheCountPerModule(t *testing.T) {
	root := repoWith(t, `{"lint":{"api":0,"cmd/loopruntime-ops":221},"gofmt":{"api":70,"cmd/loopruntime-ops":0}}`, "api", "cmd/loopruntime-ops")
	r := &fakeRunner{
		lint: map[string]int{"api": 0, "cmd/loopruntime-ops": 221},
		fmtc: map[string]int{"api": 70, "cmd/loopruntime-ops": 0},
	}
	out, err := violationOf(Check(Config{RepoRoot: root, Runner: r}))
	if err != nil {
		t.Fatalf("equal to frozen passes: %v", err)
	}
	if out.Lint["cmd/loopruntime-ops"] != 221 || out.Lint["api"] != 0 {
		t.Fatalf("count per module: %+v", out.Lint)
	}
	if !strings.Contains(out.Reason, "cmd/loopruntime-ops") || !strings.Contains(out.Reason, "221") {
		t.Fatalf("the reason must carry the per-module numbers: %q", out.Reason)
	}
}

func TestRegressionFails(t *testing.T) {
	root := repoWith(t, `{"lint":{"api":0},"gofmt":{"api":0}}`, "api")
	r := &fakeRunner{lint: map[string]int{"api": 1}}
	out, err := violationOf(Check(Config{RepoRoot: root, Runner: r}))
	if !errors.Is(err, ErrRegression) {
		t.Fatalf("a regression fails; err=%v", err)
	}
	if len(out.Regressions) != 1 || out.Regressions[0].Module != "api" {
		t.Fatalf("the regression must name the module: %+v", out.Regressions)
	}
}

// B-148: `gofmt` is a dimension of its OWN, with its own ratchet — `pkg` has 30 formatting
// violations and 0 golangci-lint ones, so one dimension does not cover the other.
func TestGofmtHasItsOwnRatchet(t *testing.T) {
	root := repoWith(t, `{"lint":{"pkg":0},"gofmt":{"pkg":30}}`, "pkg")
	r := &fakeRunner{lint: map[string]int{"pkg": 0}, fmtc: map[string]int{"pkg": 31}}
	out, err := violationOf(Check(Config{RepoRoot: root, Runner: r}))
	if !errors.Is(err, ErrRegression) {
		t.Fatalf("a formatting regression fails even with lint at 0; err=%v", err)
	}
	if len(out.Regressions) != 1 || out.Regressions[0].Dimension != "gofmt" {
		t.Fatalf("the dimension must show up: %+v", out.Regressions)
	}
}

func TestUndeclaredModuleFails(t *testing.T) {
	root := repoWith(t, `{"lint":{"api":0},"gofmt":{"api":0}}`, "api", "module/new")
	r := &fakeRunner{lint: map[string]int{"api": 0, "module/new": 5}}
	if _, err := violationOf(Check(Config{RepoRoot: root, Runner: r})); !errors.Is(err, ErrUndeclaredModule) {
		t.Fatalf("a new module enters declared, never silently; err=%v", err)
	}
}

func TestMissingBaselineFailsTheGate(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "api", "go.mod")
	_ = os.MkdirAll(filepath.Dir(p), 0o750)
	_ = os.WriteFile(p, []byte("module x\n"), 0o600)
	if _, err := violationOf(Check(Config{RepoRoot: root, Runner: &fakeRunner{}})); !errors.Is(err, ErrBaselineMissing) {
		t.Fatalf("without frozen counts no green is possible; err=%v", err)
	}
}

// `--write` must MEASURE without requiring the baseline, otherwise the first freeze is
// impossible: `Check` fails on a missing baseline before measuring anything. Measuring and
// judging are different responsibilities, and the first version of this slice conflated them.
func TestMeasureDoesNotRequireBaseline(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "api", "go.mod")
	_ = os.MkdirAll(filepath.Dir(p), 0o750)
	_ = os.WriteFile(p, []byte("module x\n"), 0o600)

	r := &fakeRunner{lint: map[string]int{"api": 7}, fmtc: map[string]int{"api": 3}}
	m, err := Measure(Config{RepoRoot: root, Runner: r})
	if err != nil {
		t.Fatalf("measure without baseline: %v", err)
	}
	if m.Lint["api"] != 7 || m.Gofmt["api"] != 3 {
		t.Fatalf("measurement: %+v", m)
	}
}
