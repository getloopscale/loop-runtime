// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package leak prova que a borda pública FECHA sem credencial.
//
// POR QUE ISTO EXISTE. Em 2026-08-10, sondando as superfícies públicas do app-dev sem
// credencial nenhuma, todas as dez responderam 401 (ou 302 para o fluxo de login). Era o
// resultado certo — e não havia nada que o garantisse. Nenhum teste afirmava o fecho, e
// nenhum gate o verificava: era verdade por sorte, não por contrato. A primeira rota
// montada fora do grupo autenticado passaria despercebida até um Red Team achá-la.
//
// O gate hermético `proxy-exposure` prova que toda rota da borda foi DECLARADA por um
// humano; ele mesmo diz, no próprio cabeçalho, que provar a resposta exige uma sonda viva
// e que essa sonda é `task leak:live`. Este pacote é essa sonda — ela era citada e não
// existia.
//
// ONDE ELA MORA, E POR QUÊ. Aqui, e não num `.sh`: a regra D3 proíbe novo shell por
// padrão, e uma sonda HTTP é justamente o caso em que o Go não precisa de ajuda — é
// `net/http` com timeout, sem processo externo. Também não é um `quality` gate: todos os
// gates são herméticos por contrato, e este depende de rede contra um alvo vivo. Fica
// como subcomando próprio, ao lado de `chaos` e `drill`, que são os que tocam ambiente.
//
// O QUE ESTA SONDA NÃO PROVA: que uma resposta autenticada expõe só o que devia. Isso
// depende do corpo, varia por rota e não cabe numa sonda genérica — o `proxy-exposure`
// registra a forma de cada uma na coluna de descrição, revisada por humano.
package leak

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Verdict é o resultado de uma sonda contra um caminho.
type Verdict struct {
	Host    string
	Path    string
	Status  int
	Passed  bool
	Reason  string
	Skipped bool
}

// Target é um caminho concreto a sondar.
//
// `PublicBySpec` inverte a asserção: a maioria das rotas tem de NEGAR sem credencial, mas
// a descoberta OIDC e o health têm de RESPONDER — clientes as leem antes de possuir
// qualquer token, e um 401 ali quebra o login em vez de protegê-lo.
type Target struct {
	Host         string
	Path         string
	PublicBySpec bool
	// PorQue explica QUEM depende desta rota responder. A razão muda o conserto: a
	// descoberta OIDC é lida por um programa antes de existir token; a raiz é alcançada
	// por uma pessoa que digitou o endereço. Uma mensagem única para os dois casos
	// mandaria quem lê o relatório para o lugar errado.
	PorQue string
}

// closed reporta se o código nega acesso ao dado.
//
// 401 e 403 negam; 302/303 levam o navegador à identidade. 404 NÃO conta: esconder não é
// negar, e aceitar 404 mascara rota quebrada como se fosse proteção — foi assim que
// `/credentials` passou meses devolvendo 404 enquanto o inventário o declarava vivo.
func closed(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden,
		http.StatusFound, http.StatusSeeOther:
		return true
	}
	return false
}

// Probe sonda um alvo SEM credencial alguma e devolve o veredito.
func Probe(ctx context.Context, client *http.Client, t Target) Verdict {
	v := Verdict{Host: t.Host, Path: t.Path}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+t.Host+t.Path, nil)
	if err != nil {
		v.Reason = fmt.Sprintf("requisição inválida: %v", err)
		return v
	}
	resp, err := client.Do(req)
	if err != nil {
		v.Skipped = true
		v.Reason = fmt.Sprintf("inalcançável: %v", err)
		return v
	}
	defer func() { _ = resp.Body.Close() }()
	v.Status = resp.StatusCode

	if t.PublicBySpec {
		// 200 serve o conteúdo; 302/303 leva a ele. Os dois cumprem o contrato de "esta
		// rota leva a algum lugar" — o que reprova é 404 ou 5xx, que é não ter destino.
		// Exigir 200 aqui reprovaria o próprio conserto da raiz, que é um redirecionamento
		// deliberado para o login.
		if resp.StatusCode == http.StatusOK ||
			resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
			v.Passed = true
			v.Reason = fmt.Sprintf("respondeu %d", resp.StatusCode)
		} else {
			porque := t.PorQue
			if porque == "" {
				porque = "é pública por especificação OIDC e clientes a leem antes de ter qualquer token"
			}
			v.Reason = fmt.Sprintf("devolveu %d e precisa responder — %s", resp.StatusCode, porque)
		}
		return v
	}

	if closed(resp.StatusCode) {
		v.Passed = true
		return v
	}
	v.Reason = fmt.Sprintf("devolveu %d SEM credencial; a borda tem de negar (401/403) ou levar ao login (302)",
		resp.StatusCode)
	return v
}

// representantes mapeia uma família declarada no inventário para UM caminho concreto.
//
// Um padrão como `/v1/*` não é requisitável; a sonda precisa de um caminho real por trás
// dele. As famílias sem representante NÃO são silenciadas — `Targets` as devolve à parte
// para que a chamadora reporte buraco de cobertura em vez de somar um PASS que não mediu
// nada.
var representantes = map[string]string{
	"/engine/*":                 "/engine/api/v1/projects",
	"/v1/*":                     "/v1/dashboard/status/versions",
	"/v1/integrations/github/*": "/v1/integrations/github/install",
	"/v1/memory/*":              "/v1/memory/health",
	"/v1/rag/*":                 "/v1/rag/health",
	"/v1/lens/*":                "/v1/lens/health",
	"/sessions/whoami":          "/sessions/whoami",
}

// isentas são famílias que esta sonda não mede, cada uma por uma razão nomeada.
//
// Isentar é uma decisão, não um esquecimento: sem esta lista, uma família nova sem
// representante seria confundida com uma isenta e sumiria da cobertura.
var isentas = map[string]string{
	"/self-service/*":         "fluxo de navegador; 200 é a resposta correta, e o corpo é o que o Kratos precisa entregar",
	"@auth_bff":               "matcher nomeado do Caddy, sem caminho fixo a requisitar",
	"/api/v1/webhooks/github": "autenticado por HMAC e não por credencial de usuário; coberto pelo B-021",
	"-":                       "host inteiro (raiz), sem rota declarada",
}

// Targets deriva os alvos do inventário declarado.
//
// Os alvos vêm de `infra/quality/proxy-exposure.txt`, e NÃO de uma lista própria. Uma
// segunda lista envelheceria sozinha: alguém adicionaria uma rota ao Caddyfile, a
// declararia no inventário (porque o gate obriga) e esqueceria daqui — e a rota nova
// seria justamente a não testada. Ler o inventário faz a cobertura acompanhar a
// superfície por construção.
//
// Devolve os alvos e as famílias declaradas SEM representante, que são buraco de
// cobertura e não silêncio.
func Targets(inventário string, host string) (alvos []Target, semRepresentante []string, err error) {
	b, err := os.ReadFile(inventário)
	if err != nil {
		return nil, nil, fmt.Errorf("ler inventário de exposição %s: %w", inventário, err)
	}

	vistos := map[string]bool{}
	for _, linha := range strings.Split(string(b), "\n") {
		linha = strings.TrimSpace(linha)
		if linha == "" || strings.HasPrefix(linha, "#") {
			continue
		}
		decl := linha
		if i := strings.Index(linha, "|"); i >= 0 {
			decl = strings.TrimSpace(linha[:i])
		}
		campos := strings.Fields(decl)
		if len(campos) < 2 || campos[0] != host {
			continue
		}
		família := campos[1]
		if vistos[família] {
			continue
		}
		vistos[família] = true

		if _, ok := isentas[família]; ok {
			continue
		}
		if p, ok := representantes[família]; ok {
			alvos = append(alvos, Target{Host: host, Path: p})
			continue
		}
		semRepresentante = append(semRepresentante, família)
	}

	sort.Slice(alvos, func(i, j int) bool { return alvos[i].Path < alvos[j].Path })
	sort.Strings(semRepresentante)
	return alvos, semRepresentante, nil
}

// IdentityTargets são as rotas do host de identidade, com a asserção invertida nas duas
// que a especificação OIDC obriga a serem públicas.
//
// A raiz entra com `PublicBySpec` porque quem a alcança é uma PESSOA que digitou o
// endereço — e ela precisa de um caminho para o login, não de um beco sem saída. Medido
// em 2026-08-10: `https://auth-dev.loopzero.example/` devolvia `404 page not found` em texto
// puro. O 404 vinha do próprio Kratos, que é uma API de identidade e não serve página na
// raiz; a borda encaminhava certo e recebia o 404 de volta. Nenhum fluxo do produto leva
// ali (os usos do host são todos programáticos: base de API, emissor de token, origem
// CORS), mas um endereço público que não leva a lugar nenhum é defeito de produto mesmo
// quando nada aponta para ele.
func IdentityTargets(host string) []Target {
	return []Target{
		{Host: host, Path: "/.well-known/openid-configuration", PublicBySpec: true,
			PorQue: "é a descoberta OIDC, lida por todo cliente antes de existir token"},
		{Host: host, Path: "/health/alive", PublicBySpec: true,
			PorQue: "é o health do serviço de identidade"},
		{Host: host, Path: "/", PublicBySpec: true,
			PorQue: "quem digita o endereço é uma pessoa, e precisa de um caminho para o login em vez de um beco sem saída"},
		{Host: host, Path: "/userinfo"},
	}
}

// DefaultClient é o cliente da sonda: timeout curto e SEM seguir redirecionamento, porque
// o 302 do fluxo de login É a evidência — segui-lo trocaria a prova pelo destino dela.
func DefaultClient() *http.Client {
	return &http.Client{
		Timeout: 12 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// DiscoveryLeak reporta os campos do documento de descoberta OIDC que anunciam um host
// inalcançável de fora do cluster, ou texto claro.
//
// POR QUE ISTO É UMA SONDA E NÃO UM GATE. O chart do Hydra JÁ resolve isto: ele usa
// `publicAuthUrl` quando presente, e um comentário nele registra que alguém já pagou esse
// preço antes ("otherwise Hydra emits verification_uri with the cluster-internal host").
// A configuração está certa; o que roda é que não estava. Medido em 2026-08-10 no
// app-dev: NOVE campos — incluindo `issuer` e `jwks_uri` — anunciavam
// `http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444`, porque o sync rodou sem a
// variável. Nenhum gate hermético pega isso, porque o defeito não está no repositório.
//
// O impacto é concreto: um cliente OIDC que siga a descoberta — o mecanismo padrão —
// tenta resolver um nome que só existe dentro do cluster, em texto claro. E o `issuer` é
// o valor que entra no token e que todo verificador compara; anunciá-lo errado quebra a
// validação em quem consumir o token depois.
func DiscoveryLeak(doc map[string]any) []string {
	var vazamentos []string
	for campo, valor := range doc {
		s, ok := valor.(string)
		if !ok {
			continue
		}
		switch {
		case strings.Contains(s, ".svc.cluster.local"):
			vazamentos = append(vazamentos, fmt.Sprintf(
				"%s anuncia o host interno %q — inalcançável fora do cluster", campo, s))
		case strings.HasPrefix(s, "http://"):
			vazamentos = append(vazamentos, fmt.Sprintf(
				"%s anuncia texto claro %q — a borda serve HTTPS", campo, s))
		}
	}
	sort.Strings(vazamentos)
	return vazamentos
}
