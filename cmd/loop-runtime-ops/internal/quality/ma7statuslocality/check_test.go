// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package ma7statuslocality

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGo(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

const cleanFile = `package svc

func Do() { _ = 1 }
`

// A store adapter legitimately calls the machinery — must NOT be flagged.
const storeAdapterFile = `package store

func (s *X) UpdateStatus() {
	_ = s.machine.SafeTransition("a", "b")
	_, _ = s.q.UpdateReleaseStatus(nil, nil)
}
`

// An outer/root-db caller invoking SafeTransition — Gate C breach.
const safeTransitionOutside = `package db

func Bad() {
	_ = releaseMachine.SafeTransition("PENDING", "SyncCompleted")
}
`

// An outer caller invoking a generated status func — Gate B breach.
const generatedStatusOutside = `package services

func Bad(q Q) {
	_, _ = q.UpdateBuildStatusToRunning(nil, nil)
}
`

// A comment mentioning SafeTransition is NOT a call — must be ignored.
const commentOnly = `package db

// UpdateStatusIfCurrent computes the target via buildMachine.SafeTransition(from, event).
func Fine() {}
`

// A read-cast statemachine.State(...) is a conversion, not SafeTransition.
const readCastDecoy = `package db

func Project() {
	_ = statemachineState("RAW")
}

func statemachineState(s string) string { return s }
`

func TestCheck_CleanPasses(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/svc.go", cleanFile)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("clean must PASS, got %+v", out.Violations)
	}
}

func TestCheck_StoreAdapterAllowed(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/db/store/release_wrapper.go", storeAdapterFile)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("db/store is the sanctioned home; must NOT flag it, got %+v", out.Violations)
	}
}

func TestCheck_SafeTransitionOutsideStore_GateC(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/db/release_wrapper.go", safeTransitionOutside)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("SafeTransition outside db/store must FAIL (Gate C)")
	}
	if out.Violations[0].Gate != "C" {
		t.Errorf("want Gate C, got %q", out.Violations[0].Gate)
	}
}

func TestCheck_GeneratedStatusOutsideStore_GateB(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/build_svc.go", generatedStatusOutside)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("a generated status func called outside db/store must FAIL (Gate B)")
	}
	if out.Violations[0].Gate != "B" {
		t.Errorf("want Gate B, got %q", out.Violations[0].Gate)
	}
}

func TestCheck_CommentIgnored(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/db/build_types.go", commentOnly)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("a comment mentioning SafeTransition is not a call; must PASS, got %+v", out.Violations)
	}
}

func TestCheck_ReadCastDecoyIgnored(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/db/project_wrapper.go", readCastDecoy)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("a read-cast conversion is not SafeTransition; must PASS, got %+v", out.Violations)
	}
}

func TestCheck_GeneratedDirAllowed(t *testing.T) {
	root := t.TempDir()
	// the generated pkg DEFINES the funcs (method decls); calls to them within
	// generated are not breaches.
	writeGo(t, root, "api/internal/db/generated/builds.sql.go", `package generated
func (q *Queries) UpdateBuildStatusToRunning() {}`)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("db/generated is exempt (definitions); must PASS, got %+v", out.Violations)
	}
}
