// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// leak subcommand group — a sonda LIVE que prova o fecho da borda pública.
//
// Fica fora de `quality` porque todo gate de qualidade é hermético por contrato (sem
// rede, sem kubectl, sem helmfile) e esta sonda depende de um alvo vivo. Fica ao lado de
// `chaos` e `drill`, que são os grupos que tocam ambiente.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/leak"
)

const leakHelpText = `loopzero-ops leak — sonda LIVE da superfície pública

USAGE:
  loopzero-ops leak live  [flags]   sonda a borda pública sem credencial
  loopzero-ops leak ports [flags]   audita as portas do host contra o registro declarado

O QUE ELA PROVA:
  Que a borda pública NEGA sem credencial (401/403) ou leva ao login (302), e que as
  rotas públicas por especificação OIDC continuam respondendo 200. Os alvos são
  derivados de infra/quality/proxy-exposure.txt, então a cobertura acompanha a
  superfície por construção — uma família declarada lá sem representante concreto é
  reportada como buraco, não silenciada.

  404 NÃO conta como fecho: esconder não é negar, e aceitá-lo mascara rota quebrada
  como se fosse proteção.

O QUE ELA NÃO PROVA:
  Que uma resposta autenticada expõe só o que devia — isso depende do corpo e está
  registrado, por rota, na coluna de descrição do inventário.

FLAGS:
  --host        host da aplicação      (padrão: app-dev.loopzero.example)
  --auth-host   host da identidade     (padrão: auth-dev.loopzero.example)
  --inventory   inventário declarado   (padrão: infra/quality/proxy-exposure.txt)
  --json        saída legível por máquina

SAÍDA:
  0 = a borda fecha    1 = algo passou sem credencial, ou uma família não é medida
`

type leakReport struct {
	Pass     int      `json:"pass"`
	Fail     int      `json:"fail"`
	Skip     int      `json:"skip"`
	Failures []string `json:"failures"`
}

func runLeak(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(leakHelpText)
		return 0
	}
	if args[0] == "ports" {
		return runLeakPorts(args[1:])
	}
	if args[0] != "live" {
		fmt.Fprintf(os.Stderr, "subcomando desconhecido %q (use: live|ports)\n", args[0])
		return 1
	}

	fs := flag.NewFlagSet("leak live", flag.ContinueOnError)
	host := fs.String("host", "app-dev.loopzero.example", "host da aplicação")
	authHost := fs.String("auth-host", "auth-dev.loopzero.example", "host da identidade")
	inventory := fs.String("inventory", "infra/quality/proxy-exposure.txt", "inventário declarado")
	asJSON := fs.Bool("json", false, "saída legível por máquina")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}

	alvos, semRepresentante, err := leak.Targets(*inventory, *host)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	alvos = append(alvos, leak.IdentityTargets(*authHost)...)

	cli := leak.DefaultClient()
	ctx := context.Background()
	rep := leakReport{Failures: []string{}}

	for _, alvo := range alvos {
		v := leak.Probe(ctx, cli, alvo)
		switch {
		case v.Skipped:
			rep.Skip++
			if !*asJSON {
				fmt.Printf("  SKIP  %-46s %s\n", v.Host+v.Path, v.Reason)
			}
		case v.Passed:
			rep.Pass++
			if !*asJSON {
				fmt.Printf("  ok    %-46s %d\n", v.Host+v.Path, v.Status)
			}
		default:
			rep.Fail++
			rep.Failures = append(rep.Failures, v.Host+v.Path+" "+v.Reason)
			if !*asJSON {
				fmt.Printf("  FAIL  %-46s %d\n", v.Host+v.Path, v.Status)
			}
		}
	}

	// O documento de descoberta OIDC é lido por todo cliente ANTES de existir token, e o
	// `issuer` que ele anuncia é o valor que entra no token e que todo verificador compara.
	// Anunciar um host de cluster ali quebra quem consumir o token depois — e nenhum gate
	// hermético pega, porque o defeito não está no repositório: o chart já sabe usar o FQDN
	// público, e o que roda é que não recebeu a variável.
	if vazamentos := probeDiscovery(ctx, cli, *authHost); len(vazamentos) > 0 {
		for _, v := range vazamentos {
			rep.Fail++
			rep.Failures = append(rep.Failures, *authHost+" descoberta OIDC: "+v)
			if !*asJSON {
				fmt.Printf("  FAIL  %-46s %s\n", *authHost+"/.well-known/openid-configuration", v)
			}
		}
	}

	// Um marcador de substituição que sobreviveu no cluster quebra o login inteiro e só
	// aparece quando alguém tenta entrar — foi o `Erro 401: invalid_client` de 2026-08-10.
	// A checagem só roda quando o kubectl alcança o cluster; fora dele, silêncio (a sonda
	// mede a borda pública e não pode exigir acesso ao cluster para funcionar).
	for _, achado := range probePlaceholders() {
		rep.Fail++
		rep.Failures = append(rep.Failures, "configuração de identidade: "+achado)
		if !*asJSON {
			fmt.Printf("  FAIL  %-46s %s\n", "ory-kratos-config", achado)
		}
	}

	// Uma família declarada e não medida é buraco de cobertura. Contá-la como falha é
	// deliberado: o modo de falha que esta sonda existe para impedir é justamente a rota
	// nova que ninguém sonda, e um aviso silencioso reproduziria isso.
	for _, f := range semRepresentante {
		rep.Fail++
		rep.Failures = append(rep.Failures,
			fmt.Sprintf("família %q está declarada em %s e não tem representante na sonda; sem um caminho concreto ela nunca é medida",
				f, *inventory))
		if !*asJSON {
			fmt.Printf("  FAIL  %-46s sem representante na sonda\n", f)
		}
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
	} else {
		fmt.Printf("\n  PASS=%d  FAIL=%d  SKIP=%d\n", rep.Pass, rep.Fail, rep.Skip)
	}

	if rep.Fail > 0 {
		fmt.Fprintln(os.Stderr, "\nA borda deixou passar algo sem credencial:")
		for _, f := range rep.Failures {
			fmt.Fprintf(os.Stderr, "  - %s\n", f)
		}
		return 1
	}
	if !*asJSON {
		fmt.Println("  A borda fecha em tudo que exige credencial, e abre no que a especificação exige.")
	}
	return 0
}

// probeDiscovery lê o documento de descoberta e devolve os campos que anunciam um host
// inalcançável de fora, ou texto claro. Falha de leitura NÃO vira vazamento — a sonda de
// fecho acima já cobre a indisponibilidade, e inventar um achado a partir de uma rede
// ruim seria pior que não medir.
func probeDiscovery(ctx context.Context, cli *http.Client, host string) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://"+host+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil
	}
	return leak.DiscoveryLeak(doc)
}

// probePlaceholders lê a configuração do Kratos no cluster e devolve os campos que
// ficaram com marcador de substituição.
//
// Sem kubectl alcançável não há achado — e isso é deliberado: esta sonda mede a borda
// PÚBLICA, e exigir acesso ao cluster para ela rodar a tornaria inútil de fora. Quem
// roda com acesso ganha a verificação de graça.
func probePlaceholders() []string {
	out, err := exec.Command("kubectl", "--context", "k3d-loopzero-core",
		"-n", "loopzero-auth", "get", "cm", "ory-kratos-config", "-o", "yaml").Output()
	if err != nil {
		return nil
	}
	return leak.PlaceholderLeak(string(out))
}

// runLeakPorts audita o que escuta no host contra `infra/quality/host-ports.txt`.
//
// FECHADO POR PADRÃO: porta que escuta em 0.0.0.0 e não está no registro REPROVA. O
// firewall anterior tinha a polaridade oposta — abria por padrão, fechava por lista — e a
// medição de 2026-08-10 encontrou 17 portas alcançáveis da internet fora de qualquer
// lista, entre elas o ArgoCD e a identidade sem TLS.
func runLeakPorts(args []string) int {
	fs := flag.NewFlagSet("leak ports", flag.ContinueOnError)
	registro := fs.String("registry", "infra/quality/host-ports.txt", "registro declarado")
	asJSON := fs.Bool("json", false, "saída legível por máquina")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	decls, err := leak.ParsePorts(*registro)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	escutando, err := portasEscutando()
	if err != nil {
		fmt.Fprintf(os.Stderr, "não foi possível ler as portas em escuta: %v\n", err)
		return 1
	}

	naoDecl, obsoletas := leak.AuditPorts(escutando, decls)
	fechar := leak.PortsToClose(escutando, decls)

	if *asJSON {
		rep := map[string]any{
			"escutando": len(escutando), "declaradas": len(decls),
			"nao_declaradas": naoDecl, "obsoletas": obsoletas, "a_fechar": fechar,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return 1
		}
	} else {
		fmt.Printf("  escutando em 0.0.0.0: %d   declaradas: %d\n", len(escutando), len(decls))
		for _, f := range naoDecl {
			fmt.Printf("  FAIL  porta %-6d %s\n", f.Port, f.Why)
		}
		for _, f := range obsoletas {
			fmt.Printf("  WARN  porta %-6d %s\n", f.Port, f.Why)
		}
		fmt.Printf("\n  a fechar (classe não admite alcance externo): %d\n", len(fechar))
		for _, d := range fechar {
			fmt.Printf("    %-6d %-8s %s\n", d.Port, d.Class, d.Reason)
		}
	}

	if len(naoDecl) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d porta(s) escutam sem estar no registro. O registro é fechado por "+
			"padrão: declare cada uma com a classe e o consumidor, ou feche.\n", len(naoDecl))
		return 1
	}
	return 0
}

// portasEscutando lê as portas TCP em escuta em endereço de todas as interfaces.
func portasEscutando() ([]int, error) {
	out, err := exec.Command("ss", "-ltn").Output()
	if err != nil {
		return nil, err
	}
	vistas := map[int]bool{}
	for _, linha := range strings.Split(string(out), "\n") {
		campos := strings.Fields(linha)
		if len(campos) < 4 {
			continue
		}
		addr := campos[3]
		if !strings.HasPrefix(addr, "0.0.0.0:") && !strings.HasPrefix(addr, "[::]:") {
			continue
		}
		i := strings.LastIndex(addr, ":")
		p, err := strconv.Atoi(addr[i+1:])
		if err != nil {
			continue
		}
		vistas[p] = true
	}
	var portas []int
	for p := range vistas {
		portas = append(portas, p)
	}
	sort.Ints(portas)
	return portas, nil
}
