// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerator_ProducesAllDotFiles — writeAll emits one .dot per machine.
func TestGenerator_ProducesAllDotFiles(t *testing.T) {
	dir := t.TempDir()
	if err := writeAll(dir); err != nil {
		t.Fatalf("writeAll: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != len(machines()) {
		t.Errorf("expected %d .dot files, got %d", len(machines()), len(entries))
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".dot") {
			t.Errorf("unexpected non-.dot file: %s", e.Name())
		}
	}
}

// TestGenerator_DeterministicOutput — two writeAll runs produce byte-identical files.
func TestGenerator_DeterministicOutput(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	if err := writeAll(dir1); err != nil {
		t.Fatalf("writeAll dir1: %v", err)
	}
	if err := writeAll(dir2); err != nil {
		t.Fatalf("writeAll dir2: %v", err)
	}
	for name := range machines() {
		b1, _ := os.ReadFile(filepath.Join(dir1, name+".dot"))
		b2, _ := os.ReadFile(filepath.Join(dir2, name+".dot"))
		if string(b1) != string(b2) {
			t.Errorf("%s.dot differs between runs (non-deterministic)", name)
		}
	}
}

// TestCheckDrift_DetectsMissingFile — a machine with no committed .dot is drift.
func TestCheckDrift_DetectsMissingFile(t *testing.T) {
	dir := t.TempDir()
	// Write all but then remove one
	if err := writeAll(dir); err != nil {
		t.Fatalf("writeAll: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "build.dot")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	drift, err := checkDrift(dir)
	if err != nil {
		t.Fatalf("checkDrift: %v", err)
	}
	found := false
	for _, d := range drift {
		if d == "build" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'build' in drift list, got %v", drift)
	}
}

// TestCheckDrift_DetectsModifiedFile — a tampered .dot is drift.
func TestCheckDrift_DetectsModifiedFile(t *testing.T) {
	dir := t.TempDir()
	if err := writeAll(dir); err != nil {
		t.Fatalf("writeAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tenant.dot"), []byte("digraph tampered {}\n"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	drift, err := checkDrift(dir)
	if err != nil {
		t.Fatalf("checkDrift: %v", err)
	}
	found := false
	for _, d := range drift {
		if d == "tenant" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'tenant' in drift list, got %v", drift)
	}
}

// TestCheckDrift_NoDriftWhenCurrent — freshly written dir has zero drift.
func TestCheckDrift_NoDriftWhenCurrent(t *testing.T) {
	dir := t.TempDir()
	if err := writeAll(dir); err != nil {
		t.Fatalf("writeAll: %v", err)
	}
	drift, err := checkDrift(dir)
	if err != nil {
		t.Fatalf("checkDrift: %v", err)
	}
	if len(drift) != 0 {
		t.Errorf("expected zero drift on fresh write, got %v", drift)
	}
}

// TestCommittedDotsAreCurrent — the REAL committed docs/state-machines/*.dot
// match Visualize() output (this is the gate that CI runs).
func TestCommittedDotsAreCurrent(t *testing.T) {
	committed := "../../../../docs/state-machines"
	if _, err := os.Stat(committed); os.IsNotExist(err) {
		t.Skipf("committed dot dir not found at %s (run from pkg module root)", committed)
	}
	drift, err := checkDrift(committed)
	if err != nil {
		t.Fatalf("checkDrift: %v", err)
	}
	if len(drift) != 0 {
		t.Errorf("committed DOT files out-of-date: %v — run `cd pkg && go generate ./statemachine/...`", drift)
	}
}

// TestCheckDrift_ReportsDriftInLexicographicOrder — the drift list is sorted,
// so CI output is stable run to run.
func TestCheckDrift_ReportsDriftInLexicographicOrder(t *testing.T) {
	dir := t.TempDir()
	if err := writeAll(dir); err != nil {
		t.Fatalf("writeAll: %v", err)
	}
	for _, name := range []string{"tenant", "build", "release", "domain"} {
		if err := os.Remove(filepath.Join(dir, name+".dot")); err != nil {
			t.Fatalf("remove: %v", err)
		}
	}

	drift, err := checkDrift(dir)

	if err != nil {
		t.Fatalf("checkDrift: %v", err)
	}
	want := []string{"build", "domain", "release", "tenant"}
	if strings.Join(drift, ",") != strings.Join(want, ",") {
		t.Fatalf("drift = %v, want %v", drift, want)
	}
}
