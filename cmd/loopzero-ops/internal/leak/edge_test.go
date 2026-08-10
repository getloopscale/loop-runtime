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
