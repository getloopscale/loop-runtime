// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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

// layout builds <tmp>/{engine,loopruntime-contracts,loopruntime-cloud} and returns the engine
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
// (loopruntime-contracts) that imports an engine package MUST fail the gate. Without
// this proving FAIL, the gate would be vacuous.
func TestAcyclicGraph_SyntheticUpwardImport_Fails(t *testing.T) {
	engine := layout(t)
	parent := filepath.Dir(engine)
	// loopruntime-contracts illegally imports an engine-internal package.
	writeGo(t, filepath.Join(parent, "loopruntime-contracts"), "jwt/validator.go",
		"package jwt\nimport _ \"github.com/getloopscale/loop-runtime/pkg/logging\"\n")

	out, err := Check(Config{RepoRoot: engine})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("gate MUST fail when loopruntime-contracts imports an engine package")
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
		"package internal\nimport _ \"github.com/getloopscale/loopruntime-cloud/internal/billing\"\n")

	out, err := Check(Config{RepoRoot: engine})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("gate MUST fail when the engine imports loopruntime-cloud")
	}
}

// TestAcyclicGraph_CloudImportsEngineInternal_Fails — loopruntime-cloud importing
// engine pkg/* is the exact coupling M4 severed; it MUST fail.
func TestAcyclicGraph_CloudImportsEngineInternal_Fails(t *testing.T) {
	engine := layout(t)
	parent := filepath.Dir(engine)
	writeGo(t, filepath.Join(parent, "loopruntime-cloud"), "internal/auth/x.go",
		"package auth\nimport _ \"github.com/getloopscale/loop-runtime/pkg/enforcement\"\n")

	out, err := Check(Config{RepoRoot: engine})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("gate MUST fail when loopruntime-cloud imports engine pkg/*")
	}
}

// TestAcyclicGraph_CleanLayout_Passes — the canonical directions (contracts
// imports only external; engine + cloud import loopruntime-contracts) are all allowed.
func TestAcyclicGraph_CleanLayout_Passes(t *testing.T) {
	engine := layout(t)
	parent := filepath.Dir(engine)
	// loopruntime-contracts imports only an external lib (allowed).
	writeGo(t, filepath.Join(parent, "loopruntime-contracts"), "jwt/validator.go",
		"package jwt\nimport _ \"github.com/golang-jwt/jwt/v5\"\n")
	// engine imports loopruntime-contracts (downward — allowed).
	writeGo(t, engine, "api/internal/x.go",
		"package internal\nimport _ \"github.com/getloopscale/contracts/jwt\"\n")
	// loopruntime-cloud imports loopruntime-contracts (downward — allowed).
	writeGo(t, filepath.Join(parent, "loopruntime-cloud"), "internal/auth/x.go",
		"package auth\nimport _ \"github.com/getloopscale/contracts/serviceauth\"\n")

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
		"package pkg\nimport _ \"github.com/getloopscale/contracts/plan\"\n")

	out, err := Check(Config{RepoRoot: engine})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("engine-only layout must pass; violations: %v", out.Violations)
	}
	if len(out.Skipped) != 3 {
		t.Errorf("expected loopruntime-contracts + loopruntime-cloud + operators-api skipped, got %v", out.Skipped)
	}
}

// TestAcyclicGraph_OperatorsApiImportsInternal_Fails — the M6 contract module
// (operators/api) is a clean base; if it imports an engine-internal package the
// gate MUST fail. Without this proving FAIL, the operators-api node is vacuous.
func TestAcyclicGraph_OperatorsApiImportsInternal_Fails(t *testing.T) {
	engine := layout(t)
	// operators/api illegally imports an engine-internal package.
	writeGo(t, filepath.Join(engine, "operators", "api"), "v1/leak.go",
		"package v1\nimport _ \"github.com/getloopscale/loop-runtime/operators/internal/conditions\"\n")

	out, err := Check(Config{RepoRoot: engine})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if out.Pass {
		t.Fatal("gate MUST fail when operators/api imports operators/internal")
	}
}

// TestAcyclicGraph_OperatorsApiCleanBase_Passes — operators/api importing only
// k8s is allowed; the gate checks it as a node.
func TestAcyclicGraph_OperatorsApiCleanBase_Passes(t *testing.T) {
	engine := layout(t)
	writeGo(t, filepath.Join(engine, "operators", "api"), "v1/types.go",
		"package v1\nimport _ \"k8s.io/apimachinery/pkg/apis/meta/v1\"\n")

	out, err := Check(Config{RepoRoot: engine})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("clean operators/api must pass; violations: %v", out.Violations)
	}
	found := false
	for _, c := range out.Checked {
		if c == "operators-api" {
			found = true
		}
	}
	if !found {
		t.Errorf("operators-api should be in Checked, got %v", out.Checked)
	}
}
