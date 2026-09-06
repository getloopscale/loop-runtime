// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package stdlibonly refuses a dependency-free package that stopped being dependency-free.
//
// # O defeito que ele previne, medido
//
// B-057: `api/internal/domain` é importado por **87 arquivos** (47 de produção, 40 de teste)
// — re-medido em 2026-09-05; eram 90 quando esta guarda foi escrita em 2026-08-18, e o número
// se move sozinho conforme a árvore anda. A razão de poder ser importado por tantos é
// justamente não arrastar dependência nenhuma — medido com `go list`, são 9 imports, todos da
// biblioteca padrão.
//
// Um único import externo ali se propaga para os 87, e nada impedia. A propriedade existia
// por disciplina, não por contrato; o `CLAUDE.md` a descreve como razão do pacote existir, e
// descrever não é verificar.
//
// # Por que a árvore ausente reprova
//
// O item que motivou esta guarda pede, entre outras coisas, RENOMEAR este pacote. Um rename
// que mova o diretório deixaria a guarda verde para sempre sobre nada — a forma `PASS — 0`
// que este ecossistema já pagou para aprender. Ausência reprova, e quem renomear atualiza o
// caminho conscientemente.
//
// # O limite
//
// Ele lê os imports declarados do pacote nomeado, não a árvore transitiva. Um import stdlib
// que passe a fazer I/O de rede continua stdlib e passa aqui.
package stdlibonly

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrExternalImport means the package imports something outside the standard library.
var ErrExternalImport = errors.New("pacote sem dependência importou algo fora da biblioteca padrão")

// ErrNothingScanned means the guarded package is absent — silence, not cleanliness.
var ErrNothingScanned = errors.New("pacote guardado ausente — não achar nada não é o mesmo que não achar nada errado")

// guardado é o pacote cuja propriedade esta guarda protege.
//
// É um caminho só de propósito: a propriedade foi MEDIDA aqui, e estender a guarda a
// pacotes cuja propriedade ninguém mediu afirmaria o que não se sabe.
const guardado = "api/internal/ports"

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Outcome reports what was established.
type Outcome struct {
	Scanned  int
	External []string
}

// Check verifies that the guarded package imports only the standard library.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	dir := filepath.Join(root, filepath.FromSlash(guardado))

	entradas, err := os.ReadDir(dir)
	if err != nil {
		return out, fmt.Errorf("%w: %s", ErrNothingScanned, guardado)
	}

	fset := token.NewFileSet()
	visto := map[string]bool{}
	for _, e := range entradas {
		nome := e.Name()
		if e.IsDir() || !strings.HasSuffix(nome, ".go") || strings.HasSuffix(nome, "_test.go") {
			continue
		}
		arq, err := parser.ParseFile(fset, filepath.Join(dir, nome), nil, parser.ImportsOnly)
		if err != nil {
			return out, fmt.Errorf("analisar %s: %w", nome, err)
		}
		for _, imp := range arq.Imports {
			caminho, err := strconv.Unquote(imp.Path.Value)
			if err != nil || visto[caminho] {
				continue
			}
			visto[caminho] = true
			out.Scanned++
			// Um import da biblioteca padrão nunca tem ponto no primeiro segmento: o ponto
			// é o que marca um host (`github.com/...`, `golang.org/x/...`).
			if strings.Contains(strings.SplitN(caminho, "/", 2)[0], ".") {
				out.External = append(out.External, caminho)
			}
		}
	}

	if out.Scanned == 0 {
		return out, fmt.Errorf("%w: %s não declara import nenhum", ErrNothingScanned, guardado)
	}
	if len(out.External) > 0 {
		return out, fmt.Errorf("%w: %s importa %s — e isso atravessa para todos os seus importadores",
			ErrExternalImport, guardado, strings.Join(out.External, ", "))
	}
	return out, nil
}
