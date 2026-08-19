// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package deploycontracts

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `isReservedEnvKey` roda dentro de `for k := range unit.Env`, e `unit.Env` vem
// de `AppSpec.Env`. Medido em 2026-08-13 e reconfirmado em 2026-08-19: NENHUM
// site de producao popula esse campo — o tipo de fronteira HTTP sequer o declara.
// O laco nunca itera, e a regra de sombreamento nao dispara (B-094).
//
// Isto nao e' vulnerabilidade: o caminho que ela guardaria tambem nao existe. E'
// APARENCIA DE PROTECAO, a mesma classe do B-045 e do B-055 — e tres artefatos ja'
// afirmaram, no presente, que a regra impede sombreamento.
//
// Este teste vigia o fato. No dia em que alguem popular o campo, a regra passa a
// valer de verdade, e essa e' uma mudanca que precisa ser REVISTA e nao
// descoberta depois: o conjunto de prefixos que ela bloqueia difere do que o
// validador do services.json bloqueia (B-093), e a severidade dos dois lados
// tambem difere (B-069).
func TestAppSpecEnvSegueSemPopuladorDeProducao(t *testing.T) {
	raiz := raizDoRepo(t)
	alvo := filepath.Join(raiz, "api")

	// A varredura e' por AST e olha CONSTRUCAO de `AppSpec` com o campo `Env`
	// nomeado — `AppSpec{Env: ...}` ou `deploycontracts.AppSpec{Env: ...}`.
	//
	// LIMITE DECLARADO: ela nao ve' atribuicao posterior a um campo
	// (`spec.Env = x`). A primeira versao tentava, com um regex sobre `.Env =`, e
	// casou `cmd.Env` de `exec.Command` no `gitclient` — um falso positivo que
	// teria feito esta guarda acusar populacao onde nao ha'. Entre uma guarda
	// estreita e honesta e uma larga e errada, a estreita.
	var populadores []string
	fset := token.NewFileSet()
	err := filepath.Walk(alvo, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			var nome string
			switch t := cl.Type.(type) {
			case *ast.Ident:
				nome = t.Name
			case *ast.SelectorExpr:
				nome = t.Sel.Name
			}
			if nome != "AppSpec" {
				return true
			}
			for _, e := range cl.Elts {
				kv, ok := e.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Env" {
					rel, _ := filepath.Rel(raiz, p)
					populadores = append(populadores, rel+":"+fmt.Sprint(fset.Position(kv.Pos()).Line))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("varrendo: %v", err)
	}

	if len(populadores) > 0 {
		t.Errorf("o campo Env passou a ser populado em producao: %v\n\n"+
			"A regra de env reservada em build_plan.go deixou de ser inalcancavel e passa a "+
			"REJEITAR deploys. Reveja o B-094 antes de seguir: o conjunto de prefixos que ela "+
			"bloqueia difere do validador do services.json (B-093), e a severidade dos dois "+
			"lados tambem difere (B-069).", populadores)
	}
}

func raizDoRepo(t *testing.T) string {
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
	t.Fatal("nao achei a raiz do repositorio")
	return ""
}
