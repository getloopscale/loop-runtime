// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package errorvocabulary garante que as mensagens de erro AUTORADAS nos
// handlers nao carreguem os termos internos que a Regra Inquebravel 3 proibe em
// saida ao usuario.
//
// Por que o escritor nao resolve isto: `httpio.WriteLoopZeroError` e' pass-through
// puro — ele nao inventa vocabulario nem o filtra, e esta' certo assim.
// Alimentado com uma mensagem suja, entrega os termos verbatim no corpo. A
// superficie de risco sao as ~277 mensagens escritas nos call sites (B-081), e
// entre os gates nao havia nenhum sobre o vocabulario delas.
//
// O gate NASCE VERDE: medido em 2026-08-19, nenhuma mensagem em `routes/` ou
// `services/` carrega termo banido. Ele nao conserta nada hoje — impede que a
// proxima mensagem quebre a regra sem que ninguem perceba.
package errorvocabulary

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// escritoresDeErro sao as funcoes cujo argumento chega ao CORPO da resposta.
// Um literal dentro da expressao de argumento de uma delas e' texto que o
// usuario le'. Fora delas, um literal com "ArgoCD" e' log ou nome de campo
// interno, e nao viola a Regra 3.
var escritoresDeErro = map[string]bool{
	"WriteLoopZeroError":                true,
	"WriteLoopZeroErrorWithFieldErrors": true,
	"WriteCodeErrorJSON":            true,
	"WriteErrorJSON":                true,
	"WriteJSONWithErrors":           true,
	"FieldError":                    true,
}

// termosBanidos e' a lista da Regra Inquebravel 3 (CLAUDE.md § The Rules).
// A comparacao e' case-insensitive: a regra lista `ArgoCD` e `argocd`
// separadamente, e o que ela protege e' o usuario nao ler o nome da ferramenta —
// a caixa nao muda isso.
var termosBanidos = []string{
	"argocd",
	"gitops",
	"crd",
	"tenant_id",
	"advisory lock",
	"pg_advisory",
	"gitea",
	"minio",
	"kubebuilder",
	"envtest",
	"kaniko",
}

// Config drives the gate.
type Config struct {
	RepoRoot string
	// Roots sao os diretorios varridos. Vazio usa o padrao medido no B-081.
	Roots []string
}

// Violacao e' uma mensagem suja, com o termo que a suja.
type Violacao struct {
	Arquivo  string
	Linha    int
	Termo    string
	Mensagem string
}

// Outcome carries the gate result.
type Outcome struct {
	Pass      bool
	Arquivos  int
	Mensagens int
	Violacoes []Violacao
}

// Check runs the gate.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	roots := cfg.Roots
	if len(roots) == 0 {
		roots = []string{
			filepath.Join("api", "internal", "routes"),
			filepath.Join("api", "internal", "services"),
		}
	}

	for _, r := range roots {
		dir := filepath.Join(cfg.RepoRoot, r)
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				// Uma raiz ausente e' FALHA, nunca skip: uma varredura que nao
				// alcanca nada reporta verde sobre nada.
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			out.Arquivos++
			return varrerArquivo(cfg.RepoRoot, p, &out)
		})
		if err != nil {
			return out, fmt.Errorf("varrendo %s: %w", r, err)
		}
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

func varrerArquivo(root, caminho string, out *Outcome) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, caminho, nil, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", caminho, err)
	}
	rel, _ := filepath.Rel(root, caminho)

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !escritoresDeErro[sel.Sel.Name] {
			return true
		}
		// Desce em TODA a expressao de argumento: a mensagem costuma vir de um
		// `.Wrap("...")` encadeado dentro da chamada, e nao como literal direto.
		for _, arg := range call.Args {
			ast.Inspect(arg, func(m ast.Node) bool {
				lit, ok := m.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				texto, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				out.Mensagens++
				baixo := strings.ToLower(texto)
				for _, t := range termosBanidos {
					if strings.Contains(baixo, t) {
						out.Violacoes = append(out.Violacoes, Violacao{
							Arquivo:  rel,
							Linha:    fset.Position(lit.Pos()).Line,
							Termo:    t,
							Mensagem: texto,
						})
						break
					}
				}
				return true
			})
		}
		return true
	})
	return nil
}

// Summary describes the outcome.
func (o Outcome) Summary() string {
	return fmt.Sprintf("%d arquivo(s) varrido(s), %d mensagem(ns) autorada(s) em escritor de erro, %d violacao(oes)",
		o.Arquivos, o.Mensagens, len(o.Violacoes))
}
