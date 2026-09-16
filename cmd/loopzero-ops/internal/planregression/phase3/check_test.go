// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package phase3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	taskfileRoot = filepath.Join(repoRoot, "Taskfile.yml")
	// Era `docs/operations/dev-loop.md` ate' `19b8c6eaa` ("docs: consolidate 1,523
	// files into six living documents"). O conteudo NAO se perdeu — `docs/operations.md:11`
	// carrega o DEV LOOP e `CLAUDE.md:47` ja aponta para la'. Medido 2026-09-16: o
	// caminho antigo sobrevivia apenas neste arquivo; README, CLAUDE.md, Taskfile.yml e
	// taskfiles/test.yml ja tinham migrado, e so' o teste ficou para tras — por isso
	// ele e' repontado e nao aposentado.
	devLoopDoc   = filepath.Join(repoRoot, "docs/operations.md")
	readmePath   = filepath.Join(repoRoot, "README.md")
	claudeMdPath = filepath.Join(repoRoot, "CLAUDE.md")
)

// TestT3_1_DevLoopDocExists — operator runbook present
func TestT3_1_DevLoopDocExists(t *testing.T) {
	data, err := os.ReadFile(devLoopDoc)
	if err != nil {
		t.Fatalf("docs/operations.md missing: %v", err)
	}
	content := string(data)
	for _, must := range []string{
		// "LoopZero Dev Loop" era o TITULO do documento dedicado. Consolidado sob
		// `# Operations`, a secao chama-se `## Dev loop` — repetir "LoopZero" num
		// documento deste repositorio seria ruido, e o que o teste protege e' que
		// a secao exista, nao que carregue o nome do arquivo que a continha.
		"## Dev loop",
		"`task setup`",
		"`tilt up`",
		"`task doctor`",
		"Troubleshooting",
	} {
		if !strings.Contains(content, must) {
			t.Errorf("docs/operations.md should contain %q", must)
		}
	}
}

// TestT3_2_SetupHasDeprecationNotice — Taskfile setup target prints notice
func TestT3_2_SetupHasDeprecationNotice(t *testing.T) {
	data, err := os.ReadFile(taskfileRoot)
	if err != nil {
		t.Fatalf("read Taskfile: %v", err)
	}
	content := string(data)
	// Find setup: target block (between `setup:` and next root-level task)
	startIdx := strings.Index(content, "  setup:")
	if startIdx == -1 {
		t.Fatal("setup: target not found in Taskfile.yml")
	}
	// Take 3000 chars block forward (sufficient for setup block)
	block := content[startIdx:]
	if len(block) > 3000 {
		block = block[:3000]
	}
	if !strings.Contains(block, "[NOTICE]") {
		t.Errorf("setup target should print a [NOTICE] deprecation message")
	}
	if !strings.Contains(block, "tilt up") {
		t.Errorf("setup target notice should reference 'tilt up' as the dev loop")
	}
	if !strings.Contains(block, "docs/operations.md") {
		t.Errorf("setup target notice should reference docs/operations.md")
	}
}

// TestT3_1_ReadmeCanonicalizesTilt — README Setup section explicitly distinguishes
// task setup vs tilt up roles.
func TestT3_1_ReadmeCanonicalizesTilt(t *testing.T) {
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	content := string(data)
	// Find "### Setup" block
	idx := strings.Index(content, "### Setup")
	if idx == -1 {
		t.Fatal("### Setup section not found in README")
	}
	block := content[idx:]
	if len(block) > 1000 {
		block = block[:1000]
	}
	if !strings.Contains(block, "DEV LOOP") && !strings.Contains(block, "dev loop") {
		t.Errorf("README Setup section should explain dev loop = tilt up")
	}
	if !strings.Contains(block, "docs/operations.md") {
		t.Errorf("README Setup section should link to docs/operations.md")
	}
}

// TestT3_1_ClaudeMdCanonicalizes — CLAUDE.md development-commands section
// references the distinction explicitly.
func TestT3_1_ClaudeMdCanonicalizes(t *testing.T) {
	data, err := os.ReadFile(claudeMdPath)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "docs/operations.md") {
		t.Errorf("CLAUDE.md should reference docs/operations.md")
	}
	if !strings.Contains(content, "ONE-SHOT") && !strings.Contains(content, "DEV LOOP") {
		t.Errorf("CLAUDE.md should distinguish task setup (one-shot) vs tilt up (dev loop)")
	}
}
