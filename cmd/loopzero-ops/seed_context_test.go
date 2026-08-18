// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A raiz de composicao precisa saber QUAL contexto resolveu, nao apenas resolver.
//
// B-024: `buildKubeClientAndRest("")` deixa o ConfigFlags escolher o contexto ativo, e o
// comentario ao lado chamava o resultado de "cliente do mgmt". Nao era — era "seja la qual
// contexto estiver ativo". Depois de `k3d:secrets:create`, que troca o contexto como efeito
// colateral, os seeds passavam a operar o cluster de secrets achando que era o central.
func TestResolveContextName_ReadsTheActiveContext(t *testing.T) {
	dir := t.TempDir()
	kc := filepath.Join(dir, "config")
	if err := os.WriteFile(kc, []byte(`apiVersion: v1
kind: Config
current-context: k3d-loopzero-secrets
clusters:
- name: c1
  cluster: {server: https://127.0.0.1:6443}
contexts:
- name: k3d-loopzero-core
  context: {cluster: c1, user: u1}
- name: k3d-loopzero-secrets
  context: {cluster: c1, user: u1}
users:
- name: u1
  user: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", kc)

	got := resolveContextName("")
	if got != "k3d-loopzero-secrets" {
		t.Errorf("contexto ativo resolvido como %q, esperava k3d-loopzero-secrets", got)
	}
}

// Um contexto pedido explicitamente vence o ativo — e o operador precisa ver o que pediu
// refletido de volta, senao a mensagem mente na direcao oposta.
func TestResolveContextName_ExplicitWins(t *testing.T) {
	// Isola o ambiente: o ramo explicito curto-circuita antes de ler o kubeconfig, entao
	// sem isto o teste passaria mesmo se a ordem invertesse.
	t.Setenv("KUBECONFIG", writeKubeconfig(t, "k3d-loopzero-secrets"))

	if got := resolveContextName("k3d-loopzero-core"); got != "k3d-loopzero-core" {
		t.Errorf("contexto explicito virou %q", got)
	}
}

// Sem contexto a nomear a funcao devolve vazio em vez de inventar — uma mensagem com um
// nome errado e pior que uma sem nome nenhum.
//
// Medido na revisao (achado 6): um KUBECONFIG apontando para arquivo AUSENTE nao produz
// erro no clientcmd — ele simplesmente resolve `current-context` vazio. Este teste exercita
// esse ramo, e o nome dele foi corrigido para dizer isso; o ramo de erro de leitura
// (arquivo malformado) segue sem cobertura, e isso esta dito em vez de suposto.
func TestResolveContextName_AbsentKubeconfigIsEmptyNotInvented(t *testing.T) {
	t.Setenv("CORE_CONTEXT", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "nao-existe"))

	if got := resolveContextName(""); got != "" {
		t.Errorf("sem kubeconfig deveria devolver vazio, veio %q", got)
	}
}

// `CORE_CONTEXT` fixa o cluster que os seeds operam, em vez de herdar o ativo.
//
// B-024, achado da revisao: nomear o contexto na mensagem melhora o diagnostico mas NAO
// elimina a heranca — trocava apenas QUAL cluster errado era atingido. Antes do conserto do
// taskfile os seeds caiam no cluster de secrets; depois dele, caem em onde quer que o
// `task setup` tenha deixado o contexto. Os dois estao errados.
//
// A convencao ja existia no repositorio — `seed-post-cleanroom-all.sh:335` usa
// `${CORE_CONTEXT:-k3d-loopzero-core}` para os proprios kubectl. A escada parou na rung 4:
// reusar o que esta instalado, nao inventar variavel nova.
func TestResolveContextName_CoreContextPins(t *testing.T) {
	t.Setenv("CORE_CONTEXT", "k3d-loopzero-core")

	if got := resolveContextName(""); got != "k3d-loopzero-core" {
		t.Errorf("CORE_CONTEXT deveria fixar o contexto, veio %q", got)
	}
}

// Um contexto pedido explicitamente ainda vence a variavel — quem digita sabe o que quer.
func TestResolveContextName_ExplicitBeatsCoreContext(t *testing.T) {
	t.Setenv("CORE_CONTEXT", "k3d-loopzero-secrets")

	if got := resolveContextName("k3d-loopzero-build"); got != "k3d-loopzero-build" {
		t.Errorf("explicito deveria vencer CORE_CONTEXT, veio %q", got)
	}
}

// Sem a variavel o comportamento nao muda: contexto ativo. Fixar um padrao rigido aqui
// quebraria quem roda seeds fora do k3d, e o item nao pede isso — pede que a origem seja
// declarada ou dita.
func TestResolveContextName_UnsetKeepsInheriting(t *testing.T) {
	t.Setenv("CORE_CONTEXT", "")
	t.Setenv("KUBECONFIG", writeKubeconfig(t, "k3d-loopzero-cell-1"))

	if got := resolveContextName(""); got != "k3d-loopzero-cell-1" {
		t.Errorf("sem CORE_CONTEXT deveria herdar o ativo, veio %q", got)
	}
}

// writeKubeconfig escreve um kubeconfig minimo com o contexto ativo pedido.
func writeKubeconfig(t *testing.T, current string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	body := "apiVersion: v1\nkind: Config\ncurrent-context: " + current + "\n" +
		"clusters:\n- name: c1\n  cluster: {server: https://127.0.0.1:6443}\n" +
		"contexts:\n- name: " + current + "\n  context: {cluster: c1, user: u1}\n" +
		"users:\n- name: u1\n  user: {}\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// O contexto que a mensagem NOMEIA tem de ser o mesmo que o cliente USA.
//
// Achado 1 da revisao, e o risco que eu quase introduzi ao fazer `CORE_CONTEXT` fixar: se
// o nome for resolvido num caminho e o cliente construido noutro, a mensagem passa a
// nomear um cluster onde ninguem procurou — pior que nao nomear nada, porque manda o
// operador conferir o lugar errado com confianca.
func TestResolvedContextIsTheOneTheClientUses(t *testing.T) {
	t.Setenv("CORE_CONTEXT", "ctx-fixado")
	t.Setenv("KUBECONFIG", writeTwoContextKubeconfig(t))

	resolvido := resolveContextName("")
	usado, err := contextUsedByClientConfig("")
	if err != nil {
		t.Fatalf("resolver a config do cliente: %v", err)
	}
	if resolvido != usado {
		t.Errorf("a mensagem nomearia %q e o cliente usaria %q — divergem", resolvido, usado)
	}
}

// writeTwoContextKubeconfig escreve um kubeconfig cujo contexto ATIVO difere do fixado, para
// que a divergencia apareca se existir.
func writeTwoContextKubeconfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	body := `apiVersion: v1
kind: Config
current-context: ctx-ativo
clusters:
- name: cl-ativo
  cluster: {server: https://127.0.0.1:6443}
- name: cl-fixado
  cluster: {server: https://127.0.0.1:6447}
contexts:
- name: ctx-ativo
  context: {cluster: cl-ativo, user: u1}
- name: ctx-fixado
  context: {cluster: cl-fixado, user: u1}
users:
- name: u1
  user: {}
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
