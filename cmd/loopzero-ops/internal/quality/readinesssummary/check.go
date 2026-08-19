// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package readinesssummary garante o invariante do ADR-0025 D3 nos SEIS pacotes de
// reconciler, e nao num so'.
//
// O invariante e' de ORDEM: o sumario de prontidao e' a ULTIMA mutacao de status
// antes de persistir, mesmo em retorno antecipado. Ordem e' o que uma tabela que
// conta chamadas nao ve' — foi assim que o B-105 ficou escondido.
//
// Por que este gate existe tendo uma guarda antiga: a guarda que vivia em
// `operators/internal/app/exposure_status_writer_test.go` e' de PACOTE e olha
// apenas a funcao que ENVOLVE cada `Patch`. A forma canonica dos cinco reconcilers
// e' outra — `defer reconcileloop.DeferReadinessSummary(...)()` no `Reconcile`, com
// os persists vivendo em helpers que ele chama. Para a guarda antiga isso e'
// ausencia: foi assim que o `preview` mediu "0 de 6" tendo cobertura (B-138).
//
// `DeferReadinessSummary` re-busca o CR e PERSISTE o sumario no retorno, ou seja, e'
// a ultima escrita do ciclo. Por isso a cobertura PROPAGA por chamada: um persist
// numa funcao alcancavel a partir de quem carrega o defer esta' coberto. A dobra em
// linha nao propaga — ela e' uma mutacao antes da escrita DAQUELA funcao.
//
// LIMITES MEDIDOS, e eles sao parte do resultado. O grafo e' intra-pacote e as
// funcoes sao indexadas por RECEPTOR+NOME, nao por nome: `operators/internal/app`
// tem DOIS `Reconcile` (Application e Exposure) e so' o Exposure carrega o defer —
// indexar por nome fazia o defer de um cobrir os helpers do outro, e o gate
// reportava verde sobre um persist descoberto. A resolucao de chamada e'
// conservadora: `r.foo()` no receptor resolve para `T.foo`, `foo()` resolve para a
// funcao livre, e QUALQUER outra forma fica sem resolver e nao propaga. Errar para
// o lado de nao-propagar produz falso positivo, que alguem le'; o contrario produz
// falso verde, que ninguem le'.
//
// A cobertura tambem e' INSENSIVEL A CAMINHO: se um helper e' alcancavel a partir de
// quem tem o defer, ele conta como coberto mesmo que outro caminho chegue nele sem
// o defer. Medido em 2026-08-19, isso e' aceitavel aqui e o motivo e' verificavel:
// os SETE defers da arvore estao todos no `Reconcile`, que e' o unico ponto de
// entrada que o controller-runtime chama. Se um defer aparecer fora de um
// `Reconcile`, esta premissa deixa de valer.
package readinesssummary

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PacotesPadrao sao os seis pacotes de reconciler que o B-138 declara como populacao.
var PacotesPadrao = []string{
	filepath.Join("operators", "internal", "app"),
	filepath.Join("operators", "internal", "environment"),
	filepath.Join("operators", "internal", "platform"),
	filepath.Join("operators", "internal", "preview"),
	filepath.Join("operators", "internal", "reconcileloop"),
	filepath.Join("operators", "internal", "tenant"),
}

// Config drives the gate.
type Config struct {
	RepoRoot string
	Pacotes  []string
}

// Sitio e' um persist de `.status` e a funcao que o contem.
type Sitio struct {
	Arquivo string
	Linha   int
	Funcao  string
	Forma   string
}

// Outcome carries the gate result.
type Outcome struct {
	Pass      bool
	Pacotes   int
	Sitios    int
	Violacoes []Sitio
}

type funcao struct {
	nome     string
	emLinha  bool
	comDefer bool
	chama    map[string]bool
	sitios   []Sitio
}

// Check runs the gate.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	pacotes := cfg.Pacotes
	if len(pacotes) == 0 {
		pacotes = PacotesPadrao
	}

	for _, rel := range pacotes {
		dir := filepath.Join(cfg.RepoRoot, rel)
		entradas, err := os.ReadDir(dir)
		if err != nil {
			// Pacote ausente e' FALHA, nunca skip: uma varredura que nao alcanca
			// nada reporta verde sobre nada.
			return out, fmt.Errorf("lendo %s: %w", rel, err)
		}
		out.Pacotes++

		funcs := map[string]*funcao{}
		for _, e := range entradas {
			nome := e.Name()
			if e.IsDir() || !strings.HasSuffix(nome, ".go") || strings.HasSuffix(nome, "_test.go") {
				continue
			}
			if err := varrer(cfg.RepoRoot, filepath.Join(dir, nome), funcs); err != nil {
				return out, err
			}
		}
		colher(funcs, &out)
	}

	sort.Slice(out.Violacoes, func(i, j int) bool {
		if out.Violacoes[i].Arquivo != out.Violacoes[j].Arquivo {
			return out.Violacoes[i].Arquivo < out.Violacoes[j].Arquivo
		}
		return out.Violacoes[i].Linha < out.Violacoes[j].Linha
	})
	out.Pass = len(out.Violacoes) == 0
	return out, nil
}

func varrer(root, caminho string, funcs map[string]*funcao) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, caminho, nil, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", caminho, err)
	}
	rel, _ := filepath.Rel(root, caminho)

	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		recv, ident := receptor(fd)
		chave := fd.Name.Name
		if recv != "" {
			chave = recv + "." + fd.Name.Name
		}
		fn := funcs[chave]
		if fn == nil {
			fn = &funcao{nome: chave, chama: map[string]bool{}}
			funcs[chave] = fn
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.DeferStmt:
				if nomeChamado(v.Call) == "DeferReadinessSummary" || chamaDentro(v.Call, "DeferReadinessSummary") {
					fn.comDefer = true
				}
			case *ast.CallExpr:
				alvo := nomeChamado(v)
				if k := resolver(v, recv, ident); k != "" {
					fn.chama[k] = true
				}
				if alvo == "SetReadinessSummaryViaPkg" || alvo == "SetReadinessSummary" {
					fn.emLinha = true
				}
				if forma := formaDePersist(v); forma != "" {
					fn.sitios = append(fn.sitios, Sitio{
						Arquivo: rel, Linha: fset.Position(v.Pos()).Line,
						Funcao: chave, Forma: forma,
					})
				}
			}
			return true
		})
	}
	return nil
}

// colher propaga a cobertura do `defer` por chamada e recolhe o que sobrou.
func colher(funcs map[string]*funcao, out *Outcome) {
	coberto := map[string]bool{}
	var fila []string
	for nome, fn := range funcs {
		if fn.comDefer {
			coberto[nome] = true
			fila = append(fila, nome)
		}
	}
	for len(fila) > 0 {
		atual := fila[0]
		fila = fila[1:]
		fn := funcs[atual]
		if fn == nil {
			continue
		}
		for alvo := range fn.chama {
			if !coberto[alvo] && funcs[alvo] != nil {
				coberto[alvo] = true
				fila = append(fila, alvo)
			}
		}
	}
	for nome, fn := range funcs {
		out.Sitios += len(fn.sitios)
		if fn.emLinha || coberto[nome] {
			continue
		}
		out.Violacoes = append(out.Violacoes, fn.sitios...)
	}
}

// formaDePersist devolve "Patch"/"Update" quando a chamada e' `...Status().X(...)`.
func formaDePersist(c *ast.CallExpr) string {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if sel.Sel.Name != "Patch" && sel.Sel.Name != "Update" {
		return ""
	}
	if !chamaDentroExpr(sel.X, "Status") {
		return ""
	}
	return sel.Sel.Name
}

// receptor devolve o nome do TIPO do receptor e o identificador usado para ele.
func receptor(fd *ast.FuncDecl) (tipo, ident string) {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return "", ""
	}
	campo := fd.Recv.List[0]
	if len(campo.Names) > 0 {
		ident = campo.Names[0].Name
	}
	e := campo.Type
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		tipo = id.Name
	}
	return tipo, ident
}

// resolver devolve a chave da funcao chamada, ou "" quando nao da' para resolver.
// Conservador de proposito: nao-resolver nao propaga cobertura.
func resolver(c *ast.CallExpr, recvTipo, recvIdent string) string {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok && recvIdent != "" && id.Name == recvIdent {
			return recvTipo + "." + f.Sel.Name
		}
	}
	return ""
}

func nomeChamado(c *ast.CallExpr) string {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.CallExpr: // `Helper(...)()`
		return nomeChamado(f)
	}
	return ""
}

func chamaDentro(c *ast.CallExpr, nome string) bool { return chamaDentroExpr(c, nome) }

func chamaDentroExpr(e ast.Expr, nome string) bool {
	var achou bool
	ast.Inspect(e, func(n ast.Node) bool {
		if ce, ok := n.(*ast.CallExpr); ok && nomeChamado(ce) == nome {
			achou = true
			return false
		}
		return true
	})
	return achou
}

// Summary describes the outcome.
func (o Outcome) Summary() string {
	return fmt.Sprintf("%d pacote(s), %d persist(s) de .status, %d sem dobra do sumario",
		o.Pacotes, o.Sitios, len(o.Violacoes))
}
