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
// O handshake reproduz fora do GitHub — `openssl s_client -servername cloud-dev.loopzero.example`
// devolve `tlsv1 alert internal error` (alerta 80, lado servidor), enquanto `app-dev`, no
// mesmo host e no mesmo Caddy, serve certificado normalmente.
//
// A causa estava em `infra/local/Caddyfile`: sete blocos de site declarados e **nenhum** para
// `cloud-dev.loopzero.example`. Sem bloco e sem TLS on-demand, o Caddy não tem certificado para
// aquele SNI e derruba a conexão antes de qualquer status HTTP. O nome aparecia no arquivo
// apenas dentro de strings de CSP — citado, nunca servido.
//
// Não era hostname esquecido: o DNS o provisiona, `infra/quality/proxy-exposure.txt` o lista,
// e `caddy-up-dev-public.sh` **exige** emissor Let's Encrypt para ele e sai 1 sem isso.
//
// # Por que um gate, e não só o conserto
//
// Porque o verificador que veria isso não podia passar. Com o nome na lista e fora do
// Caddyfile, `--verify` reprovava 100% das vezes sobre uma configuração correta — e um gate
// que não pode passar é um gate que ninguém roda. Foi assim que semanas de entregas falhas
// nunca viraram sinal. Este gate é hermético: lê a árvore, não a rede, então roda em toda
// entrega sem depender de certificado emitido.
//
// # O limite, dito em voz alta
//
// Ele compara **duas declarações do repositório** — a lista do verificador e os blocos do
// Caddyfile. Não prova que o certificado foi emitido, que o DNS resolve, nem que o destino
// atrás do bloco responde. Um bloco apontando para a porta errada passa aqui e falha ao vivo;
// essa metade é `B-151`, e prometer o contrário seria a superestimação que este ecossistema
// já mede em vários itens.
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

// ErrUnownedFreeze means a frozen host names no item that owns fixing it.
//
// Congelar sem dono é esconder: a linha some do relatório e ninguém fica responsável.
var ErrUnownedFreeze = errors.New("hostname congelado sem item que o resolva")

// ErrStaleFreeze means a frozen host is now served — the freeze outlived its reason.
var ErrStaleFreeze = errors.New("congelamento obsoleto — o hostname já tem bloco de site")

// ErrNothingScanned means the host list parsed empty.
//
// Zero exigidos é indistinguível de um parse quebrado, e é exatamente a forma
// `PASS — 0 hostnames` que este ecossistema já pagou para aprender.
var ErrNothingScanned = errors.New("nenhum hostname exigido varrido — não achar nada não é o mesmo que não achar nada errado")

const (
	verifierRel  = "infra/scripts/local/caddy-up-dev-public.sh"
	caddyfileRel = "infra/local/Caddyfile"
	baselineRel  = "infra/quality/caddy-unserved-baseline.txt"
)

var (
	// hostsArrayRE isola o corpo do array `hosts=( ... )` do verificador.
	hostsArrayRE = regexp.MustCompile(`(?s)hosts=\(\s*(.*?)\)`)
	// hostRE aceita um hostname, com ou sem rótulo curinga à esquerda.
	hostRE = regexp.MustCompile(`^(\*\.)?[a-z0-9-]+(\.[a-z0-9-]+)+$`)
	// siteBlockRE casa um bloco de site: hostname na coluna zero abrindo chave.
	// A âncora de coluna é o que separa um site servido de uma citação dentro de
	// uma diretiva — foi assim que `cloud-dev` viveu no arquivo sem ser servido.
	siteBlockRE = regexp.MustCompile(`(?m)^((?:\*\.)?[a-z0-9.*-]+\.[a-z]{2,})\s*\{`)
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
	Frozen   int
	Findings []Finding
}

// frozen reads the ratchet: hostnames already known unserved, each naming the item that
// owns fixing it. Absent file means an empty freeze, which is the strictest state.
func frozen(root string) (map[string]string, error) {
	body, err := os.ReadFile(filepath.Clean(filepath.Join(root, baselineRel)))
	if err != nil {
		return map[string]string{}, nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			return nil, fmt.Errorf("%w: %q em %s", ErrUnownedFreeze, line, baselineRel)
		}
		out[f[0]] = f[1]
	}
	return out, nil
}

// serves reports whether a declared site block covers the hostname, honouring the wildcard
// form: `*.preview-dev.loopzero.example` serves every single label beneath it.
func serves(block, host string) bool {
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
		return nil, fmt.Errorf("%w: %s não declara `hosts=(...)`", ErrVerifierMissing, verifierRel)
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

// servedBlocks parses the site blocks the Caddyfile declares.
func servedBlocks(root string) ([]string, error) {
	body, err := os.ReadFile(filepath.Clean(filepath.Join(root, caddyfileRel)))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrCaddyfileMissing, caddyfileRel)
	}
	var blocks []string
	for _, m := range siteBlockRE.FindAllStringSubmatch(string(body), -1) {
		blocks = append(blocks, m[1])
	}
	return blocks, nil
}

// Check compares every demanded hostname against the declared site blocks.
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

	freeze, err := frozen(root)
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
			// Um congelamento sobre hostname já servido perdeu a razão de existir, e uma
			// isenção que ninguém revoga é como um gate volta a mentir devagar.
			if _, isFrozen := freeze[h]; isFrozen {
				return out, fmt.Errorf("%w: %s — remova a linha de %s", ErrStaleFreeze, h, baselineRel)
			}
			continue
		}
		if _, isFrozen := freeze[h]; isFrozen {
			out.Frozen++
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
