// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func escrevePortas(t *testing.T, conteúdo string) string {
	t.Helper()
	c := filepath.Join(t.TempDir(), "host-ports.txt")
	if err := os.WriteFile(c, []byte(conteúdo), 0o600); err != nil {
		t.Fatalf("escrever registro: %v", err)
	}
	return c
}

// TestPortaNaoDeclaradaReprova é a asserção que dá sentido à polaridade.
//
// O firewall anterior abria por padrão e fechava por lista, e o resultado medido em
// 2026-08-10 foram 17 portas alcançáveis da internet sem estar em lista nenhuma. Aqui a
// ausência é reprovação: uma porta fora do registro é decisão que ninguém tomou.
func TestPortaNaoDeclaradaReprova(t *testing.T) {
	c := escrevePortas(t, "3002 | edge | loopzero-cloud pela borda\n")
	decls, err := ParsePorts(c)
	if err != nil {
		t.Fatalf("ParsePorts: %v", err)
	}
	naoDecl, _ := AuditPorts([]int{3002, 30082}, decls)
	if len(naoDecl) != 1 || naoDecl[0].Port != 30082 {
		t.Fatalf("queria a 30082 reportada como não declarada, veio %+v", naoDecl)
	}
	if !strings.Contains(naoDecl[0].Why, "fechado por padrão") {
		t.Errorf("a razão devia explicar a polaridade, veio %q", naoDecl[0].Why)
	}
}

// TestSoExternalPermaneceAberta — a classe é o que decide, não a intenção de quem lê.
func TestSoExternalPermaneceAberta(t *testing.T) {
	c := escrevePortas(t, `8787  | external | therag_api lido por gate de CI de fora
14433 | edge     | Kratos pela borda em HTTPS
30082 | admin    | ArgoCD
5432  | host     | Postgres
18083 | pendente | consumidor não medido
`)
	decls, err := ParsePorts(c)
	if err != nil {
		t.Fatalf("ParsePorts: %v", err)
	}
	fechar := PortsToClose([]int{8787, 14433, 30082, 5432, 18083}, decls)

	querFechar := map[int]bool{14433: true, 30082: true, 5432: true}
	if len(fechar) != len(querFechar) {
		t.Fatalf("queria %d portas a fechar, veio %d: %+v", len(querFechar), len(fechar), fechar)
	}
	for _, d := range fechar {
		if !querFechar[d.Port] {
			t.Errorf("porta %d (%s) não deveria ser fechada", d.Port, d.Class)
		}
	}
}

// TestPendenteNaoEFechadaAsCegas trava a lição de 2026-07-30.
//
// As portas 8787 e 18084 foram fechadas uma vez e derrubaram dois gates de CI que sondam
// o droplet de fora — os gates que DETECTAM deploy. Fechar uma porta cujo consumidor
// ninguém mediu troca uma exposição por uma cegueira, que é o pior negócio possível num
// controle de segurança.
func TestPendenteNaoEFechadaAsCegas(t *testing.T) {
	c := escrevePortas(t, "18083 | pendente | consumidor não medido\n")
	decls, err := ParsePorts(c)
	if err != nil {
		t.Fatalf("ParsePorts: %v", err)
	}
	if fechar := PortsToClose([]int{18083}, decls); len(fechar) != 0 {
		t.Errorf("porta pendente não pode ser fechada antes de medir quem a consome, veio %+v", fechar)
	}
}

// TestLinhaMalformadaEErroENaoSilencio — num registro de segurança, a linha que o parser
// pula é exatamente a porta que ninguém revisou.
func TestLinhaMalformadaEErroENaoSilencio(t *testing.T) {
	casos := map[string]string{
		"sem classe":      "3002\n",
		"classe inválida": "3002 | qualquer | algo\n",
		"sem razão":       "3002 | edge | \n",
		"porta inválida":  "abc | edge | algo\n",
		"duplicada":       "3002 | edge | um\n3002 | host | outro\n",
	}
	for nome, conteúdo := range casos {
		t.Run(nome, func(t *testing.T) {
			if _, err := ParsePorts(escrevePortas(t, conteúdo)); err == nil {
				t.Error("esperava erro; um registro de segurança não pode aceitar linha que ninguém consegue revisar")
			}
		})
	}
}

// TestRegistroRealDoRepositorioEValido — o arquivo que de fato governa o host.
func TestRegistroRealDoRepositorioEValido(t *testing.T) {
	raiz := repoRootFromTest(t)
	decls, err := ParsePorts(filepath.Join(raiz, "infra", "quality", "host-ports.txt"))
	if err != nil {
		t.Fatalf("o registro real não parseia: %v", err)
	}
	if len(decls) < 20 {
		t.Errorf("o registro tem %d portas; a medição de 2026-08-10 encontrou 39 escutando, "+
			"então um registro muito menor sugere que ele parou de acompanhar o host", len(decls))
	}
	// A classe `external` é a única que permanece exposta: cada linha dela precisa dizer
	// QUEM consome, e uma razão curta demais não diz.
	for _, d := range decls {
		if d.Class == ClassExternal && len(d.Reason) < 20 {
			t.Errorf("porta %d é `external` com razão curta demais (%q) — a exceção precisa nomear "+
				"o consumidor e como foi medido", d.Port, d.Reason)
		}
	}
}
