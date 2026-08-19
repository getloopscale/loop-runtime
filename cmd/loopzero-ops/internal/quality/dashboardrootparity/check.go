// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package dashboardrootparity refuses a runbook that names a build path the edge does not
// serve.
//
// # O defeito, medido
//
// B-039, 2026-08-18. Implantar um conserto no painel exigiu descobrir onde o codigo mora no
// droplet, e a descoberta foi cara porque o caminho obvio engana:
//
//	~/loopzero-cloud                     e um clone do UMBRELLA (loopzero-workspace), nao do produto
//	~/.buildmeta-cloud/loopzero-cloud    ad-hoc, refspec restrito a `develop`
//	~/.b017-prova/loopzero-cloud         ad-hoc, refspec restrito, e de OUTRA organizacao
//	~/loopzero-cloud/loopzero-cloud          o canonico — refspec completo, e o que o Caddy serve
//
// O canonico sempre funcionou: `git fetch` popula `origin/workspace` normalmente ali. O que
// nao existia era alguem dizer qual dos quatro era ele — e os dois quebrados sao os que uma
// busca encontra primeiro.
//
// # Por que um gate, e nao so documentacao
//
// Porque a documentacao sozinha apodrece na direcao mais perigosa. Quem mudar o `root *` do
// Caddyfile deixa o runbook apontando para um caminho que ja nao e servido — e ai a proxima
// pessoa paga a mesma descoberta COM um documento que confirma o erro, que e pior do que nao
// ter documento.
//
// # O limite, dito em voz alta
//
// Ele compara duas declaracoes desta arvore: o `root *` do Caddyfile e o caminho citado no
// runbook. Nao prova que o diretorio existe no droplet, que o clone la esta no remoto certo,
// nem que `dist` foi construido. Essa metade e viva e nao cabe num gate hermetico.
package dashboardrootparity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrDivergent means the runbook names a path the edge does not serve.
var ErrDivergent = errors.New("o runbook nomeia um caminho que a borda nao serve")

// ErrUndocumented means the runbook names no build path at all — the state before B-039.
var ErrUndocumented = errors.New("o runbook nao diz onde o painel e construido")

// ErrNothingServed means the Caddyfile serves no file root, so there is no parity to check.
//
// Reportar "de acordo" aqui seria verde sobre nada.
var ErrNothingServed = errors.New("o Caddyfile nao declara raiz de arquivo — nao ha paridade a verificar")

// ErrMissingFile means one of the two sides is absent.
var ErrMissingFile = errors.New("arquivo ausente")

const (
	caddyfileRel = "infra/local/Caddyfile"
	runbookRel   = "docs/operations/local-public-dashboard.md"
)

var (
	// rootRE captura o caminho servido por `root *`.
	rootRE = regexp.MustCompile(`(?m)^\s*root\s+\*\s+(\S+)`)
	// homeRE reconhece o prefixo de workstation, para compara-lo com a forma `~/`.
	//
	// O runbook NAO pode carregar `/home/<usuario>/`: o gate `publish-hygiene` recusa
	// caminho de workstation em arquivo versionado, com a razao de que historia publicada
	// e publica retroativamente. O `Caddyfile` carrega porque e isencao COM PRAZO, que e
	// divida reconhecida — nao licenca para espalhar.
	homeRE = regexp.MustCompile(`^/(?:home|Users)/[^/]+/`)
)

// normalizarHome troca o prefixo de workstation por `~/`, para que os dois lados sejam
// comparaveis sem que o documento precise citar o caminho absoluto de alguem.
func normalizarHome(p string) string {
	return homeRE.ReplaceAllString(p, "~/")
}

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Outcome reports both sides, so a failure names them.
type Outcome struct {
	Served     string
	Documented string
}

// Check compares the served root with the path the runbook documents.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	caddy, err := os.ReadFile(filepath.Clean(filepath.Join(root, caddyfileRel)))
	if err != nil {
		return out, fmt.Errorf("%w: %s", ErrMissingFile, caddyfileRel)
	}
	m := rootRE.FindStringSubmatch(string(caddy))
	if m == nil {
		return out, fmt.Errorf("%w: %s", ErrNothingServed, caddyfileRel)
	}
	out.Served = m[1]

	// O runbook fala do DIRETORIO do clone; o Caddy serve `<clone>/dashboard/dist`.
	// Comparar o prefixo e o que liga os dois sem exigir que o texto repita o sufixo.
	esperado := normalizarHome(strings.TrimSuffix(out.Served, "/dashboard/dist"))

	runbook, err := os.ReadFile(filepath.Clean(filepath.Join(root, runbookRel)))
	if err != nil {
		return out, fmt.Errorf("%w: %s", ErrMissingFile, runbookRel)
	}
	texto := string(runbook)

	if strings.Contains(texto, esperado) {
		out.Documented = esperado
		return out, nil
	}

	// Documenta ALGUM caminho de clone, mas outro: divergencia, que e o caso perigoso.
	cloneRE := regexp.MustCompile(`([~/][A-Za-z0-9_./-]*loopzero-cloud[A-Za-z0-9_./-]*)`)
	if c := cloneRE.FindString(texto); c != "" {
		out.Documented = c
		return out, fmt.Errorf("%w: serve %q, runbook diz %q", ErrDivergent, out.Served, c)
	}
	return out, fmt.Errorf("%w: a borda serve %q e %s nao o menciona",
		ErrUndocumented, out.Served, runbookRel)
}
