// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestClosedRejeita404 é a asserção que dá valor à sonda inteira.
//
// Aceitar 404 como fecho tornaria a sonda inútil no caso exato que ela existe para pegar:
// uma rota quebrada responde 404 e passaria como "protegida". Foi assim que
// `/credentials` passou meses em 404 enquanto o inventário o declarava vivo — esconder
// não é negar.
func TestClosedRejeita404(t *testing.T) {
	casos := []struct {
		status int
		fecha  bool
		por    string
	}{
		{http.StatusUnauthorized, true, "nega explicitamente"},
		{http.StatusForbidden, true, "nega explicitamente"},
		{http.StatusFound, true, "leva ao fluxo de login"},
		{http.StatusSeeOther, true, "leva ao fluxo de login"},
		{http.StatusNotFound, false, "esconder não é negar, e mascara rota quebrada"},
		{http.StatusOK, false, "entregou o dado sem credencial"},
		{http.StatusInternalServerError, false, "falhou, mas não negou"},
		{http.StatusBadGateway, false, "upstream fora; não é prova de fecho"},
	}
	for _, c := range casos {
		if got := closed(c.status); got != c.fecha {
			t.Errorf("closed(%d) = %v, queria %v — %s", c.status, got, c.fecha, c.por)
		}
	}
}

func escreveInventário(t *testing.T, conteúdo string) string {
	t.Helper()
	dir := t.TempDir()
	caminho := filepath.Join(dir, "proxy-exposure.txt")
	if err := os.WriteFile(caminho, []byte(conteúdo), 0o600); err != nil {
		t.Fatalf("escrever inventário: %v", err)
	}
	return caminho
}

// TestTargetsDerivaDoInventário prova que a cobertura acompanha a superfície.
//
// Se os alvos viessem de uma lista própria, ela envelheceria sozinha: a rota adicionada
// ao Caddyfile e declarada no inventário (porque o gate obriga) seria justamente a não
// testada.
func TestTargetsDerivaDoInventário(t *testing.T) {
	inv := escreveInventário(t, `# comentário é ignorado
app-dev.loopzero.example /engine/* | reviewed | motor via BFF
app-dev.loopzero.example /v1/* | reviewed | BFF
outro-host.loopzero.example /v1/* | reviewed | host diferente, não deve entrar
`)
	alvos, sem, err := Targets(inv, "app-dev.loopzero.example")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(sem) != 0 {
		t.Errorf("nenhuma família deveria ficar sem representante, veio %v", sem)
	}
	if len(alvos) != 2 {
		t.Fatalf("queria 2 alvos, veio %d: %+v", len(alvos), alvos)
	}
	for _, a := range alvos {
		if a.Host != "app-dev.loopzero.example" {
			t.Errorf("alvo de outro host vazou: %+v", a)
		}
		if strings.Contains(a.Path, "*") {
			t.Errorf("alvo tem curinga e não é requisitável: %q", a.Path)
		}
	}
}

// TestTargetsReportaFamíliaSemRepresentante trava o buraco de cobertura.
//
// Uma família nova declarada no inventário e sem caminho concreto aqui não pode sumir em
// silêncio — silêncio vira um PASS que não mediu nada, que é pior que uma falha.
func TestTargetsReportaFamíliaSemRepresentante(t *testing.T) {
	inv := escreveInventário(t, "app-dev.loopzero.example /v1/faturamento/* | reviewed | superfície nova\n")
	alvos, sem, err := Targets(inv, "app-dev.loopzero.example")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(alvos) != 0 {
		t.Errorf("família sem representante não deveria virar alvo: %+v", alvos)
	}
	if len(sem) != 1 || sem[0] != "/v1/faturamento/*" {
		t.Fatalf("queria a família reportada como sem representante, veio %v", sem)
	}
}

// TestTargetsNãoConfundeIsentaComEsquecida — isentar é decisão, esquecer é defeito.
func TestTargetsNãoConfundeIsentaComEsquecida(t *testing.T) {
	inv := escreveInventário(t, `app-dev.loopzero.example /self-service/* | reviewed | fluxo de navegador
app-dev.loopzero.example @auth_bff | reviewed | matcher nomeado
app-dev.loopzero.example /api/v1/webhooks/github | internal | HMAC
`)
	alvos, sem, err := Targets(inv, "app-dev.loopzero.example")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(alvos) != 0 || len(sem) != 0 {
		t.Errorf("famílias isentas não geram alvo nem buraco; veio alvos=%+v sem=%v", alvos, sem)
	}
}

// TestProbeExigeQueAPúblicaResponda — a asserção invertida.
//
// Um 401 na descoberta OIDC quebra o login em vez de protegê-lo: clientes leem esse
// documento antes de possuir qualquer token.
func TestProbeExigeQueAPúblicaResponda(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cli := srv.Client()
	cli.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	host := strings.TrimPrefix(srv.URL, "https://")
	v := Probe(context.Background(), cli, Target{Host: host, Path: "/.well-known/openid-configuration", PublicBySpec: true})
	if v.Passed {
		t.Error("uma rota pública por especificação devolvendo 401 tem de REPROVAR — ela quebra clientes OIDC")
	}
	if !strings.Contains(v.Reason, "especificação") {
		t.Errorf("a razão deveria explicar o contrato OIDC, veio %q", v.Reason)
	}
}

// TestProbeReprovaVazamento — o caso que a sonda existe para pegar.
func TestProbeReprovaVazamento(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"projects":[{"id":"tn-1"}]}`))
	}))
	defer srv.Close()

	cli := srv.Client()
	cli.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	host := strings.TrimPrefix(srv.URL, "https://")
	v := Probe(context.Background(), cli, Target{Host: host, Path: "/engine/api/v1/projects"})
	if v.Passed {
		t.Fatal("200 sem credencial tem de REPROVAR — é exatamente o vazamento que a sonda procura")
	}
	if v.Status != http.StatusOK {
		t.Errorf("status observado = %d, queria 200", v.Status)
	}
}

// TestProbeMarcaInalcançávelComoSkip — alvo fora do ar não é prova de fecho.
//
// Contar um erro de conexão como PASS transformaria "o ambiente caiu" em "a borda está
// segura", que é a inversão mais perigosa que uma sonda pode fazer.
func TestProbeMarcaInalcançávelComoSkip(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	host := strings.TrimPrefix(srv.URL, "https://")
	cli := srv.Client()
	srv.Close() // derruba antes de sondar

	v := Probe(context.Background(), cli, Target{Host: host, Path: "/v1/qualquer"})
	if v.Passed {
		t.Error("alvo inalcançável NÃO pode passar — seria trocar 'o ambiente caiu' por 'a borda está segura'")
	}
	if !v.Skipped {
		t.Error("alvo inalcançável tem de ser marcado como pulado, para o relatório distinguir de uma reprovação")
	}
}

// TestDiscoveryLeakPegaHostInternoETextoClaro — os dois modos medidos em 2026-08-10.
func TestDiscoveryLeakPegaHostInternoETextoClaro(t *testing.T) {
	doc := map[string]any{
		"issuer":                 "http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444/",
		"jwks_uri":               "http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444/.well-known/jwks.json",
		"authorization_endpoint": "https://auth-dev.loopzero.example/oauth2/auth",
		"scopes_supported":       []any{"openid"},
	}
	got := DiscoveryLeak(doc)
	if len(got) != 2 {
		t.Fatalf("queria 2 vazamentos (issuer + jwks_uri), veio %d: %v", len(got), got)
	}
	for _, g := range got {
		if !strings.Contains(g, "interno") {
			t.Errorf("o vazamento devia nomear o host interno: %q", g)
		}
	}
}

// TestDiscoveryLeakAceitaDocumentoCorreto — sem falso positivo em HTTPS público.
func TestDiscoveryLeakAceitaDocumentoCorreto(t *testing.T) {
	doc := map[string]any{
		"issuer":                 "https://auth-dev.loopzero.example/",
		"jwks_uri":               "https://auth-dev.loopzero.example/.well-known/jwks.json",
		"authorization_endpoint": "https://auth-dev.loopzero.example/oauth2/auth",
	}
	if got := DiscoveryLeak(doc); len(got) != 0 {
		t.Errorf("documento correto não pode gerar vazamento, veio %v", got)
	}
}

// TestNenhumaTaskFixaOAmbienteHelmfile trava a causa raiz medida em 2026-08-10.
//
// `LOOPZERO_HELMFILE_ENV=dev-public task cloud:setup` sincronizava o Ory com
// `-e standalone-multi` FIXO, ignorando a variável. O efeito era invisível e caro: o
// ambiente `standalone-multi` não declara `publicAuthUrl`, então o chart do Hydra caía no
// FQDN de cluster e o documento de descoberta OIDC passava a anunciar
// `http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444` em NOVE campos — incluindo o
// `issuer`, que é o valor gravado no token e comparado por todo verificador.
//
// A task não falhava; ela sincronizava o ambiente errado com sucesso. Este guarda existe
// porque nada mais distinguia "instalou dev-public" de "instalou standalone-multi
// enquanto você pedia dev-public".
func TestNenhumaTaskFixaOAmbienteHelmfile(t *testing.T) {
	raiz := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(raiz, "taskfiles", "infra.yml"))
	if err != nil {
		t.Fatalf("ler taskfiles/infra.yml: %v", err)
	}
	var ofensoras []string
	for i, linha := range strings.Split(string(b), "\n") {
		if strings.Contains(linha, "-e standalone-multi") {
			ofensoras = append(ofensoras, fmt.Sprintf("infra.yml:%d", i+1))
		}
	}
	if len(ofensoras) > 0 {
		t.Errorf("%d task(s) fixam `-e standalone-multi` e ignoram LOOPZERO_HELMFILE_ENV: %s\n"+
			"Use `${LOOPZERO_HELMFILE_ENV:-standalone-multi}` como as demais. Fixar o ambiente faz a task "+
			"instalar o ambiente ERRADO com sucesso — foi assim que o issuer do OIDC virou um host "+
			"de cluster em 9 campos do documento de descoberta.",
			len(ofensoras), strings.Join(ofensoras, ", "))
	}
}

// repoRootFromTest sobe até achar go.work, que só existe na raiz do repositório.
func repoRootFromTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("raiz do repositório não encontrada (go.work ausente)")
	return ""
}

// TestProbeAceitaRedirecionamentoNaRotaPública trava o conserto da raiz.
//
// A raiz do host de identidade responde 302 para o login — de propósito, porque quem a
// alcança é uma pessoa e o Kratos não serve página ali. Exigir 200 nas rotas públicas
// reprovaria justamente o conserto: 200 serve o conteúdo, 302 leva a ele, e os dois
// cumprem "esta rota leva a algum lugar". O que reprova é 404, que é não ter destino.
func TestProbeAceitaRedirecionamentoNaRotaPública(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "https://app-dev.loopzero.example/login", http.StatusFound)
	}))
	defer srv.Close()

	cli := srv.Client()
	cli.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	host := strings.TrimPrefix(srv.URL, "https://")
	v := Probe(context.Background(), cli, Target{Host: host, Path: "/", PublicBySpec: true,
		PorQue: "quem digita o endereço é uma pessoa e precisa de um caminho para o login"})
	if !v.Passed {
		t.Errorf("302 para o login é destino válido numa rota pública; reprovar aqui invalidaria o "+
			"conserto da raiz. Razão dada: %q", v.Reason)
	}
}

// TestProbeReprova404NaRotaPública — o beco sem saída continua reprovando.
func TestProbeReprova404NaRotaPública(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "404 page not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cli := srv.Client()
	cli.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	host := strings.TrimPrefix(srv.URL, "https://")
	v := Probe(context.Background(), cli, Target{Host: host, Path: "/", PublicBySpec: true,
		PorQue: "quem digita o endereço é uma pessoa e precisa de um caminho para o login"})
	if v.Passed {
		t.Error("404 numa rota pública é beco sem saída e tem de REPROVAR")
	}
	if !strings.Contains(v.Reason, "pessoa") {
		t.Errorf("a razão devia dizer QUEM depende da rota, veio %q", v.Reason)
	}
}

// TestPlaceholderLeakPegaCredencialNaoSubstituida — o defeito que quebrou o login.
func TestPlaceholderLeakPegaCredencialNaoSubstituida(t *testing.T) {
	cfg := `
      providers:
        - id: google
          client_id: "REPLACE_VIA_PULUMI"
          client_secret: "REPLACE_VIA_PULUMI"
        - id: github
          client_id: "Ov23li3uhReal"
`
	got := PlaceholderLeak(cfg)
	if len(got) != 2 {
		t.Fatalf("queria 2 achados (client_id + client_secret), veio %d: %v", len(got), got)
	}
	for _, g := range got {
		if !strings.Contains(g, "marcador") {
			t.Errorf("o achado devia nomear o marcador: %q", g)
		}
	}
}

// TestPlaceholderLeakAceitaCredencialReal — sem falso positivo.
func TestPlaceholderLeakAceitaCredencialReal(t *testing.T) {
	cfg := `client_id: "46285214407-8mrcsu.apps.googleusercontent.com"
client_secret: "GOCSPX-real"`
	if got := PlaceholderLeak(cfg); len(got) != 0 {
		t.Errorf("credencial real não pode gerar achado, veio %v", got)
	}
}

// TestInstalarCelulaNaoFixaUmaCelula trava a divergência entre células.
//
// MEDIDO em 2026-08-10: a `cell-0` roda 8 componentes de plataforma e a `cell-1` roda 4 —
// faltam KEDA (escala por demanda), static-assets, cloudflared, policy-controller e
// kube-state-metrics. As duas tasks de bootstrap instalam o MESMO conjunto de 5 releases,
// então a divergência não vem dali: vem de `infra:install:cell`, que sincroniza o
// helmfile completo (28 releases) com `--kube-context {{.CELL_0_CONTEXT}}` FIXO.
//
// Duas consequências, e a segunda é pior:
//
//  1. Um tenant fixado na cell-1 recebe MENOS plataforma que um na cell-0, e ninguém
//     escolhe a célula — o roteamento é por bin-packing. A célula é um detalhe de
//     alocação, então duas células com capacidades diferentes tornam o comportamento do
//     produto dependente de onde o tenant caiu.
//  2. `task clusters:recreate -- cell-1` chama esta mesma task (k3d.yml:149). Recriar a
//     cell-1 instalaria na cell-0 — no cluster errado, sem erro.
//
// O guarda é textual de propósito: ele reprova o contexto FIXO, não a divergência
// observada. A divergência é o sintoma; o contexto fixo é a causa, e é o que volta.
func TestInstalarCelulaNaoFixaUmaCelula(t *testing.T) {
	raiz := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(raiz, "taskfiles", "infra.yml"))
	if err != nil {
		t.Fatalf("ler taskfiles/infra.yml: %v", err)
	}

	linhas := strings.Split(string(b), "\n")
	inicio := -1
	for i, l := range linhas {
		if strings.HasPrefix(l, "  infra:install:cell:") {
			inicio = i
			break
		}
	}
	if inicio < 0 {
		t.Fatal("task infra:install:cell não encontrada — o guarda perdeu o alvo e precisa ser revisto")
	}

	var ofensoras []string
	for i := inicio + 1; i < len(linhas); i++ {
		// a próxima task de topo encerra o bloco
		if strings.HasPrefix(linhas[i], "  ") && !strings.HasPrefix(linhas[i], "   ") &&
			strings.HasSuffix(strings.TrimSpace(linhas[i]), ":") {
			break
		}
		// Comentário não é código: a própria explicação do conserto cita o nome antigo, e
		// contá-la reprovaria o texto que documenta a correção. O guarda mira a diretiva.
		if strings.HasPrefix(strings.TrimSpace(linhas[i]), "#") {
			continue
		}
		if strings.Contains(linhas[i], "CELL_0_CONTEXT") {
			ofensoras = append(ofensoras, fmt.Sprintf("infra.yml:%d", i+1))
		}
	}
	if len(ofensoras) > 0 {
		t.Errorf("`infra:install:cell` fixa a cell-0 em %d linha(s): %s\n"+
			"A task instala a plataforma de UMA célula, então o contexto tem de ser parâmetro. "+
			"Fixo, ela deixa a cell-1 com menos plataforma que a cell-0 — e faz "+
			"`clusters:recreate -- cell-1` instalar no cluster errado.",
			len(ofensoras), strings.Join(ofensoras, ", "))
	}
}

// TestTodaCelulaApareceEmTodaListaQueAsEnumera trava a lacuna que sobrou depois do B-026.
//
// Consertar `infra:install:cell` fez as duas células existentes ficarem iguais — mas o
// conserto foi de VALOR, não de ESTRUTURA. As listas de células seguem escritas à mão em
// três lugares (`clusters:delete`, `infra:install`, `k3d:bootstrap-cells`), e uma terceira
// célula adicionada a `infra/k3d/` entraria em algumas e não em outras. O modo de falha
// seria o mesmo do B-026 — uma célula com menos plataforma que as outras, sem erro —, e
// desta vez o guarda de contexto-fixo não pegaria, porque o contexto já é parâmetro.
//
// A fonte de verdade é `infra/k3d/cell-*-cluster.yaml`: cada arquivo é uma célula que
// existe. Este guarda compara essa fonte com cada lista que enumera células.
//
// Medido em 2026-08-10: as três listas concordam com a fonte, mas por coincidência —
// nada as amarrava.
func TestTodaCelulaApareceEmTodaListaQueAsEnumera(t *testing.T) {
	raiz := repoRootFromTest(t)

	// Fonte de verdade: um arquivo de cluster por célula.
	arquivos, err := filepath.Glob(filepath.Join(raiz, "infra", "k3d", "cell-*-cluster.yaml"))
	if err != nil || len(arquivos) == 0 {
		t.Fatalf("nenhum infra/k3d/cell-*-cluster.yaml encontrado — o guarda perdeu a fonte de verdade: %v", err)
	}
	var células []string
	for _, a := range arquivos {
		base := filepath.Base(a)
		células = append(células, strings.TrimSuffix(base, "-cluster.yaml")) // cell-0, cell-1, …
	}
	sort.Strings(células)

	listas := []struct {
		arquivo string
		task    string
		porque  string
	}{
		{"taskfiles/k3d.yml", "clusters:delete", "uma célula fora daqui sobrevive ao `clean:all` e o próximo cleanroom começa sujo"},
		{"taskfiles/infra.yml", "infra:install", "uma célula fora daqui não recebe a plataforma — é o defeito do B-026 de volta"},
		{"Taskfile.yml", "k3d:bootstrap-cells", "uma célula fora daqui fica sem credencial de acesso"},
	}

	for _, l := range listas {
		t.Run(l.task, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(raiz, l.arquivo))
			if err != nil {
				t.Fatalf("ler %s: %v", l.arquivo, err)
			}
			linhas := strings.Split(string(b), "\n")

			inicio := -1
			for i, ln := range linhas {
				if strings.TrimSpace(ln) == strings.TrimSpace(l.task)+":" || strings.TrimSpace(ln) == strings.TrimSpace(l.task) {
					inicio = i
					break
				}
			}
			if inicio < 0 {
				t.Fatalf("task %q não encontrada em %s — o guarda perdeu o alvo", l.task, l.arquivo)
			}

			var bloco strings.Builder
			for i := inicio; i < len(linhas) && i < inicio+40; i++ {
				if i > inicio && strings.HasPrefix(linhas[i], "  ") && !strings.HasPrefix(linhas[i], "   ") &&
					strings.HasSuffix(strings.TrimSpace(linhas[i]), ":") {
					break
				}
				bloco.WriteString(linhas[i] + "\n")
			}
			texto := bloco.String()

			for _, c := range células {
				// aceita `cell-0`, `loopzero-cell-0`, `CELL_0_CONTEXT` — formas diferentes, mesma célula
				envVar := "CELL_" + strings.TrimPrefix(c, "cell-") + "_"
				if strings.Contains(texto, c) || strings.Contains(texto, envVar) {
					continue
				}
				t.Errorf("a célula %q existe (infra/k3d/%s-cluster.yaml) e não aparece em `%s`.\n%s",
					c, c, l.task, l.porque)
			}
		})
	}
}

// TestTodoRepoImportadoTemDominio fecha a lacuna que deixou o loopzero-forge invisível.
//
// A tabela de domínios em `.claude/rules/cycle-backlog.md` lista repositórios, e nada a
// compara com o que o código realmente importa. Foi assim que `loopzero-forge` passou: 18
// arquivos do engine dependem dele — incluindo o construtor de workflows, o coração do
// caminho de build — e ele não aparecia na tabela, não tinha especialista e não tinha
// checkout.
//
// A consequência não é estética. `route_domain.py` recusa um repositório sem domínio, e o
// gate G1 do intake bloqueia o item: um achado no forge não teria para onde ser roteado.
// O ecossistema simplesmente não alcançaria uma dependência central.
//
// A fonte de verdade aqui é o `go.mod`: o que o código importa é o que existe. Uma tabela
// escrita à mão envelhece; uma derivada do que se importa acompanha por construção.
func TestTodoRepoImportadoTemDominio(t *testing.T) {
	raiz := repoRootFromTest(t)

	tabela, err := os.ReadFile(filepath.Join(raiz, ".claude", "rules", "cycle-backlog.md"))
	if err != nil {
		t.Fatalf("ler a tabela de domínios: %v", err)
	}
	texto := string(tabela)

	// Todo `loopzero-*` importado por qualquer go.mod deste repositório.
	importados := map[string]bool{}
	err = filepath.WalkDir(raiz, func(caminho string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "go.mod" {
			return nil
		}
		if strings.Contains(caminho, "/vendor/") || strings.Contains(caminho, "/testdata/") {
			return nil
		}
		b, rerr := os.ReadFile(caminho) // #nosec G304 -- caminho vem do walk sobre o próprio repo
		if rerr != nil {
			return nil
		}
		for _, m := range regexp.MustCompile(`loopzero[a-z]*/(loopzero-[a-z-]+)`).FindAllStringSubmatch(string(b), -1) {
			importados[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("varrer go.mod: %v", err)
	}
	if len(importados) == 0 {
		t.Fatal("nenhum repositório irmão importado — o guarda perdeu o alvo e precisa ser revisto")
	}

	var semDominio []string
	for repo := range importados {
		// A tabela cita o repositório entre crases, na coluna de repositórios.
		if strings.Contains(texto, "`"+repo+"`") {
			continue
		}
		semDominio = append(semDominio, repo)
	}
	sort.Strings(semDominio)

	if len(semDominio) > 0 {
		t.Errorf("%d repositório(s) importado(s) sem domínio na tabela: %s\n"+
			"Um repositório sem domínio não tem especialista e não pode receber um achado: "+
			"route_domain.py o recusa e o gate G1 do intake bloqueia o item. Declare-o em "+
			"`.claude/rules/cycle-backlog.md`, ou declare-o explicitamente como dependência de "+
			"terceiros — as duas respostas servem, o silêncio não.",
			len(semDominio), strings.Join(semDominio, ", "))
	}
}

// TestRotaDePortalExigeOMesmoPapelDosDoisLados trava a assimetria do B-031.
//
// MEDIDO em 2026-08-11: a MESMA capacidade — abrir a sessão do portal de cobrança do
// Stripe — exige papel diferente em cada implementação. O `loopzero-cloud` monta
// `RequireRole(tenantProvisioner, "owner")`; o engine monta a rota com rate-limit e mais
// nada, embora TENHA o middleware (hierarquia idêntica, `owner: 3`) e o use nas rotas
// vizinhas — `tenants/bulk-delete` aplica `RequireAdmin`.
//
// Não é vazamento: a rota está autenticada. É escalonamento horizontal — qualquer membro
// de um time abre o portal de cobrança dele, onde se vê fatura, meio de pagamento e
// histórico.
//
// POR QUE ESTE GUARDA LÊ A MONTAGEM, e não exercita um handler. A primeira versão que
// escrevi montava `RequireRole("owner")(h.Portal)` DENTRO do teste e verificava o 403 —
// e passava, com a rota real seguindo aberta. Provava que o middleware funciona, que
// ninguém duvidava, e nada sobre a rota. Um teste que constrói a própria proteção não
// testa a ausência dela; a autorização vive na montagem da rota, então é a montagem que
// precisa ser lida.
func TestRotaDePortalExigeOMesmoPapelDosDoisLados(t *testing.T) {
	raiz := repoRootFromTest(t)
	b, err := os.ReadFile(filepath.Join(raiz, "api", "internal", "server", "router.go"))
	if err != nil {
		t.Fatalf("ler router.go: %v", err)
	}

	var linhaRota string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, `"/api/v1/billing/portal"`) && !strings.Contains(strings.TrimSpace(l), "//") {
			linhaRota = l
			break
		}
	}
	if linhaRota == "" {
		t.Fatal("rota /api/v1/billing/portal não encontrada em router.go — o guarda perdeu o alvo")
	}
	if !strings.Contains(linhaRota, "RequireRole") && !strings.Contains(linhaRota, "RequireOwner") {
		t.Errorf("a rota do portal de cobrança não exige papel:\n  %s\n"+
			"O loopzero-cloud exige `owner` para a MESMA capacidade (cmd/main.go:1572). Sem isso, "+
			"qualquer membro autenticado do time abre o portal de cobrança dele — fatura, meio de "+
			"pagamento e histórico. As duas hierarquias são idênticas (`owner: 3`), então alinhar é "+
			"aplicar o middleware que já existe em middleware/roles.go.", strings.TrimSpace(linhaRota))
	}
}

// TestSoUmComponenteFalaComOStripe fecha o B-029.
//
// A decisão (ADR 2026-08-11) é que o `loopzero-cloud` é o único emissor de sessão do portal.
// Enquanto o engine mantiver a biblioteca do Stripe importada e o injetor de emissor, a
// capacidade continua lá — basta alguém semear o Secret para ela voltar a ser usada, e a
// duplicação que o B-029 registra reaparece sem código novo.
//
// O guarda mira a CAPACIDADE e não a configuração: um Secret ausente é estado de
// ambiente, e ambientes mudam. O que não pode voltar sem decisão é o engine saber falar
// com o Stripe.
func TestSoUmComponenteFalaComOStripe(t *testing.T) {
	raiz := repoRootFromTest(t)

	var ofensores []string
	err := filepath.WalkDir(filepath.Join(raiz, "api"), func(caminho string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(caminho, ".go") ||
			strings.HasSuffix(caminho, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(caminho) // #nosec G304 -- walk sobre o próprio repo
		if rerr != nil {
			return nil
		}
		if strings.Contains(string(b), "stripe/stripe-go") {
			rel, _ := filepath.Rel(raiz, caminho)
			ofensores = append(ofensores, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("varrer api/: %v", err)
	}
	sort.Strings(ofensores)

	if len(ofensores) > 0 {
		t.Errorf("o engine importa a biblioteca do Stripe em %d arquivo(s): %s\n"+
			"Por ADR 2026-08-11 o loopzero-cloud é o único emissor de sessão de portal. Enquanto a "+
			"biblioteca estiver aqui, a capacidade continua no engine e basta semear o Secret para a "+
			"duplicação voltar — sem código novo, sem revisão, sem decisão.",
			len(ofensores), strings.Join(ofensores, ", "))
	}
}
