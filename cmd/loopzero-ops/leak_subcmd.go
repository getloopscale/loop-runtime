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
	// Upstream diz QUAL superficie caiu quando ha falha de gateway (B-022). Vazio em dia
	// normal — um campo que fala sempre vira ruido e para de ser lido.
	Upstream string `json:"upstream,omitempty"`
	Pass     int    `json:"pass"`
	Fail     int    `json:"fail"`
	Skip     int    `json:"skip"`
	// Uncovered conta o que a sonda NAO mede, separado do que ela mediu e reprovou.
	//
	// Somar os dois no `fail` impedia o operador de distinguir "a borda deixou passar algo"
	// de "esta superficie nao e medida" — e as duas exigem acoes diferentes. Medido na
	// revisao de B-025: a maioria das falhas era buraco de cobertura, e misturadas com
	// vazamento elas fariam a sonda parar de ser lida.
	Uncovered   int      `json:"uncovered"`
	Failures    []string `json:"failures"`
	NotMeasured []string `json:"not_measured"`
	// Wildcards nomeia o que foi pulado por nao ter hostname concreto. Antes ficava so no
	// modo texto, e o JSON — que e o contrato legivel por maquina — dizia `skip: 0` com
	// curingas pulados (achado M-5 da revisao).
	Wildcards []string `json:"wildcards,omitempty"`
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

	// B-025: sonda TODO host que o inventario declara, nao apenas o do `--host`.
	//
	// Medido 2026-08-18: o inventario declarava 7 hosts e a sonda alcancava 2. Os cinco
	// restantes eram inalcancaveis por construcao — `Targets` filtra por um unico host —, e
	// dois deles serviam servico VIVO em hostname de producao: `cloud.loopzero.example`
	// devolvia 401 em `/v1/billing` e `auth.loopzero.example` devolvia 200 em `/health/ready`.
	//
	// `--host` continua valendo, agora como filtro para uma execucao focada.
	// `fs.Visit` so visita flag REALMENTE passada. Comparar com `DefValue` fazia
	// `--host app-dev.loopzero.example` — o valor que o `--help` anuncia como padrao, e portanto
	// o que o operador digita para focar — cair na varredura completa (achado HIGH-2 da
	// revisao de B-025).
	hostPassado := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "host" {
			hostPassado = true
		}
	})

	var hostsAlvo []string
	if hostPassado {
		// `--host ""` sondaria zero superficie de aplicacao e sairia 0: verde vazio, que e
		// a forma que esta sonda existe para recusar (achado M-6 da revisao de B-025).
		if strings.TrimSpace(*host) == "" {
			fmt.Fprintln(os.Stderr, "--host vazio: nao ha superficie a sondar; omita a flag para varrer o inventario")
			return 1
		}
		hostsAlvo = []string{*host}
	} else {
		hs, err := leak.Hosts(*inventory)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		hostsAlvo = hs
	}

	var alvos []leak.Target
	var semRepresentante []string
	var curingas []string
	for _, h := range hostsAlvo {
		// Um curinga nao tem hostname concreto para resolver; sondar `*.x` produziria um
		// erro de DNS que se pareceria com host fora do ar. Fica dito, nao escondido.
		if strings.HasPrefix(h, "*.") {
			curingas = append(curingas, h)
			continue
		}
		a, semRep, err := leak.Targets(*inventory, h)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		alvos = append(alvos, a...)
		semRepresentante = append(semRepresentante, semRep...)
	}
	// A identidade tambem segue os hosts descobertos (achado HIGH-1 da revisao de B-025).
	//
	// `auth.loopzero.example` servia Kratos VIVO num hostname de producao, e a varredura sabia
	// que ele existia sem aplicar nele nenhuma assercao de identidade. Rodando com
	// `--auth-host auth.loopzero.example` a revisao mediu tres defeitos concretos: descoberta
	// OIDC 404, raiz 404 e `/userinfo` 404 — invisiveis enquanto o host aparecia so como
	// "buraco nao medido".
	hostsIdent := map[string]bool{*authHost: true}
	for _, h := range hostsAlvo {
		if strings.HasPrefix(h, "auth") {
			hostsIdent[h] = true
		}
	}
	identOrdenados := make([]string, 0, len(hostsIdent))
	for h := range hostsIdent {
		identOrdenados = append(identOrdenados, h)
	}
	sort.Strings(identOrdenados)
	for _, h := range identOrdenados {
		alvos = append(alvos, leak.IdentityTargets(h)...)
	}

	// Retira dos buracos o que a sonda de identidade acabou de cobrir: contabilizar como
	// descoberto o que foi sondado e o relatorio discordando de si mesmo (HIGH-3).
	semRepresentante = leak.SemCoberturaDeIdentidade(semRepresentante, alvos)

	cli := leak.DefaultClient()
	ctx := context.Background()
	rep := leakReport{Failures: []string{}, NotMeasured: []string{}}

	if len(curingas) > 0 {
		rep.Wildcards = curingas
		rep.Skip += len(curingas)
		if !*asJSON {
			fmt.Printf("  SKIP  %-46s curinga sem hostname concreto para resolver\n",
				strings.Join(curingas, ", "))
		}
	}

	// Guarda os codigos das duas superficies para separar QUEM caiu (B-022). O caminho do
	// motor atravessa o painel, entao um 502 em `/engine/*` sozinho nao diz se o motor
	// caiu ou se o painel levou os dois junto — e as duas situacoes pedem acoes diferentes.
	var engineStatus, dashboardStatus int

	for _, alvo := range alvos {
		v := leak.Probe(ctx, cli, alvo)
		switch {
		case strings.HasPrefix(alvo.Path, "/engine/"):
			engineStatus = v.Status
		case strings.HasPrefix(alvo.Path, "/v1/"):
			// Rota de produto do painel: nao atravessa o motor (medido em B-002). O
			// primeiro que aparecer basta — o que importa e se o painel responde.
			if dashboardStatus == 0 {
				dashboardStatus = v.Status
			}
		}
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

	// B-022: com as duas superficies sondadas, o relatorio passa a dizer QUAL caiu em vez
	// de deixar o operador com dois 502 iguais. So fala quando ha algo a separar — em dia
	// normal isto e silencio.
	if engineStatus != 0 && dashboardStatus != 0 {
		if st := leak.ClassifyUpstream(engineStatus, dashboardStatus); st != leak.UpstreamBothUp {
			rep.Upstream = st.Explicacao()
			if !*asJSON {
				fmt.Printf("  UPSTREAM  %s\n", rep.Upstream)
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

	// Uma família declarada e não medida é buraco de cobertura, e continua contando para o
	// código de saída: o modo de falha que esta sonda existe para impedir é justamente a
	// rota nova que ninguém sonda, e um aviso silencioso reproduziria isso.
	//
	// O que mudou (revisão de B-025): ela conta num campo PRÓPRIO. Somada ao `fail`, ela
	// impedia o operador de distinguir "a borda deixou passar algo" de "esta superfície não
	// é medida" — e as duas exigem ações diferentes. A maioria das falhas era buraco, e
	// misturadas com vazamento fariam a sonda parar de ser lida.
	for _, f := range semRepresentante {
		rep.Uncovered++
		rep.NotMeasured = append(rep.NotMeasured,
			fmt.Sprintf("família %q está declarada em %s e não tem representante na sonda; sem um caminho concreto ela nunca é medida",
				f, *inventory))
		if !*asJSON {
			fmt.Printf("  NAO-MEDIDO  %-40s sem representante na sonda\n", f)
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
		fmt.Printf("\n  PASS=%d  FAIL=%d  NAO-MEDIDO=%d  SKIP=%d\n",
			rep.Pass, rep.Fail, rep.Uncovered, rep.Skip)
	}

	if rep.Fail > 0 || rep.Uncovered > 0 {
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
