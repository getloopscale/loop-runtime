// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `loopzero-ops quality --list` e' o ORACULO do inventario de gates — o `CLAUDE.md`
// o designa assim, e o `quality_gate_wiring_test.go` exige que todo gate
// REGISTRADO esteja ligado ou isento. Nada, porem, alcancava um pacote que vive
// em `internal/quality/` e nunca foi registrado: ele nao aparece no oraculo, nao
// e' cobrado pelo wiring, e mesmo assim veste o nome do diretorio.
//
// Medido em 2026-08-18 (B-066): 7 dos 148 diretorios nao eram importados pelo
// registry. Quatro sao AUXILIARES legitimos — `baseline`, `coverageparse`,
// `qgshell` e `qtestutil`, importados por 2, 2, 5 e 15 irmaos. Tres nao eram nada:
// `opencostgate`, `phase2gate` e `phase3gate`, com ZERO importadores e zero
// registro, verificando fases de um plano ja' concluido.
//
// O sinal que separa os dois casos e' esse mesmo: auxiliar e' importado por irmao;
// pacote sem registro E sem importador nao pertence a este diretorio.
func TestTodoPacoteDeQualityEGateOuAuxiliar(t *testing.T) {
	const raizQuality = "internal/quality"
	ents, err := os.ReadDir(raizQuality)
	if err != nil {
		t.Fatalf("lendo %s: %v", raizQuality, err)
	}

	// Fontes do proprio cmd (onde o registry vive) — para saber quem e' registrado.
	registrado := map[string]bool{}
	cmdFiles, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var textoDoCmd strings.Builder
	for _, f := range cmdFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("lendo %s: %v", f, err)
		}
		textoDoCmd.Write(b)
	}

	var dirs []string
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		t.Fatal("zero diretorios varridos: uma verificacao sobre nada passa por vacuidade")
	}

	cmdSrc := textoDoCmd.String()
	for _, d := range dirs {
		imp := "quality/" + d + "\""
		registrado[d] = strings.Contains(cmdSrc, imp)
		if registrado[d] {
			continue
		}
		// auxiliar: importado por ALGUM irmao que nao ele proprio
		var auxiliar bool
		for _, outro := range dirs {
			if outro == d {
				continue
			}
			arqs, _ := filepath.Glob(filepath.Join(raizQuality, outro, "*.go"))
			for _, a := range arqs {
				b, err := os.ReadFile(a)
				if err != nil {
					continue
				}
				if strings.Contains(string(b), imp) {
					auxiliar = true
					break
				}
			}
			if auxiliar {
				break
			}
		}
		if !auxiliar {
			t.Errorf("internal/quality/%s nao e' gate registrado nem auxiliar importado por irmao — "+
				"veste o nome do diretorio sem pertencer a ele, e o oraculo `--list` nao consegue enumera-lo", d)
		}
	}
}
