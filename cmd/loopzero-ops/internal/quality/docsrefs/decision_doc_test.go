// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package docsrefs

import (
	"os"
	"path/filepath"
	"testing"
)

func escreverArquivo(t *testing.T, raiz, rel, corpo string) {
	t.Helper()
	p := filepath.Join(raiz, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(corpo), 0o644); err != nil {
		t.Fatal(err)
	}
}

// O defeito do B-086: um documento de decisao que cita `ADR-0024` PARA DIZER QUE
// ELE NAO EXISTE fazia o id resolver. E' textualmente o que o comentario de
// `cycleArtifactDirs` proibe — escrever o documento que registra uma ausencia nao
// pode fabricar uma presenca.
func TestDocumentoDeDecisaoNaoResolveIdQueApenasMenciona(t *testing.T) {
	raiz := t.TempDir()
	escreverArquivo(t, raiz, "pkg/a/a.go", "package a\n\n// per ADR-0024\nvar X = 1\n")
	escreverArquivo(t, raiz, "docs/wiki/decisions/2026-08-12-contrato.md",
		"# Duas regras mandam o oposto\n\n**Data:** 2026-08-12\n\n"+
			"O `ADR-0024` e' citado por 8 pontos de codigo e NAO EXISTE.\n")

	// O gate RETORNA ERRO quando uma referencia normativa nao resolve — o erro e'
	// o resultado esperado aqui, e nao uma falha do teste.
	out, err := Check(Config{RepoRoot: raiz})
	if err == nil || len(out.Unresolved) == 0 {
		t.Fatal("ADR-0024 resolveu por ser mencionado no corpo de um documento de decisao — a ausencia fabricou a presenca")
	}
	if out.Unresolved[0].Raw != "ADR-0024" {
		t.Errorf("nao resolveu o id errado: %+v", out.Unresolved)
	}
}

// A contrapartida que a DoD exige: o criterio nao pode ser "documento de decisao
// nunca resolve". Um que DECLARE o id no proprio cabecalho e' a definicao dele, e
// resolve normalmente.
func TestDocumentoDeDecisaoResolveOIdQueDeclaraNoCabecalho(t *testing.T) {
	raiz := t.TempDir()
	escreverArquivo(t, raiz, "pkg/a/a.go", "package a\n\n// per ADR-0024\nvar X = 1\n")
	escreverArquivo(t, raiz, "docs/wiki/decisions/adr-0024.md",
		"# ADR-0024: quem escreve o status\n\n**Status:** ACCEPTED\n")

	out, err := Check(Config{RepoRoot: raiz})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Unresolved) != 0 {
		t.Errorf("o ADR que se DECLARA no cabecalho deixou de resolver: %+v", out.Unresolved)
	}
}

// Documentacao comum — runbook, doc de arquitetura — segue resolvendo por mencao.
// Sem esta assercao a mudanca viraria uma exigencia de cabecalho para toda a
// arvore, e ai' os ~30 ids citados por runbooks passariam a reprovar.
func TestDocumentacaoComumSegueResolvendoPorMencao(t *testing.T) {
	raiz := t.TempDir()
	escreverArquivo(t, raiz, "pkg/a/a.go", "package a\n\n// per ADR-0021\nvar X = 1\n")
	escreverArquivo(t, raiz, "docs/operations/cell-drain-runbook.md",
		"# Cell drain\n\nSem rebalanceamento automatico (ADR-0008 / ADR-0021).\n")

	out, err := Check(Config{RepoRoot: raiz})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Unresolved) != 0 {
		t.Errorf("um runbook deixou de resolver por mencao: %+v", out.Unresolved)
	}
}
