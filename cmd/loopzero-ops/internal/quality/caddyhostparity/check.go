// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package caddyhostparity refuses a hostname that the edge verifier demands a certificate
// for and the Caddyfile never declares a site block for.
//
// # O defeito, medido
//
// Em 2026-08-18, o log de entregas do App do GitHub (id 3319955) mostrava **toda** entrega
// falhando do mesmo jeito, incluindo o `push` de um commit real:
//
//	POST https://cloud-dev.loopzero.example/api/v1/webhooks/github
//	giving up after 1 attempt(s): remote error: tls: internal error   code=500
//
// A primeira leitura culpou o `Caddyfile` por não declarar bloco para esse hostname. Estava
// invertida, e a revisão pegou: o `Caddyfile` **removeu** `cloud-dev` deliberadamente em
// 2026-08-10, com a razão escrita seis linhas abaixo do último bloco — era host público
// servindo redirecionamento, com superfície de auth concedida a ninguém, e a carga do webhook
// já vivia em `app-dev`, que tem o `handle /api/v1/webhooks/github`.
//
// O lado obsoleto era `infra/scripts/local/caddy-up-dev-public.sh`, que continuou exigindo
// certificado de um hostname que ninguém mais serve. Com isso, `--verify` reprovava 100% das
// vezes sobre a configuração correta — e um verificador que não pode passar é um verificador
// que ninguém roda. Foi assim que semanas de entregas falhas nunca viraram sinal, e é a
// divergência, não a ausência, que este gate mede.
//
// # O que este gate verifica
//
// Que todo hostname cobrado pelo verificador de borda tenha, no `Caddyfile`, um endereço de
// site que o atenda. É hermético — compara duas declarações do repositório, sem rede — então
// roda em toda entrega sem depender de certificado emitido.
//
// # O limite, dito em voz alta
//
// Ele compara declarações. Não prova que o certificado foi emitido, que o DNS resolve, nem
// que o destino atrás do endereço responde. Um bloco apontando para a porta errada passa aqui
// e falha ao vivo. Quando o `Caddyfile` usa `import`, a visão fica parcial e o gate recusa em
// vez de relatar paridade sobre metade do arquivo.
package caddyhostparity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrUnserved means a demanded hostname has no site block to terminate its TLS.
var ErrUnserved = errors.New("hostname exigido pela verificação e sem bloco de site")

// ErrVerifierMissing means the script that prescribes the host list is absent.
//
// Sem prescrição não há paridade a verificar, e reportar "sem divergência" seria verde
// sobre nada.
var ErrVerifierMissing = errors.New("verificador de borda ausente — sem lista exigida não há paridade")

// ErrCaddyfileMissing means the served-hosts side of the comparison is absent.
var ErrCaddyfileMissing = errors.New("arquivo Caddyfile ausente — sem blocos de site não há o que comparar")

// ErrPartialView means the Caddyfile pulls site blocks this gate cannot see.
//
// Relatar paridade sobre metade do arquivo seria verde sobre o que não foi lido.
var ErrPartialView = errors.New("o Caddyfile importa outro arquivo — paridade não verificável sobre visão parcial")

// ErrNothingScanned means the host list parsed empty.
//
// Zero exigidos é indistinguível de um parse quebrado, e é exatamente a forma
// `PASS — 0 hostnames` que este ecossistema já pagou para aprender.
var ErrNothingScanned = errors.New("nenhum hostname exigido varrido — não achar nada não é o mesmo que não achar nada errado")

const (
	verifierRel  = "infra/scripts/local/caddy-up-dev-public.sh"
	caddyfileRel = "infra/local/Caddyfile"
)

var (
	// hostsArrayRE isola o corpo do array `hosts=( ... )` do verificador. Ancorado em
	// início de linha para não capturar outro array antes dele, e fechando só num `)`
	// que abre linha — um `)` no corpo (de `$(...)`, de comentário) truncava a lista em
	// silêncio, e "1 de 6 exigidos" reportava-se igual a "6 exigidos".
	hostsArrayRE = regexp.MustCompile(`(?m)^\s*(?:local\s+)?hosts=\(\s*$([\s\S]*?)^\s*\)`)
	// hostRE aceita um hostname, com ou sem rótulo curinga à esquerda.
	hostRE = regexp.MustCompile(`^(\*\.)?[a-z0-9-]+(\.[a-z0-9-]+)+$`)
	// siteAddrLineRE captura a linha de endereço inteira de um bloco de site: coluna
	// zero até a chave que abre, na MESMA linha. A âncora de coluna é o que separa um
	// site servido de uma citação dentro de uma diretiva.
	siteAddrLineRE = regexp.MustCompile(`(?m)^([^\s{][^{\n]*?)[ \t]*\{[ \t]*$`)
	// importRE detecta blocos que este gate não enxerga.
	importRE = regexp.MustCompile(`(?m)^\s*import\s+\S`)
)

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Finding is one demanded hostname with nothing to terminate its TLS.
type Finding struct {
	Host       string
	DemandedBy string
}

// Outcome reports what was established.
type Outcome struct {
	Scanned  int
	Served   int
	Findings []Finding
}

// serves reports whether a declared site address covers the hostname, honouring the
// wildcard form: `*.preview-dev.loopzero.example` serves every single label beneath it, and
// neither a deeper host nor the apex.
func serves(block, host string) bool {
	// O Caddy serve tudo por trás de um catch-all; tratá-lo como não-servido seria
	// reprovar configuração que funciona.
	if block == "*" || strings.HasPrefix(block, ":") {
		return true
	}
	if block == host {
		return true
	}
	suffix, isWildcard := strings.CutPrefix(block, "*.")
	if !isWildcard {
		return false
	}
	rest, ok := strings.CutSuffix(host, "."+suffix)
	// Um rótulo só: o curinga do Caddy não atravessa ponto.
	return ok && rest != "" && !strings.Contains(rest, ".")
}

// demandedHosts parses the hostnames the edge verifier requires a certificate for.
func demandedHosts(root string) ([]string, error) {
	body, err := os.ReadFile(filepath.Clean(filepath.Join(root, verifierRel)))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrVerifierMissing, verifierRel)
	}
	m := hostsArrayRE.FindStringSubmatch(string(body))
	if m == nil {
		return nil, fmt.Errorf("%w: %s não declara `hosts=(...)` em forma reconhecível", ErrVerifierMissing, verifierRel)
	}
	var hosts []string
	seen := map[string]bool{}
	for _, f := range strings.Fields(m[1]) {
		f = strings.Trim(f, `"'`)
		if hostRE.MatchString(f) && !seen[f] {
			seen[f] = true
			hosts = append(hosts, f)
		}
	}
	return hosts, nil
}

// servedBlocks parses the site addresses the Caddyfile declares.
func servedBlocks(root string) ([]string, error) {
	body, err := os.ReadFile(filepath.Clean(filepath.Join(root, caddyfileRel)))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrCaddyfileMissing, caddyfileRel)
	}
	if importRE.MatchString(string(body)) {
		return nil, fmt.Errorf("%w: %s", ErrPartialView, caddyfileRel)
	}
	var blocks []string
	for _, m := range siteAddrLineRE.FindAllStringSubmatch(string(body), -1) {
		// A vírgula é a forma canônica de multi-host no Caddy; cada token é um endereço.
		for _, tok := range strings.Split(m[1], ",") {
			if a := normalizeAddr(tok); a != "" {
				blocks = append(blocks, a)
			}
		}
	}
	return blocks, nil
}

// normalizeAddr reduces a Caddy site address to the hostname it serves, dropping the
// scheme and port and folding case — the Caddy address is case-insensitive, and a gate
// that is not would refuse a working configuration.
func normalizeAddr(tok string) string {
	a := strings.ToLower(strings.TrimSpace(tok))
	if i := strings.Index(a, "://"); i >= 0 {
		a = a[i+3:]
	}
	// Porta sem host (`:443`) é catch-all e sobrevive inteira; com host, cai fora.
	if i := strings.LastIndex(a, ":"); i > 0 {
		a = a[:i]
	}
	return strings.TrimSuffix(a, "/")
}

// Check compares every demanded hostname against the declared site addresses.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	hosts, err := demandedHosts(root)
	if err != nil {
		return out, err
	}
	if len(hosts) == 0 {
		return out, fmt.Errorf("%w: %s", ErrNothingScanned, verifierRel)
	}
	blocks, err := servedBlocks(root)
	if err != nil {
		return out, err
	}

	out.Scanned = len(hosts)
	for _, h := range hosts {
		covered := false
		for _, b := range blocks {
			if serves(b, h) {
				covered = true
				break
			}
		}
		if covered {
			out.Served++
			continue
		}
		out.Findings = append(out.Findings, Finding{Host: h, DemandedBy: verifierRel})
	}

	if len(out.Findings) > 0 {
		names := make([]string, 0, len(out.Findings))
		for _, f := range out.Findings {
			names = append(names, f.Host)
		}
		return out, fmt.Errorf("%w: %s — exigido por %s, ausente de %s",
			ErrUnserved, strings.Join(names, ", "), verifierRel, caddyfileRel)
	}
	return out, nil
}
