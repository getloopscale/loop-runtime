// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package acyclicgraph

import (
	"os"
	"path/filepath"
	"testing"
)

// writeGo creates <root>/<rel> with the given Go source.
func writeGo(t *testing.T, root, rel, src string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// layout builds <tmp>/{engine,loopzero-contracts,loopzero-cloud} and returns the engine
// root (the Config.RepoRoot the gate is given).
func layout(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	engine := filepath.Join(tmp, "engine")
	if err := os.MkdirAll(engine, 0o755); err != nil {
		t.Fatalf("mkdir engine: %v", err)
	}
	return engine
}

// TestAcyclicGraph_SyntheticUpwardImport_Fails — the keystone test: a base
// (loopzero-contracts) that imports an engine package MUST fail the gate. Without
// this proving FAIL, the gate would be vacuous.
func TestAcyclicGraph_SyntheticUpwardImport_Fails(t *testing.T) {
	engine := layout(t)
	parent := filepath.Dir(engine)
	// loopzero-contracts illegally imports an engine-internal package.
	writeGo(t, filepath.Join(parent, "loopzero-contracts"), "jwt/validator.go",
		"package jwt\nimport _ \"github.com/loopzero/loopzero/pkg/logging\"\n")

	out, err := Check(Config{RepoRoot: engine})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("gate MUST fail when loopzero-contracts imports an engine package")
	}
	if len(out.Violations) == 0 {
		t.Fatal("expected at least one violation")
	}
}

// TestAcyclicGraph_EngineImportsCloud_Fails — the engine importing the cloud
// (cloud is a leaf consumer) is an upward edge and MUST fail.
func TestAcyclicGraph_EngineImportsCloud_Fails(t *testing.T) {
	engine := layout(t)
	writeGo(t, engine, "api/internal/x.go",
		"package internal\nimport _ \"github.com/getloopscale/loopzero-cloud/internal/billing\"\n")

	out, err := Check(Config{RepoRoot: engine})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("gate MUST fail when the engine imports loopzero-cloud")
	}
}

// TestAcyclicGraph_CloudImportsEngineInternal_Fails — loopzero-cloud importing
// engine pkg/* is the exact coupling M4 severed; it MUST fail.
func TestAcyclicGraph_CloudImportsEngineInternal_Fails(t *testing.T) {
	engine := layout(t)
	parent := filepath.Dir(engine)
	writeGo(t, filepath.Join(parent, "loopzero-cloud"), "internal/auth/x.go",
		"package auth\nimport _ \"github.com/loopzero/loopzero/pkg/enforcement\"\n")

	out, err := Check(Config{RepoRoot: engine})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("gate MUST fail when loopzero-cloud imports engine pkg/*")
	}
}

// TestAcyclicGraph_CleanLayout_Passes — the canonical directions (contracts
// imports only external; engine + cloud import loopzero-contracts) are all allowed.
func TestAcyclicGraph_CleanLayout_Passes(t *testing.T) {
	engine := layout(t)
	parent := filepath.Dir(engine)
	// loopzero-contracts imports only an external lib (allowed).
	writeGo(t, filepath.Join(parent, "loopzero-contracts"), "jwt/validator.go",
		"package jwt\nimport _ \"github.com/golang-jwt/jwt/v5\"\n")
	// engine imports loopzero-contracts (downward — allowed).
	writeGo(t, engine, "api/internal/x.go",
		"package internal\nimport _ \"github.com/getloopscale/loopzero-contracts/jwt\"\n")
	// loopzero-cloud imports loopzero-contracts (downward — allowed).
	writeGo(t, filepath.Join(parent, "loopzero-cloud"), "internal/auth/x.go",
		"package auth\nimport _ \"github.com/getloopscale/loopzero-contracts/serviceauth\"\n")

	out, err := Check(Config{RepoRoot: engine})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("clean layout must pass; violations: %v", out.Violations)
	}
	if len(out.Checked) != 3 {
		t.Errorf("expected all 3 nodes checked, got %v", out.Checked)
	}
}

// TestAcyclicGraph_AbsentSiblings_Skip — when run in isolation (no sibling
// repos) the gate degrades gracefully: engine checked, siblings skipped, PASS.
func TestAcyclicGraph_AbsentSiblings_Skip(t *testing.T) {
	engine := layout(t)
	writeGo(t, engine, "pkg/x.go",
		"package pkg\nimport _ \"github.com/getloopscale/loopzero-contracts/plan\"\n")

	out, err := Check(Config{RepoRoot: engine})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("engine-only layout must pass; violations: %v", out.Violations)
	}
	if len(out.Skipped) != 2 {
		t.Errorf("expected loopzero-contracts + loopzero-cloud skipped, got %v", out.Skipped)
	}
}
