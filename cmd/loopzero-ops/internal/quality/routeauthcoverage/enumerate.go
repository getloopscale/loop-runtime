// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routeauthcoverage

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// # Por que a enumeração é AST, e por que o escopo deixou de ser um arquivo
//
// O enumerador anterior era um regex sobre linhas: `^\s*r\.(?:Get|Post|…)\(`. Ele
// exige que `r.` seja seguido IMEDIATAMENTE pelo verbo — e a forma mais numerosa do
// roteador real é `r.With(rateLimitMiddlewares...).Post(…)`, em que vem `With`.
//
// Medido 2026-08-17: 42 registros diretos, **28** na forma encadeada, 1 montado por
// helper noutro arquivo. O gate reportava `42 routes` sobre ~71 e vendia a garantia
// "toda rota pública está atrás de Auth" verificando 59% delas.
//
// Nenhuma das 29 invisíveis estava insegura — mas por POSIÇÃO, não por verificação:
// 27 caem dentro de `registerProtectedRoutes` e uma está no allowlist. A linha 202 do
// roteador (`r.With(middleware.RequireAdminService).Post(`) prova que a forma cega
// também é usada FORA das faixas protegidas, então a próxima nessa forma poderia
// nascer aberta com o gate dizendo PASS.
//
// A AST enxerga a CHAMADA, não a grafia: `r.With(a,b).Post(x)` e `r.Post(x)` são o
// mesmo `*ast.CallExpr` com `Sel.Name == "Post"`, e o receptor é irrelevante.
//
// Ver MEET-ASYNC-AMENDMENT-2026-08-17-route-auth-coverage-is-blind-to-40-percent.

// ErrRouterUnparseable means router.go exists but is not valid Go.
//
// Distinto de `ErrRouterNotFound` de propósito: "não achei o arquivo" e "achei e não
// entendi" são erros de instrumentação diferentes, e colapsá-los faria o gate
// enumerar zero e reportar o erro errado.
var ErrRouterUnparseable = errors.New("router.go existe mas não é Go válido — o gate não enumerou nada, e isso é erro de instrumentação, não ausência de rotas")

// ErrUndeclaredOffRouterMount means a production file outside router.go registers a
// route without a declared reason.
var ErrUndeclaredOffRouterMount = errors.New("uma rota é registrada fora de router.go sem declaração — o gate não decide se ela está protegida, ele recusa não vê-la")

// ErrEnumerationBelowFloor means the enumeration collapsed below the declared floor.
var ErrEnumerationBelowFloor = errors.New("a enumeração ficou abaixo do piso declarado — uma queda pela metade é o modo de falha realista, e recusar apenas o zero nunca a pegaria")

// routeVerbs are the chi registration methods. `Handle`/`HandleFunc`/`Method`/
// `MethodFunc` take the pattern in a later argument, which is why the pattern is
// located by SHAPE (first string literal starting with "/") and not by position.
var routeVerbs = map[string]bool{
	"Get": true, "Post": true, "Put": true, "Patch": true, "Delete": true,
	"Head": true, "Options": true, "Connect": true, "Trace": true,
	"Handle": true, "HandleFunc": true, "Method": true, "MethodFunc": true,
}

// RouteSite is one enumerated registration.
type RouteSite struct {
	File    string
	Line    int
	Verb    string
	Pattern string
}

// enumerateFile returns every route registration in one parsed file.
func enumerateFile(fset *token.FileSet, file *ast.File, rel string) []RouteSite {
	var sites []RouteSite
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !routeVerbs[sel.Sel.Name] {
			return true
		}
		pattern, ok := firstRouteLiteral(call.Args)
		if !ok {
			// Um verbo sem padrão literal não é enumerável — pode ser uma variável.
			// Não inventamos o valor: seguir em frente e deixar o piso e a varredura
			// da árvore cobrirem o buraco é mais honesto que adivinhar.
			return true
		}
		sites = append(sites, RouteSite{
			File:    rel,
			Line:    fset.Position(call.Lparen).Line,
			Verb:    sel.Sel.Name,
			Pattern: pattern,
		})
		return true
	})
	return sites
}

// firstRouteLiteral finds the first string argument that looks like a route path.
//
// Located by shape rather than by index because the chi verbs disagree on position:
// `Get(pattern, h)` puts it first, `Method(verb, pattern, h)` second. Indexing per
// verb would be a table that rots the day chi adds one.
func firstRouteLiteral(args []ast.Expr) (string, bool) {
	for _, a := range args {
		lit, ok := a.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil || !strings.HasPrefix(s, "/") {
			continue
		}
		return s, true
	}
	return "", false
}

// authRanges locates, by AST position, the `r.Group(func(r chi.Router){…})` whose
// body references middleware.Auth, plus the body of registerProtectedRoutes.
//
// Positions come from the AST rather than from brace counting: a brace inside a
// string or a comment shifts a counter and silently moves the range, which is the
// class of defect this whole change removes.
func authRanges(fset *token.FileSet, file *ast.File) (authStart, authEnd, protStart, protEnd int) {
	qualifiers := authQualifiers(file)
	ast.Inspect(file, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == "registerProtectedRoutes" && fn.Body != nil {
			protStart = fset.Position(fn.Body.Lbrace).Line
			protEnd = fset.Position(fn.Body.Rbrace).Line
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Group" || len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.FuncLit)
		if !ok || lit.Body == nil || !mentionsAuth(lit.Body, qualifiers) {
			return true
		}
		authStart = fset.Position(lit.Body.Lbrace).Line
		authEnd = fset.Position(lit.Body.Rbrace).Line
		return true
	})
	return
}

// authPackagePaths are the import paths that own the `Auth` middleware: the
// concern package it lives in today, and the flat package it lived in before
// B-059 split that one into five. Both are listed so this gate reads a tree
// from either side of that split.
var authPackagePaths = map[string]bool{
	"github.com/getloopscale/loopzero/api/internal/middleware/auth": true,
	"github.com/getloopscale/loopzero/api/internal/middleware":      true,
}

// authQualifiers returns the local names under which router.go imports the
// package that owns `Auth` — the alias when it has one, the path's last segment
// otherwise.
//
// Resolving the name from the import rather than hard-coding it is the
// difference between a gate that survives a rename and one that goes blind on
// it. Measured: when `internal/middleware` split into concern packages,
// router.go had to import the auth one as `mwauth`, because
// `internal/routes/auth` already owns the plain name — and a gate matching the
// letters `middleware.Auth` reported "could not locate the auth group" instead
// of the violations it exists to report. Failing to find its own subject is the
// one way an invariant gate can fail open.
func authQualifiers(file *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !authPackagePaths[path] {
			continue
		}
		if imp.Name != nil {
			names[imp.Name.Name] = true
			continue
		}
		names[path[strings.LastIndex(path, "/")+1:]] = true
	}
	return names
}

// mentionsAuth reports whether a block references the auth middleware's `Auth`
// — the proof that a chi Group is THE auth group and not some other grouping.
// `qualifiers` comes from authQualifiers, so an unrelated package that happens
// to export an `Auth` symbol does not qualify.
func mentionsAuth(block *ast.BlockStmt, qualifiers map[string]bool) bool {
	found := false
	ast.Inspect(block, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Auth" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && qualifiers[id.Name] {
			found = true
		}
		return true
	})
	return found
}

// scanOffRouter walks the production tree for route registrations outside router.go.
//
// It deliberately does NOT try to decide whether such a route is protected. Doing so
// would mean following the call chain from the mount helper back to its caller, and
// an analysis that gets THAT wrong is worse than no analysis — it would vouch for a
// route it never understood. What it refuses is the invisibility.
func scanOffRouter(root string, declared map[string]string) ([]string, error) {
	apiDir := filepath.Join(root, "api")
	var undeclared []string

	err := filepath.WalkDir(apiDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // uma árvore sem api/ não é defeito deste gate
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil //nolint:nilerr
		}
		rel = filepath.ToSlash(rel)
		if rel == routerPath {
			return nil
		}

		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			// Um arquivo de produção que não parseia é problema de quem o escreveu,
			// não deste gate; ignorar aqui evita que ele vire o reportador de erros
			// de compilação de todo o módulo.
			return nil //nolint:nilerr
		}
		sites := enumerateOnChiRouterParams(fset, f, rel)
		if len(sites) == 0 {
			return nil
		}
		// Declarada é declarada COM RAZÃO. Uma entrada vazia não declara nada — só
		// silencia, e silenciar por chave vazia é como uma isenção morre útil.
		if reason, ok := declared[rel]; ok && strings.TrimSpace(reason) != "" {
			return nil
		}
		for _, s := range sites {
			undeclared = append(undeclared, fmt.Sprintf("%s:%d  %s %s  (registro fora de router.go, sem declaração em OffRouterMounts)",
				s.File, s.Line, s.Verb, s.Pattern))
		}
		return nil
	})
	return undeclared, err
}

// enumerateOnChiRouterParams conta apenas registros feitos sobre um `chi.Router`
// RECEBIDO COMO PARÂMETRO — isto é, sobre o roteador público, montado de fora.
//
// # Por que a distinção é semântica e não uma lista de caminhos
//
// A primeira versão desta varredura contava qualquer registro HTTP sob `api/`, e
// achou 10 sítios. Nove deles não têm nada a ver com o roteador que este gate
// audita: `/debug/pprof/*` num `http.ServeMux` dedicado e numa porta própria
// (`api/cmd/main.go`), `/healthz`+`/readyz`+`/metrics` no mux do binário
// **controller**, e `/metrics` noutro mux (`internal/server/validation.go`).
//
// Excluí-los por caminho produziria uma lista de exceções de nove entradas — o
// mecanismo que o contraditor do MEET apontou como o que envelhece mal, e teria de
// crescer a cada mux novo. A propriedade que realmente os separa é outra e não
// envelhece: **rota registrada num `chi.Router` que a função recebeu é ponto de
// montagem do roteador público; rota num `ServeMux` local é outro servidor.**
//
// Isto é análise sintática, não de tipos: casa `chi.Router` pela forma da expressão
// do parâmetro. Um alias local (`type R = chi.Router`) escaparia — e escapa para o
// lado SEGURO, porque o registro simplesmente não é contado como montagem no
// roteador público, que é o que ele de fato não é sem alguém provar o contrário.
func enumerateOnChiRouterParams(fset *token.FileSet, file *ast.File, rel string) []RouteSite {
	var sites []RouteSite
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Type.Params == nil {
			continue
		}
		routers := map[string]bool{}
		for _, field := range fn.Type.Params.List {
			sel, ok := field.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Router" {
				continue
			}
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "chi" {
				continue
			}
			for _, name := range field.Names {
				routers[name.Name] = true
			}
		}
		if len(routers) == 0 {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !routeVerbs[sel.Sel.Name] {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok || !routers[recv.Name] {
				return true
			}
			pattern, ok := firstRouteLiteral(call.Args)
			if !ok {
				return true
			}
			sites = append(sites, RouteSite{File: rel, Line: fset.Position(call.Lparen).Line, Verb: sel.Sel.Name, Pattern: pattern})
			return true
		})
	}
	return sites
}
