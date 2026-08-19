// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/leak"
)

// O inventario declara o que a borda expoe; a sonda tem de alcancar TUDO que ele declara.
//
// B-025, medido 2026-08-18: `app.loopzero.example`, `cloud.loopzero.example` e `auth.loopzero.example` —
// sem o sufixo `-dev` — tem bloco no Caddyfile e cinco linhas no inventario, e **nenhum
// deles jamais foi sondado**. `Targets` filtra por um unico host (`campos[0] != host`), e
// `leak live` passa `app-dev`/`auth-dev` por padrao, entao os tres eram inalcancaveis por
// construcao.
//
// E nao apontam para o vazio, como o item supunha: medido ao vivo, `cloud.loopzero.example`
// devolve 401 em `/v1/billing` (loopzero-cloud vivo negando credencial) e
// `auth.loopzero.example` devolve 200 em `/health/ready` (Kratos vivo). Sao hostnames de
// producao servindo instancias de desenvolvimento, sem medicao nenhuma.
func TestHosts_ListsEveryDeclaredHost(t *testing.T) {
	inv := escreverInventario(t, `# comentario ignorado
app-dev.loopzero.example /v1/x | reviewed | nota
cloud.loopzero.example - | reviewed | outro
app-dev.loopzero.example /v1/y | reviewed | repetido
auth.loopzero.example - | reviewed | terceiro
`)
	got, err := leak.Hosts(inv)
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	esperado := []string{"app-dev.loopzero.example", "cloud.loopzero.example", "auth.loopzero.example"}
	if len(got) != len(esperado) {
		t.Fatalf("hosts: %v, esperava %v", got, esperado)
	}
	for i := range esperado {
		if got[i] != esperado[i] {
			t.Errorf("posicao %d: %q, esperava %q (ordem de declaracao)", i, got[i], esperado[i])
		}
	}
}

// Inventario ausente e erro, nao lista vazia: sondar zero host e indistinguivel de nao
// haver o que sondar, e essa e a forma de verde que este pacote existe para recusar.
func TestHosts_MissingInventoryIsAnError(t *testing.T) {
	if _, err := leak.Hosts(filepath.Join(t.TempDir(), "nao-existe")); err == nil {
		t.Error("inventario ausente deveria ser erro")
	}
}

// A populacao real (rules/testing.md § 4.2): o inventario deste repositorio precisa
// declarar os tres hosts que ninguem sondava. Um caso sintetico prova que a funcao le;
// so isto diz o que ela encontra no que existe.
func TestHosts_RealInventoryCoversTheUnprobedThree(t *testing.T) {
	hosts, err := leak.Hosts(filepath.Join("..", "..", "..", "..", "infra", "quality", "proxy-exposure.txt"))
	if err != nil {
		t.Fatalf("inventario real: %v", err)
	}
	if len(hosts) == 0 {
		t.Fatal("zero hosts declarados — verde sobre nada")
	}
	tem := map[string]bool{}
	for _, h := range hosts {
		tem[h] = true
	}
	for _, h := range []string{"app.loopzero.example", "cloud.loopzero.example", "auth.loopzero.example"} {
		if !tem[h] {
			t.Errorf("%s nao esta no inventario — a sonda nao teria como alcanca-lo", h)
		}
	}
	t.Logf("populacao real: %d hosts declarados: %v", len(hosts), hosts)
}

func escreverInventario(t *testing.T, corpo string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "proxy-exposure.txt")
	if err := os.WriteFile(p, []byte(corpo), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Um host declarado como site inteiro (`-`) nao pode sair da sonda em SILENCIO.
//
// B-025, o fundo do defeito: `isentas` isentava o caminho `-`, que e a forma como um host
// INTEIRO se declara — a exposicao mais ampla que existe. O efeito medido em 2026-08-18:
// `cloud.loopzero.example` e `auth.loopzero.example` produziam **zero alvos e zero reclamacoes**.
// Nao apareciam como cobertos nem como buraco; simplesmente nao existiam para a sonda.
//
// E os dois estavam VIVOS: 401 em `/v1/billing` e 200 em `/health/ready`.
//
// O pacote ja separa "buraco de cobertura" de "silencio" — o campo `semRepresentante`
// existe para isso. Um site inteiro sem sonda e buraco, e passa a ser reportado como tal.
func TestTargets_WholeSiteIsReportedNotSilentlyExempt(t *testing.T) {
	inv := escreverInventario(t, `cloud.loopzero.example - | reviewed | BFF autenticado
`)
	alvos, semRep, err := leak.Targets(inv, "cloud.loopzero.example")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(alvos) == 0 && len(semRep) == 0 {
		t.Fatal("host declarado produziu zero alvos E zero reclamacoes — silencio, " +
			"que e indistinguivel de cobertura")
	}
}

// A varredura da populacao real: nenhum host declarado no inventario deste repositorio
// pode desaparecer da sonda sem deixar rastro.
func TestTargets_NoDeclaredHostVanishesSilently(t *testing.T) {
	inv := filepath.Join("..", "..", "..", "..", "infra", "quality", "proxy-exposure.txt")
	hosts, err := leak.Hosts(inv)
	if err != nil {
		t.Fatalf("inventario real: %v", err)
	}
	if len(hosts) == 0 {
		t.Fatal("zero hosts declarados — a varredura nao alcancou nada, e verde sobre nada")
	}
	var mudos []string
	for _, h := range hosts {
		if strings.HasPrefix(h, "*.") {
			continue // curinga nao tem hostname concreto; tratado no subcomando
		}
		alvos, semRep, err := leak.Targets(inv, h)
		if err != nil {
			t.Fatalf("%s: %v", h, err)
		}
		if len(alvos) == 0 && len(semRep) == 0 {
			mudos = append(mudos, h)
		}
	}
	if len(mudos) > 0 {
		t.Errorf("hosts declarados que somem da sonda sem rastro: %v", mudos)
	}
	t.Logf("populacao real: %d hosts declarados, %d mudos", len(hosts), len(mudos))
}

// O buraco de cobertura precisa dizer DE QUEM ele e.
//
// Ao remover a isencao do `-`, os buracos passaram a aparecer — e apareceram como quatro
// linhas identicas dizendo apenas "-", sem nomear o host. Trocar silencio por ruido que nao
// identifica o sujeito nao e conserto: quem le o relatorio continua sem saber qual host
// esta descoberto.
func TestTargets_CoverageHoleNamesItsHost(t *testing.T) {
	inv := escreverInventario(t, `cloud.loopzero.example - | reviewed | BFF autenticado
`)
	_, semRep, err := leak.Targets(inv, "cloud.loopzero.example")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(semRep) == 0 {
		t.Fatal("esperava um buraco reportado")
	}
	if !strings.Contains(semRep[0], "cloud.loopzero.example") {
		t.Errorf("o buraco nao diz de quem e: %q", semRep[0])
	}
}

// Isencao declarada com glob tem de valer para o que o glob cobre.
//
// Achado M-4 da revisao: `isentas` declara `/self-service/*`, mas o lookup era exato, entao
// `/self-service/registration` e `/self-service/registration/methods/password` reprovavam
// pela razao que a isencao ja cobria. Duas das dez falhas eram ruido.
func TestTargets_GlobExemptionCoversWhatItDeclares(t *testing.T) {
	inv := escreverInventario(t, `h.exemplo.dev /self-service/registration | reviewed | nota
h.exemplo.dev /self-service/registration/methods/password | reviewed | nota
`)
	_, semRep, err := leak.Targets(inv, "h.exemplo.dev")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(semRep) != 0 {
		t.Errorf("caminhos cobertos por isencao com glob nao sao buraco: %v", semRep)
	}
}

// Uma rota de classe `internal` nao tem credencial a negar; exigir 401 dela e erro de
// categoria.
//
// Achado M-3 da revisao: a coluna `class` do inventario era ignorada, e `app.loopzero.example -`
// e declarada `internal` — "static assets, no user data". Pior, `isentas` e a coluna `class`
// codificavam a mesma decisao em dois lugares, que e exatamente a "segunda lista que
// envelheceria sozinha" que este pacote escreve como razao de ler o inventario.
func TestTargets_InternalClassIsNotACoverageHole(t *testing.T) {
	inv := escreverInventario(t, `h.exemplo.dev /assets/* | internal | bundle estatico, sem dado de usuario
`)
	alvos, semRep, err := leak.Targets(inv, "h.exemplo.dev")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(semRep) != 0 || len(alvos) != 0 {
		t.Errorf("classe internal nao e buraco nem alvo: alvos=%v buracos=%v", alvos, semRep)
	}
}

// Uma familia que a sonda de identidade JA cobre nao pode ser contada como buraco.
//
// Achado HIGH-3 da revisao: o relatorio se contradizia na mesma execucao —
// `auth-dev.loopzero.example/userinfo` aparecia `ok 401` e, linhas abaixo,
// `auth-dev.loopzero.example /userinfo  sem representante`. Duas das dez falhas eram o relatorio
// discordando de si mesmo, e isso corroi a confianca mais rapido que volume.
func TestCoveredByIdentity_RemovesTheContradiction(t *testing.T) {
	buracos := []string{
		"auth-dev.loopzero.example /userinfo",
		"auth-dev.loopzero.example /.well-known/*",
		"auth-dev.loopzero.example /oauth2/*",
	}
	alvos := leak.IdentityTargets("auth-dev.loopzero.example")

	restou := leak.SemCoberturaDeIdentidade(buracos, alvos)
	for _, b := range restou {
		if strings.Contains(b, "/userinfo") || strings.Contains(b, "/.well-known/") {
			t.Errorf("familia sondada pela identidade continua contada como buraco: %q", b)
		}
	}
	achouOauth := false
	for _, b := range restou {
		if strings.Contains(b, "/oauth2/") {
			achouOauth = true
		}
	}
	if !achouOauth {
		t.Error("/oauth2/* NAO e sondado pela identidade e deve continuar sendo buraco")
	}
}
