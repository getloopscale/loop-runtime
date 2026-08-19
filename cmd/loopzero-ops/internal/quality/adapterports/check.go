// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package adapterports dá ao pacote `adapters` um critério de admissão que consegue recusar.
//
// # O defeito, medido
//
// B-058, 2026-08-18. O pacote `api/internal/adapters` admitia membro por uma regra que não
// nega nada: *"é um adapter"*. Medido: dos **nove** adaptadores, cinco nomeavam a porta que
// satisfazem em **prosa**, quatro não diziam nada, e apenas **um** a afirmava em tempo de
// compilação.
//
// Prosa não compila. Um comentário dizendo *"implements routes.X"* continua ali depois de a
// interface mudar de forma, de nome ou de lugar — e o próximo leitor confia nele.
//
// # O critério
//
// Todo arquivo com tipo exportado neste pacote declara, com `var _ Porta = (*Tipo)(nil)`,
// qual porta satisfaz. Quem não souber nomear a porta não pertence aqui — e essa é
// exatamente a frase que o critério antigo não conseguia dizer.
//
// A distinção que importa: `var _ = pkg.Tipo{}` é asserção de **uso**, não de porta. Ela não
// afirma que o adaptador satisfaz interface alguma, e confundir as duas deixaria passar o
// defeito que esta guarda existe para pegar.
//
// # O limite
//
// Ele exige que a porta esteja DECLARADA, não que seja a porta certa. Um adaptador que
// asserta a interface errada compila e passa aqui — mas o consumidor não compilaria, então o
// compilador cobre essa metade.
package adapterports

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoPortDeclared means an adapter file declares an exported type without naming its port.
var ErrNoPortDeclared = errors.New("adaptador nao declara a porta que satisfaz")

// ErrNothingScanned means the package is absent — silence, not cleanliness.
//
// A outra alternativa do DoD de B-058 e DISSOLVER este pacote. Se isso acontecer, esta
// guarda tem de reprovar em vez de ficar verde sobre um diretorio que sumiu.
var ErrNothingScanned = errors.New("pacote adapters ausente — nao achar nada nao e o mesmo que nao achar nada errado")

const pacote = "api/internal/adapters"

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Outcome reports what was established.
type Outcome struct {
	Scanned int
	Missing []string
}

// declaraPorta reporta se o arquivo tem `var _ <algo> = ...` com TIPO declarado.
//
// `var _ = expr` (sem tipo) e assercao de uso e nao conta.
func declaraPorta(arq *ast.File) bool {
	for _, d := range arq.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.VAR {
			continue
		}
		for _, spec := range g.Specs {
			v, ok := spec.(*ast.ValueSpec)
			if !ok || v.Type == nil {
				continue // sem tipo declarado: assercao de uso
			}
			for _, n := range v.Names {
				if n.Name == "_" {
					return true
				}
			}
		}
	}
	return false
}

// temTipoExportado reporta se o arquivo declara algum tipo exportado.
func temTipoExportado(arq *ast.File) bool {
	for _, d := range arq.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}
		for _, spec := range g.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.IsExported() {
				return true
			}
		}
	}
	return false
}

// Check verifies every adapter declares the port it satisfies.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	dir := filepath.Join(root, filepath.FromSlash(pacote))

	entradas, err := os.ReadDir(dir)
	if err != nil {
		return out, fmt.Errorf("%w: %s", ErrNothingScanned, pacote)
	}

	fset := token.NewFileSet()
	for _, e := range entradas {
		nome := e.Name()
		if e.IsDir() || !strings.HasSuffix(nome, ".go") || strings.HasSuffix(nome, "_test.go") {
			continue
		}
		arq, err := parser.ParseFile(fset, filepath.Join(dir, nome), nil, 0)
		if err != nil {
			return out, fmt.Errorf("analisar %s: %w", nome, err)
		}
		if !temTipoExportado(arq) {
			continue // nao e adaptador; nao ha porta a exigir
		}
		out.Scanned++
		if !declaraPorta(arq) {
			out.Missing = append(out.Missing, nome)
		}
	}

	if out.Scanned == 0 && len(out.Missing) == 0 {
		// Nenhum arquivo com tipo exportado: ou o pacote foi dissolvido, ou a varredura
		// nao alcancou nada. Os dois merecem os olhos de alguem.
		return out, fmt.Errorf("%w: %s nao tem adaptador algum", ErrNothingScanned, pacote)
	}
	if len(out.Missing) > 0 {
		return out, fmt.Errorf("%w: %s — declare `var _ <Porta> = (*<Tipo>)(nil)`",
			ErrNoPortDeclared, strings.Join(out.Missing, ", "))
	}
	return out, nil
}
