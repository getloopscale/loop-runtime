// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Um arquivo em `infra/quality/` e' lido como INSUMO DE GATE. Sem leitor, ele
// parece contrato e nao e' — e a diferenca so' aparece quando alguem o edita
// achando que muda alguma coisa.
//
// Medido em 2026-08-19 (B-091): dos 12 arquivos de dados do diretorio, onze
// tinham de 1 a 8 leitores e UM tinha ZERO. Era a segunda baseline orfa da mesma
// familia — o B-045 nomeou uma e elas eram duas — e alem de sem leitor estava
// obsoleta: tres das suas quatro entradas apontavam para scripts que ja' nao
// existem.
//
// O nome dele NAO aparece escrito aqui, e a omissao e' o ponto: esta assercao
// conta leitores procurando o nome do arquivo no codigo, entao cita-lo neste
// comentario faria ESTE teste virar o leitor que falta. Foi o que aconteceu na
// primeira versao — a prosa sobre a ausencia fabricou a presenca.
func TestTodoArquivoDeInfraQualityTemLeitor(t *testing.T) {
	raiz := repoRootDir(t)
	dir := filepath.Join(raiz, "infra", "quality")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("lendo %s: %v", dir, err)
	}

	var arquivos []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		// O README documenta o DIRETORIO; ele nao e' insumo de gate. A isencao
		// e' declarada aqui em vez de silenciosa porque o nome e' generico o
		// bastante para casar com dezenas de arquivos e produzir um "leitor"
		// que nao le' nada.
		if e.Name() == "README.md" {
			continue
		}
		arquivos = append(arquivos, e.Name())
	}
	if len(arquivos) == 0 {
		t.Fatal("zero arquivos varridos: uma assercao sobre nada passa por vacuidade")
	}

	for _, nome := range arquivos {
		out, err := exec.Command("grep", "-rl", "--include=*.go", "--include=*.yml",
			"--include=*.yaml", "--include=*.sh", nome, raiz).Output()
		if err != nil && len(out) == 0 {
			t.Errorf("%s nao tem leitor em codigo — um arquivo em infra/quality/ sem leitor "+
				"parece contrato e nao e'", nome)
			continue
		}
		var leitores int
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l == "" || strings.Contains(l, filepath.Join("infra", "quality")) {
				continue
			}
			// O proprio arquivo que conta nao vale como leitor: senao qualquer
			// nome citado por esta assercao passa a se auto-satisfazer.
			if strings.HasSuffix(l, "quality_baseline_readers_test.go") {
				continue
			}
			leitores++
		}
		if leitores == 0 {
			t.Errorf("%s nao tem leitor fora de infra/quality/ — parece contrato e nao e'", nome)
		}
	}
}

func repoRootDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "api", "go.mod")); err == nil {
			return dir
		}
		pai := filepath.Dir(dir)
		if pai == dir {
			break
		}
		dir = pai
	}
	t.Fatal("nao achei a raiz do repositorio (procurei api/go.mod)")
	return ""
}
