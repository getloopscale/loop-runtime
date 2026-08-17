// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routeauthcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A enumeração passou de regex sobre linhas para AST, e o escopo deixou de ser um
// arquivo — MEET-ASYNC-AMENDMENT-2026-08-17-route-auth-coverage-is-blind-to-40-percent.
//
// O regex exigia que `r.` fosse seguido IMEDIATAMENTE pelo verbo, então
// `r.With(mw...).Post(...)` — 28 rotas — não casava. O gate reportava 42 sobre um
// sistema com ~71.

// tree escreve uma árvore mínima com um router.go e, opcionalmente, outros arquivos.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// routerWith monta um router.go sintático válido com um grupo Auth contendo `inside`
// e registros soltos em `outside`.
func routerWith(inside, outside string) string {
	return `package server

func NewRouter(cfg Config) *chi.Mux {
	r := chi.NewRouter()
` + outside + `
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth)
` + inside + `
	})
	return r
}
`
}

// A forma que o regex não via. É a mais numerosa do roteador real.
func TestWithChainedRegistrationIsEnumerated(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.With(rateLimit...).Post(\"/api/v1/builds\", h)\n", ""),
	})
	out, err := Check(Config{RepoRoot: root, MinRoutes: 1})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.PassCount != 1 {
		t.Fatalf("`r.With(...).Post(` é registro de rota e precisa ser enumerado; PassCount=%d", out.PassCount)
	}
}

// E precisa ser enumerada TAMBÉM quando está desprotegida — senão a mudança
// trocaria uma cegueira por outra, contando só o que já passa.
func TestWithChainedRegistrationOutsideAuthIsAViolation(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", "\tr.With(rateLimit...).Post(\"/api/v1/aberta\", h)\n"),
	})
	out, err := Check(Config{RepoRoot: root, MinRoutes: 1})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("rota `r.With(...)` fora do grupo Auth e fora do allowlist deve reprovar; err=%v out=%+v", err, out)
	}
	if len(out.Violations) != 1 || !strings.Contains(out.Violations[0], "/api/v1/aberta") {
		t.Fatalf("a violação precisa nomear a rota; got %v", out.Violations)
	}
}

// A AST não pode confundir texto com código: um caminho citado num comentário ou
// numa string não é uma rota.
func TestCommentedRegistrationIsNotEnumerated(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n",
			"\t// r.Post(\"/api/v1/fantasma\", h) — desligada em 2026-01\n"),
	})
	out, err := Check(Config{RepoRoot: root, MinRoutes: 1})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.PassCount != 1 {
		t.Fatalf("registro comentado não é rota; PassCount=%d", out.PassCount)
	}
}

// A instância que o item mediu: uma rota montada por helper, noutro arquivo, que o
// gate nunca viu. Ele não tenta decidir se ela está protegida — recusa a
// invisibilidade.
func TestRouteRegisteredOutsideRouterFileIsRefused(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", ""),
		"api/internal/routes/x/mount.go": `package x

func Register(r chi.Router) {
	r.Get("/api/v1/escondida", h)
}
`,
	})
	_, err := Check(Config{RepoRoot: root, MinRoutes: 1})
	if !errors.Is(err, ErrUndeclaredOffRouterMount) {
		t.Fatalf("registro fora do router.go precisa reprovar até ser declarado; err=%v", err)
	}
}

// Declarado com razão, passa. O custo de entrar na lista é escrever o porquê.
func TestDeclaredOffRouterMountPasses(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", ""),
		"api/internal/routes/declarado/mount.go": `package declarado

func Register(r chi.Router) {
	r.Get("/api/v1/declarada", h)
}
`,
	})
	cfg := Config{RepoRoot: root, MinRoutes: 1, OffRouterMounts: map[string]string{
		"api/internal/routes/declarado/mount.go": "ponto de montagem canônico, chamado de dentro da faixa protegida",
	}}
	if _, err := Check(cfg); err != nil {
		t.Fatalf("um ponto de montagem declarado com razão deve passar: %v", err)
	}
}

// Uma entrada sem razão não é uma declaração — é um silenciamento.
func TestOffRouterMountWithoutReasonIsRefused(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", ""),
		"api/internal/routes/vazio/mount.go": `package vazio

func Register(r chi.Router) {
	r.Get("/api/v1/x", h)
}
`,
	})
	cfg := Config{RepoRoot: root, MinRoutes: 1, OffRouterMounts: map[string]string{
		"api/internal/routes/vazio/mount.go": "   ",
	}}
	if _, err := Check(cfg); !errors.Is(err, ErrUndeclaredOffRouterMount) {
		t.Fatalf("declaração sem razão não declara nada; err=%v", err)
	}
}

// Um teste não é produção. Contar `_test.go` inflaria o piso e transformaria
// fixture em superfície pública.
func TestRegistrationInsideATestFileIsIgnored(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", ""),
		"api/internal/routes/x/mount_test.go": `package x

func TestX(t *testing.T) {
	r.Get("/api/v1/fixture", h)
}
`,
	})
	if _, err := Check(Config{RepoRoot: root, MinRoutes: 1}); err != nil {
		t.Fatalf("registro em arquivo de teste não é superfície pública: %v", err)
	}
}

// O piso: recusar só o zero deixa passar a queda pela metade, que é o modo de falha
// realista. `MinRoutes` é uma contagem MÍNIMA — não a exata, que viraria manutenção
// a cada rota nova e seria desligada pela primeira pessoa a tropeçar nela.
func TestEnumerationCollapseBelowFloorFails(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/api/v1/ok\", h)\n", ""),
	})
	_, err := Check(Config{RepoRoot: root, MinRoutes: 10})
	if !errors.Is(err, ErrEnumerationBelowFloor) {
		t.Fatalf("1 rota sob um piso de 10 é uma queda, não um sistema pequeno; err=%v", err)
	}
}

// A fronteira oposta do piso: exatamente no mínimo passa. Sem isto, endurecer o
// piso viraria uma regra que o próprio repositório não satisfaz.
func TestExactlyAtTheFloorPasses(t *testing.T) {
	root := tree(t, map[string]string{
		routerPath: routerWith("\t\tr.Get(\"/a\", h)\n\t\tr.Get(\"/b\", h)\n", ""),
	})
	if _, err := Check(Config{RepoRoot: root, MinRoutes: 2}); err != nil {
		t.Fatalf("exatamente no piso deve passar: %v", err)
	}
}

// Um router.go que não parseia é erro de instrumentação, e precisa dizer isso — não
// enumerar zero e cair num erro que fala de outra coisa.
func TestUnparseableRouterIsAnInstrumentationError(t *testing.T) {
	root := tree(t, map[string]string{routerPath: "package server\nfunc quebrado( {\n"})
	_, err := Check(Config{RepoRoot: root, MinRoutes: 1})
	if !errors.Is(err, ErrRouterUnparseable) {
		t.Fatalf("um router.go inválido precisa reprovar como erro de parse; err=%v", err)
	}
}

// A população real (B-109). Um mutante sintético prova que o detector dispara; só
// isto prova o que ele vê no que existe — e é a asserção que fixa a correção, porque
// a contagem tinha de SUBIR de 42 para ~71.
func TestRealRouterEnumeratesTheChainedFormToo(t *testing.T) {
	out, err := Check(Config{RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("Check na árvore real: %v", err)
	}
	if out.PassCount < 60 {
		t.Fatalf("PassCount=%d — o roteador real tem ~42 registros diretos MAIS ~28 na forma `r.With(...)`; "+
			"um número perto de 42 significa que a forma encadeada voltou a ser invisível", out.PassCount)
	}
}
