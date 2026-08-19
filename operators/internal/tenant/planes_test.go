// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// pacotesDeServico sao os pacotes de cliente externo do operator. Um campo do
// `TenantReconciler` cujo tipo venha de um deles E' um cliente de plano.
var pacotesDeServico = map[string]bool{
	"argocd": true, "gitea": true, "minio": true, "miniop": true,
	"harbor": true, "vault": true,
}

// arquivosDoPacote lista os .go de producao do pacote.
func arquivosDoPacote(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("lendo o diretorio do pacote: %v", err)
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Um arquivo novo entra no pacote sem plano declarado, e a costura passa a
// cobrir menos do que aparenta. Declarar e' obrigatorio; declarar lista vazia
// (logica pura) e' uma resposta valida e verificada.
func TestCostura_TodoArquivoDeclaraSeusPlanos(t *testing.T) {
	arqs := arquivosDoPacote(t)
	if len(arqs) == 0 {
		t.Fatal("zero arquivos varridos: uma costura sobre nada passa por vacuidade")
	}
	for _, a := range arqs {
		if _, ok := planosDoArquivo[a]; !ok {
			t.Errorf("%s nao declara plano em planosDoArquivo (planes.go)", a)
		}
	}
	for a := range planosDoArquivo {
		if _, err := os.Stat(a); err != nil {
			t.Errorf("planosDoArquivo declara %s, que nao existe — declaracao que sobreviveu ao arquivo", a)
		}
	}
}

// Um cliente novo na struct sem entrada em `planoDoCliente` seria invisivel
// para a verificacao seguinte: seus usos nao pertenceriam a plano nenhum e
// passariam por omissao.
func TestCostura_TodoClienteDaStructTemPlano(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "tenant_controller.go", nil, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var campos []string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "TenantReconciler" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, campo := range st.Fields.List {
			sel, ok := campo.Type.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || !pacotesDeServico[pkg.Name] {
				continue
			}
			for _, nome := range campo.Names {
				campos = append(campos, nome.Name)
			}
		}
		return false
	})
	if len(campos) == 0 {
		t.Fatal("nenhum cliente encontrado na struct: a varredura nao alcancou nada")
	}
	for _, c := range campos {
		if _, ok := planoDoCliente[c]; !ok {
			t.Errorf("o campo %s e' cliente de servico externo e nao tem plano em planoDoCliente (planes.go)", c)
		}
	}
	for c := range planoDoCliente {
		var achou bool
		for _, v := range campos {
			if v == c {
				achou = true
			}
		}
		if !achou {
			t.Errorf("planoDoCliente declara %s, que nao e' mais campo da struct", c)
		}
	}
}

var usoDeCliente = regexp.MustCompile(`\br\.([A-Z][A-Za-z]*)\b`)

// A verificacao que da' sentido as duas anteriores: o plano declarado precisa
// cobrir os clientes que o arquivo realmente chama. E' isto que impede a
// declaracao de envelhecer — mover uma chamada de plano sem mover a declaracao
// reprova aqui.
func TestCostura_UsosRespeitamOPlanoDeclarado(t *testing.T) {
	var usos int
	for _, a := range arquivosDoPacote(t) {
		src, err := os.ReadFile(a)
		if err != nil {
			t.Fatalf("lendo %s: %v", a, err)
		}
		declarados := planosDoArquivo[a]
		for _, m := range usoDeCliente.FindAllStringSubmatch(string(src), -1) {
			p, ehCliente := planoDoCliente[m[1]]
			if !ehCliente {
				continue
			}
			usos++
			if !serve(declarados, p) {
				t.Errorf("%s usa r.%s (plano %s) mas declara %v", a, m[1], p, declarados)
			}
		}
	}
	// O piso: um regex que deixasse de casar tornaria este teste verde sobre
	// nada. Medido em 2026-08-18: 8 usos de cliente na arvore real.
	if usos < 8 {
		t.Fatalf("apenas %d usos de cliente varridos; a arvore real tinha 8 em 2026-08-18 — "+
			"a varredura encolheu e o verde deixou de significar alguma coisa", usos)
	}
}
